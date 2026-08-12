package insights

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/provider"
	"github.com/shikanon/cookies/internal/systems/insights/skills"
)

// 特征提取运行器：AM-005 里「AI 提」的那一半。
//
// **模型产出只进 AI 推断层，永远不进人工结论层**（03 §14、09 §8）。
// 这不是一句原则，是代码路径上的事实：这里唯一的写入口是 ExtractFeatures，
// 它写的是 source='ai' 的行，而人工结论走 PatchFeatures，两条路不相交。
// 提取完成后素材进「待确认」，等人来看——模型说完话，事情没完。
//
// **只有人点按钮才会走到这里。** 素材登记时不自动排队：自动提取意味着
// 复核队列会被灌满没人要看的结果，而复核是这套东西唯一的质量闸门。

// TextGenerator 是这里对供应商的全部依赖。
// 定义成接口而不是直接用 *provider.Service，是为了让测试能在不连供应商的
// 情况下走完整条路——包括失败路径，那条路在真实供应商上很难稳定复现。
type TextGenerator interface {
	GenerateText(context.Context, provider.TextGenerateRequest) (provider.SynchronousResponse, error)
}

// AnalyzeAssetRequest 是人点下「提取特征」时带上的东西。
//
// **Content 什么时候可以不填**：视频类素材，指向了素材库里的文件，而且这个环境
// 接了多模态——三条都满足时，画面由模型自己去看（understanding.go），人不用再
// 写一遍画面描述。其余情况仍然必填：图文类的本体就是那段文字，洞察这边不存正文
// （insight_assets 只有身份和状态），拿不到就只能由调用方带上。
//
// 视频类填了 Content 也不浪费——它会和多模态看到的东西一起给模型，当成人的补充。
// 人知道的东西模型不一定看得出来（「这条是给老客户的续费提醒」）。
type AnalyzeAssetRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
	// Content 是要分析的素材内容：图文类填正文；视频类不填时由多模态去看画面。
	Content string `json:"content"`
	// Note 是人写的补充说明（例如「这条是竖版重剪，画面同 A 版」）。
	Note string `json:"note,omitempty"`
}

// validate 只管长度。「填没填」这件事要等素材类型和文件引用都读出来才判得了，
// 放在 AnalyzeAsset 里（missingContentError）。
func (r AnalyzeAssetRequest) validate() error {
	// 上限不是性能考虑，是成本和可解释性：超长输入下模型的注意力会散开，
	// 提出来的特征更多是「文章里提过」而不是「这篇文章的特点」。
	if len([]rune(strings.TrimSpace(r.Content))) > 20000 {
		return fmt.Errorf("%w: 素材内容超过 2 万字，请截取要分析的部分", ErrInvalidRequest)
	}
	if len([]rune(r.Note)) > 1000 {
		return fmt.Errorf("%w: 补充说明请控制在 1000 字以内", ErrInvalidRequest)
	}
	return nil
}

// AnalyzeAssetResult 是一次提取的完整结果。
//
// Dropped 一定要回给调用方。少几条特征在界面上看不出来——
// 用户只会觉得「模型没看出来」，而真实原因可能是格式没对上，
// 那是要修的 bug，不是模型能力问题。
type AnalyzeAssetResult struct {
	Run      AnalysisRun    `json:"run"`
	Features []AssetFeature `json:"features"`
	Dropped  []string       `json:"dropped_fields,omitempty"`
}

// AnalyzeAsset 跑一次特征提取。
//
// 失败路径上仍然会留下一条 failed 的 AnalysisRun——**这是有意的**：
// 只记成功的话，运营面上的成功率永远是 100%，供应商挂掉的那天也是一片绿。
func (s Service) AnalyzeAsset(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request AnalyzeAssetRequest) (AnalyzeAssetResult, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return AnalyzeAssetResult{}, err
	}
	if s.Runs == nil {
		return AnalyzeAssetResult{}, fmt.Errorf("分析任务留痕未配置，拒绝执行无法追溯的提取")
	}
	if err := request.validate(); err != nil {
		return AnalyzeAssetResult{}, err
	}
	projectContext, err := s.Projects.RequireActiveContext(ctx, actor, projectID)
	if err != nil {
		return AnalyzeAssetResult{}, err
	}
	asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return AnalyzeAssetResult{}, err
	}
	if !asset.TypeIdentified() {
		return AnalyzeAssetResult{}, fmt.Errorf("%w: 素材类型待识别，无法确定要提取哪套特征", ErrInvalidState)
	}
	schema, ok := FeatureSchemaFor(asset.AssetType)
	if !ok {
		return AnalyzeAssetResult{}, fmt.Errorf("%w: 素材类型 %q 没有特征体系", ErrInvalidState, string(asset.AssetType))
	}
	registry, err := s.skillRegistry()
	if err != nil {
		return AnalyzeAssetResult{}, err
	}
	skill, ok := registry.For(string(asset.AssetType))
	if !ok {
		return AnalyzeAssetResult{}, fmt.Errorf("%w: %s还没有特征提取 Skill", ErrInvalidState, schema.Label)
	}
	outputSchema, err := featureOutputSchema(schema)
	if err != nil {
		return AnalyzeAssetResult{}, err
	}

	instruction := extractionInstruction(skill, schema)
	note := strings.TrimSpace(request.Note)
	human := strings.TrimSpace(request.Content)

	// 视频类先问多模态。这一步可能什么都不做——不是视频、没指向素材库里的文件、
	// 或者这个环境没接多模态，那时 sawVideo 为 false，下面照走老路。
	understanding, sawVideo, err := s.understandingFor(ctx, actor, projectID, asset)
	if err != nil {
		return AnalyzeAssetResult{}, err
	}
	switch {
	case sawVideo && understanding.Pending && human == "":
		// 正在看。**不落 failed 留痕**：这一趟什么都没跑，记成失败会让运营面上
		// 凭空多出一堆「提取失败」，而真实情况是人来早了。
		return AnalyzeAssetResult{}, fmt.Errorf("%w: 模型正在看这条视频，看完再点一次提取",
			ErrUnderstandingPending)
	case sawVideo && understanding.Ready && understanding.hasContent():
		// 看完了而且有东西，走多模态那条路。
	default:
		// 其余一律回落到人填的正文：还在看但人已经填了（不该让人干等）、
		// 规格不支持、理解失败、看完了却一句话都没有。
		sawVideo = false
	}
	if !sawVideo && human == "" {
		return AnalyzeAssetResult{}, missingContentError(asset, understanding)
	}

	content, promptVersion := human, extractionPromptVersion
	payload := extractionPayload(asset, content, note)
	if sawVideo {
		content = visionContent(understanding)
		promptVersion = extractionVisionPromptVersion
		payload = visionExtractionPayload(asset, content, note, human)
	}

	// 先落一行 running，再调模型。反过来的话，调用期间进程挂掉就什么都不剩，
	// 而那正是最需要被看见的一类失败——它不会出现在任何统计里。
	started := s.now()
	runID, err := s.idGenerator()("insightrun")
	if err != nil {
		return AnalyzeAssetResult{}, err
	}
	summaryFields := map[string]any{
		"asset_revision": asset.Revision,
		"content_chars":  len([]rune(content)),
		"has_note":       request.Note != "",
		"field_count":    len(schema.Fields),
	}
	if sawVideo {
		// 视觉那一次调用的来历放在输入侧，不放 ProviderCode/ModelVersion——
		// 那两格记的是产出特征的那次文本调用，混进来的话，回头查「这批特征哪个
		// 模型提的」会查到一个根本没提过特征的视觉模型。
		summaryFields["vision"] = true
		summaryFields["vision_artifact_id"] = understanding.ArtifactID
		summaryFields["vision_keyframes"] = understanding.KeyframeCount
		summaryFields["vision_model_alias"] = understanding.ModelAlias
		summaryFields["vision_provider_code"] = understanding.ProviderCode
		summaryFields["human_supplement_chars"] = len([]rune(human))
	}
	inputSummary, _ := json.Marshal(summaryFields)
	run := AnalysisRun{
		ID: runID, OrganizationID: actor.OrganizationID, ProjectID: projectID,
		Kind: AnalysisRunFeatureExtraction, AssetID: asset.ID, AssetType: asset.AssetType,
		Status:  AnalysisRunRunning,
		SkillID: skill.Name, SkillVersion: skill.Version, SkillContentHash: skill.ContentHash,
		PromptVersion: promptVersion,
		// 输入指纹算的是真正发出去的那两段，不是原始正文——
		// 指令变了而正文没变，也应当算作不同的输入。
		InputHash: hashPayload([]byte(instruction + "\n" + payload)),
		// 全文不入表（09 §7），只留规模。
		InputSummary: inputSummary,
		ModelAlias:   s.textModelAlias(),
		StartedAt:    started, CreatedBy: actor.Principal.ID, CreatedAt: started, UpdatedAt: started,
	}
	if run, err = s.Runs.CreateAnalysisRun(ctx, run); err != nil {
		return AnalyzeAssetResult{}, err
	}

	inputs, dropped, trace, err := s.generateFeatures(ctx, actor, projectContext, schema, instruction, payload, outputSchema, asset.ID, run.InputHash)
	if err != nil {
		s.failRun(ctx, run, err)
		return AnalyzeAssetResult{}, err
	}
	if len(inputs) == 0 {
		// 一条都没提出来当失败处理。当成功会让「模型什么也没看出来」被计入
		// 成功率，而这种运行对使用者来说和报错没有区别——都是白等一场。
		failure := fmt.Errorf("%w: 模型没有提出任何符合%s特征体系的字段", ErrInvalidState, schema.Label)
		s.failRun(ctx, run, failure)
		return AnalyzeAssetResult{}, failure
	}

	features, err := s.ExtractFeatures(ctx, actor, projectID, asset.ID, ExtractFeaturesRequest{
		ExpectedVersion: request.ExpectedVersion,
		SkillID:         skill.Name, SkillVersion: skill.Version, Features: inputs,
	})
	if err != nil {
		s.failRun(ctx, run, err)
		return AnalyzeAssetResult{}, err
	}

	finished := s.now()
	run.Status = AnalysisRunSucceeded
	run.ProviderCode = trace.providerCode
	run.ModelVersion = trace.modelVersion
	run.RouteRevisionID = trace.routeRevisionID
	run.ResponseMode = trace.responseMode
	run.GenerationMode = trace.mode
	run.ResultHash = hashFeatureInputs(inputs)
	run.FeatureCount = len(features)
	run.DroppedFields = dropped
	run.PromptTokens = trace.promptTokens
	run.CompletionTokens = trace.completionTokens
	run.LatencyMS = int(finished.Sub(started).Milliseconds())
	run.FinishedAt = &finished
	run.UpdatedAt = finished
	saved, err := s.Runs.FinishAnalysisRun(ctx, run)
	if err != nil {
		// 特征已经写进去了，留痕却没写上。**不回滚特征**：
		// 有结果没留痕，比没结果好办——留痕能补，结果重跑要再花一次钱。
		// 但必须报出来，否则这条特征会永远缺一段来历。
		return AnalyzeAssetResult{}, fmt.Errorf("特征已写入但分析留痕失败，这批特征暂时无法追溯：%w", err)
	}
	return AnalyzeAssetResult{Run: saved, Features: features, Dropped: dropped}, nil
}

// ListAnalysisRuns 供「数据与方法」视图读这条素材的分析历史（03 §82）。
func (s Service) ListAnalysisRuns(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, filter AnalysisRunFilter) ([]AnalysisRun, error) {
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	if s.Runs == nil {
		return nil, fmt.Errorf("分析任务留痕未配置")
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	return s.Runs.ListAnalysisRuns(ctx, actor.OrganizationID, projectID, filter)
}

const (
	extractionPromptVersion = "insight.extract.v1"
	// 视频那条路单独一个版本号。喂进去的东西完全不同（视觉证据 vs 人的转述），
	// 共用一个版本号会让「换了 prompt 之后效果变没变」这个问题永远算不清楚。
	extractionVisionPromptVersion = "insight.extract.video.v1"
)

// missingContentError 解释「为什么这条素材必须人填正文」。
//
// 四种情况的说法不一样，而这正是这个函数存在的理由：统一回一句「请填素材内容」，
// 人对着一条明明有视频文件的素材会以为系统坏了。
func missingContentError(asset Asset, understanding MediaUnderstanding) error {
	switch {
	case !asset.AssetType.IsVideo():
		return fmt.Errorf("%w: %s要分析的是那段文字本身，请把正文贴进来",
			ErrInvalidRequest, asset.AssetType.Label())
	case asset.PlatformAssetID == "" || asset.PlatformAssetVersion == 0:
		return fmt.Errorf("%w: 这条素材没指向素材库里的视频文件，模型看不到画面，"+
			"请先补一段画面描述或脚本。从创意导入的素材才带这个引用", ErrInvalidRequest)
	case strings.TrimSpace(understanding.Unavailable) != "":
		// 理由括起来：它是媒体理解那边原样带过来的一整句话，直接用逗号接上去，
		// 两句话会糊成一句读不通的长句。
		return fmt.Errorf("%w: 模型这次看不了这条视频（%s），请补一段画面描述或脚本",
			ErrInvalidRequest, strings.TrimSpace(understanding.Unavailable))
	default:
		return fmt.Errorf("%w: 这个环境还没接多模态，视频画面进不了模型，"+
			"请补一段画面描述或脚本", ErrInvalidRequest)
	}
}

// generationTrace 是一次模型调用留下的可追溯信息。
type generationTrace struct {
	mode             GenerationMode
	providerCode     string
	modelVersion     string
	routeRevisionID  string
	responseMode     string
	promptTokens     int
	completionTokens int
}

func (s Service) generateFeatures(ctx context.Context, actor contract.ActorContext, projectContext contract.ProjectContext, schema FeatureSchema, instruction, payload string, outputSchema json.RawMessage, assetID, inputHash string) ([]FeatureInput, []string, generationTrace, error) {
	if s.Text == nil {
		// 没配供应商时不假装提取。返回空+模板模式，让上层按「一条都没提出来」失败，
		// 而不是写进一批编造的特征——库里一条假特征的代价，远大于一次失败的提取。
		return nil, nil, generationTrace{mode: GenerationModeTemplate}, fmt.Errorf("%w: 还没有配置可用的文本模型，无法提取特征", ErrInvalidState)
	}
	textActor := actor
	// 先拷一份再改，别在调用方的切片上追加。已经有这个 scope 就不再加一次——
	// ActorContext.Validate 见到重复的 scope 会直接拒，请求根本发不出去。
	textActor.Scopes = append([]contract.Scope{}, actor.Scopes...)
	if !textActor.HasScope(provider.ScopeTextGenerate) {
		textActor.Scopes = append(textActor.Scopes, provider.ScopeTextGenerate)
	}
	response, err := s.Text.GenerateText(ctx, provider.TextGenerateRequest{
		Actor: textActor, Project: projectContext, ModelAlias: s.textModelAlias(),
		// 连点两次付两次钱是要避免的，但只按素材 ID 去重会走过头：同一条素材
		// 先按人填的正文提一次、再走多模态提一次，输入完全不同，却会命中同一个
		// key 拿回上一次的答案。所以键里带上输入指纹——同样的输入才算重复。
		InvocationKey: contract.IdempotencyKey("insight-extract-" + assetID + "-" + truncateRunes(inputHash, 16)),
		Messages: []provider.TextMessage{
			{Role: provider.TextRoleSystem, Content: instruction},
			{Role: provider.TextRoleUser, Content: payload},
		},
		OutputJSONSchema: outputSchema,
	})
	if err != nil {
		return nil, nil, generationTrace{}, fmt.Errorf("调用文本模型失败：%w", err)
	}
	trace := generationTrace{
		mode: GenerationModeModel, providerCode: response.ProviderCode,
		modelVersion: response.ModelVersion, routeRevisionID: response.RouteRevisionID,
		responseMode: string(response.ResponseMode),
	}
	if response.Usage != nil {
		trace.promptTokens = int(response.Usage.InputTokens)
		trace.completionTokens = int(response.Usage.OutputTokens)
	}
	candidate := response.StructuredOutput
	if len(candidate) == 0 {
		candidate = trimJSONFence(response.Text)
	}
	if len(bytes.TrimSpace(candidate)) == 0 {
		return nil, nil, trace, fmt.Errorf("%w: 模型没有返回内容", ErrInvalidState)
	}
	var output extractionOutput
	if err := decodeExactlyOneJSON(candidate, &output); err != nil {
		// 不把模型的原始返回写进错误信息（09 §7：不记录内容全文）。
		return nil, nil, trace, fmt.Errorf("%w: 模型返回的不是约定的特征格式", ErrInvalidState)
	}
	inputs, dropped := featureInputsFromOutput(schema, output)
	return inputs, dropped, trace, nil
}

// failRun 把 running 那一行结掉。
//
// 结不掉也只记不管——**原始失败必须原样返回给调用方**，
// 用留痕失败盖掉它会让人去查错的方向。
func (s Service) failRun(ctx context.Context, run AnalysisRun, cause error) {
	finished := s.now()
	run.Status = AnalysisRunFailed
	run.FinishedAt = &finished
	run.UpdatedAt = finished
	run.LatencyMS = int(finished.Sub(run.StartedAt).Milliseconds())
	run.ErrorCode = extractionErrorCode(cause)
	run.ErrorMessage = truncateRunes(cause.Error(), 900)
	_, _ = s.Runs.FinishAnalysisRun(ctx, run)
}

func extractionErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return "invalid_request"
	case errors.Is(err, ErrInvalidState):
		return "invalid_state"
	case errors.Is(err, ErrVersionConflict):
		return "conflict"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	default:
		return "provider_error"
	}
}

// extractionInstruction 拼 system 消息：角色 + 规则 + 这套特征体系的说明。
//
// 特征清单也放进去，尽管输出格式里已经有了一份。**重复是有用的**：
// 有些供应商把 JSON Schema 当成事后校验而不是生成约束，模型看不到字段说明；
// 写在正文里能让「时长填秒数」这类要求真正到达模型。
func extractionInstruction(skill skills.Snapshot, schema FeatureSchema) string {
	var builder strings.Builder
	builder.WriteString(skill.Persona)
	builder.WriteString("\n\n提取规则：\n")
	for index, rule := range skill.Instructions {
		fmt.Fprintf(&builder, "%d. %s\n", index+1, rule)
	}
	builder.WriteString("\n要提取的特征（按分组）：\n")
	for _, group := range schema.Groups() {
		fmt.Fprintf(&builder, "【%s】\n", group)
		for _, field := range schema.Fields {
			if field.Group != group {
				continue
			}
			fmt.Fprintf(&builder, "- %s（%s）", field.Key, field.Label)
			if field.Unit != "" {
				fmt.Fprintf(&builder, "，单位 %s", field.Unit)
			}
			if len(field.Vocabulary) > 0 {
				fmt.Fprintf(&builder, "，只能选：%s", strings.Join(field.Vocabulary, "、"))
			}
			if field.Note != "" {
				fmt.Fprintf(&builder, "。%s", field.Note)
			}
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\n只返回一个 JSON 对象，不要任何解释文字。")
	return builder.String()
}

// extractionPayload 拼 user 消息。素材标题也给出去——
// 标题本身就是要提取的对象之一（标题角度、标题与正文一致性）。
func extractionPayload(asset Asset, content, note string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "素材标题：%s\n", asset.Title)
	if note != "" {
		fmt.Fprintf(&builder, "补充说明：%s\n", note)
	}
	builder.WriteString("\n素材内容：\n")
	builder.WriteString(content)
	return builder.String()
}

// visionExtractionPayload 拼视频那条路的 user 消息。
//
// 和图文那条路的关键差别是**要告诉模型这段字是怎么来的**：它读到的不是素材原件，
// 是另一个模型看画面之后写下的证据。不说明的话，模型会把「模型的推断」那一段
// 当成画面里确凿存在的东西，两层推断叠起来，落库的特征说不清是从哪来的。
//
// 人填的正文放在最后，标明是补充。不覆盖视觉证据，也不被视觉证据覆盖——
// 人知道的和模型看到的是两种东西（「这条是给老客户的续费提醒」画面里看不出来）。
func visionExtractionPayload(asset Asset, vision, note, human string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "素材标题：%s\n", asset.Title)
	builder.WriteString("\n下面是多模态模型看过这条视频的关键帧之后写下的内容。" +
		"标着「观察」的是画面里能看到的，标着「推断」的是它的判断，不要当成事实。\n\n")
	builder.WriteString(vision)
	if note != "" {
		fmt.Fprintf(&builder, "\n\n【人写的补充说明】\n%s", note)
	}
	if human != "" {
		fmt.Fprintf(&builder, "\n\n【人补充的脚本或画面描述】\n%s", human)
	}
	return builder.String()
}

func (s Service) skillRegistry() (skills.Registry, error) {
	if s.Skills != nil {
		return *s.Skills, nil
	}
	registry, err := skills.DefaultRegistry()
	if err != nil {
		return skills.Registry{}, fmt.Errorf("加载特征提取 Skill 失败：%w", err)
	}
	return registry, nil
}

func (s Service) textModelAlias() string {
	if alias := strings.TrimSpace(s.TextModelAlias); alias != "" {
		return alias
	}
	return "cookies.text.standard"
}

// trimJSONFence 去掉模型习惯性包上的 ```json 围栏。
func trimJSONFence(value string) json.RawMessage {
	content := strings.TrimSpace(value)
	if strings.HasPrefix(content, "```") {
		if newline := strings.IndexByte(content, '\n'); newline >= 0 {
			content = content[newline+1:]
		}
		content = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(content), "```"))
	}
	return json.RawMessage(content)
}

// decodeExactlyOneJSON 只接受恰好一个 JSON 值。
// 后面还跟着东西说明模型没按格式来，此时前半段也不可信。
func decodeExactlyOneJSON(value json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(value))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("模型返回了不止一个 JSON 值")
	}
	return nil
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
