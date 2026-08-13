package insights

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 分析素材库与内容分析的领域层（建设顺序第 1、2 批）。
//
// 素材类型与特征字段的权威定义在 features.go，由 03 §5 逐条转录；本文件只负责
// 「素材怎么进来、状态怎么走、特征怎么写」，不复制字段清单。

// AnalysisStatus is the analysis lifecycle in 03 §11.1:
// 待数据 → 待匹配 → 可分析 → 分析中 → 待确认 → 已确认 / 待复审 / 已失效。
type AnalysisStatus string

const (
	AnalysisAwaitingData        AnalysisStatus = "awaiting_data"        // 待数据
	AnalysisAwaitingMatch       AnalysisStatus = "awaiting_match"       // 待匹配
	AnalysisAnalysable          AnalysisStatus = "analysable"           // 可分析
	AnalysisAnalysing           AnalysisStatus = "analysing"            // 分析中
	AnalysisPendingConfirmation AnalysisStatus = "pending_confirmation" // 待确认
	AnalysisConfirmed           AnalysisStatus = "confirmed"            // 已确认
	AnalysisNeedsReview         AnalysisStatus = "needs_review"         // 待复审
	AnalysisRetired             AnalysisStatus = "retired"              // 已失效
)

func (s AnalysisStatus) valid() bool {
	switch s {
	case AnalysisAwaitingData, AnalysisAwaitingMatch, AnalysisAnalysable, AnalysisAnalysing,
		AnalysisPendingConfirmation, AnalysisConfirmed, AnalysisNeedsReview, AnalysisRetired:
		return true
	}
	return false
}

// Label returns the Chinese name used in 03 §11.1 and in the UI.
func (s AnalysisStatus) Label() string {
	switch s {
	case AnalysisAwaitingData:
		return "待数据"
	case AnalysisAwaitingMatch:
		return "待匹配"
	case AnalysisAnalysable:
		return "可分析"
	case AnalysisAnalysing:
		return "分析中"
	case AnalysisPendingConfirmation:
		return "待确认"
	case AnalysisConfirmed:
		return "已确认"
	case AnalysisNeedsReview:
		return "待复审"
	case AnalysisRetired:
		return "已失效"
	}
	return string(s)
}

// AssetSourceKind is where an indexed asset came from (AM-001).
type AssetSourceKind string

const (
	AssetSourceCreative AssetSourceKind = "creative" // 创意模块产物
	AssetSourceUpload   AssetSourceKind = "upload"   // 上传文件
	AssetSourceExternal AssetSourceKind = "external" // 外部引用
	// AssetSourceMiyun 是米云采集、导入或回流回来的素材。
	//
	// 它从 external 里拆出来是因为那个标签在界面上写的是「外部引用」，指的是
	// 平台外的竞品参照证据——那些永远不能投。米云的素材有 platform_asset_id、
	// 能投、要跑归因。同一个词指两样东西，看的人一定会搞错。
	AssetSourceMiyun AssetSourceKind = "miyun"
)

func (k AssetSourceKind) valid() bool {
	switch k {
	case AssetSourceCreative, AssetSourceUpload, AssetSourceExternal, AssetSourceMiyun:
		return true
	}
	return false
}

// AssetRole 是素材的**身份**，和 AnalysisStatus 说的**进度**是两回事。
//
// 台账（ledger）是平台里所有素材的账本：创意做的每一张图、每一版剪辑、每一段配音
// 都在里面，绝大多数永远不会拿去投流。分析对象（analysis）是真正投出去、有花费、
// 要跑归因的那些成品。
//
// 不做成第九个 analysis_status 的理由：一条素材从台账被拉进分析时，它走到过哪一步
// 应该原样保留；退回台账再拉回来也不该清零。两个正交维度各管各的，队列一律按
// role 过滤，而不是靠把状态归零来实现。
type AssetRole string

const (
	AssetRoleLedger   AssetRole = "ledger"   // 台账：登记在册，不进分析队列
	AssetRoleAnalysis AssetRole = "analysis" // 分析对象：投过流、要跑归因
)

func (r AssetRole) valid() bool {
	switch r {
	case AssetRoleLedger, AssetRoleAnalysis:
		return true
	}
	return false
}

func (r AssetRole) Label() string {
	switch r {
	case AssetRoleLedger:
		return "台账"
	case AssetRoleAnalysis:
		return "分析对象"
	}
	return string(r)
}

// FeatureSource separates the data layers 03 §14 insists must not overwrite
// each other: AI 推断、人工结论，以及从文件本身算出来的客观量。
type FeatureSource string

const (
	// SourceAI 模型推断：模型看着素材猜出来的。可以展示，不能进归因结论——
	// 拿一个猜测去解释另一个猜测，结论看起来有理，其实一层假设都没减少。
	SourceAI FeatureSource = "ai"
	// SourceHuman 人工标注：人填的。人会错，但人为自己填的东西负责。
	SourceHuman FeatureSource = "human"
	// SourceDerived 客观可测：从文件本身算出来的，时长、分辨率、镜头数、语速。
	// 同一个文件算两遍结果一样，这是三类里唯一可复现的。
	SourceDerived FeatureSource = "derived"
)

func (s FeatureSource) valid() bool {
	return s == SourceAI || s == SourceHuman || s == SourceDerived
}

func (s FeatureSource) Label() string {
	switch s {
	case SourceDerived:
		return "客观可测"
	case SourceHuman:
		return "人工标注"
	case SourceAI:
		return "模型推断"
	}
	return string(s)
}

// AdmissibleForAttribution 决定这个来源的特征能不能进归因结论。
func (s FeatureSource) AdmissibleForAttribution() bool {
	return s == SourceDerived || s == SourceHuman
}

// Confidence is the level an AI inference must carry (03 §5 末).
type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

func (c Confidence) valid() bool {
	switch c {
	case ConfidenceLow, ConfidenceMedium, ConfidenceHigh:
		return true
	}
	return false
}

// ReviewState is the per-feature human verdict behind AM-006.
//
// An AI row is written 待复核 and stays that way: §14 keeps the machine's answer
// intact, so the review is recorded by the 人工结论 row that appears beside it.
// On that human row the state says whether the person agreed with the machine —
// confirmed 表示认可 AI 的判断，rejected 表示推翻它并给出自己的取值 — which is what
// makes 技能提取准确率 measurable later.
//
// authored 是第四种，它和上面三种的区别是刚性的：**AI 从来没提过这一项，人是第一个
// 填的**。rejected 说的是「有个推断，我不认」，authored 说的是「没有推断，我来填」。
// 把两者混成一个值有两处后果：投后分析会把人手填的特征当成「被否掉的推断」丢掉，
// 于是内容分析里填的东西在素材对比和驱动因素里一条都看不见；技能提取准确率也会把
// AI 压根没提过的项算成它提错了。
type ReviewState string

const (
	ReviewPending   ReviewState = "pending"
	ReviewConfirmed ReviewState = "confirmed"
	ReviewRejected  ReviewState = "rejected"
	ReviewAuthored  ReviewState = "authored"
)

func (s ReviewState) valid() bool {
	switch s {
	case ReviewPending, ReviewConfirmed, ReviewRejected, ReviewAuthored:
		return true
	}
	return false
}

// CountsTowardAnalysis 说明这一行该不该参与变量识别与归因。
// 只有被人明确否掉的推断出局；待复核、认可 AI、人工原创都算数。
func (s ReviewState) CountsTowardAnalysis() bool {
	return s != ReviewRejected
}

// MappingStatus tracks AM-003: a platform creative/ad either points at one of
// our asset revisions, sits in the 待匹配 queue, or was deliberately ignored.
type MappingStatus string

const (
	MappingUnmatched MappingStatus = "unmatched"
	MappingMatched   MappingStatus = "matched"
	MappingIgnored   MappingStatus = "ignored"
)

func (s MappingStatus) valid() bool {
	switch s {
	case MappingUnmatched, MappingMatched, MappingIgnored:
		return true
	}
	return false
}

// Asset is one analysable creative revision (AM-001). Identity is the stable
// id/lineage pair, never the file name.
type Asset struct {
	ID             string                  `json:"id"`
	OrganizationID contract.OrganizationID `json:"organization_id"`
	ProjectID      contract.ProjectID      `json:"project_id"`

	// 台账还是分析对象。登记时不填默认是分析对象——现有的每一条都是分析对象。
	Role AssetRole `json:"role"`

	LineageID string `json:"lineage_id"`
	Revision  int    `json:"revision"`
	Title     string `json:"title"`

	SourceKind  AssetSourceKind `json:"source_kind"`
	SourceRef   string          `json:"source_ref,omitempty"`
	SourceJobID string          `json:"source_job_id,omitempty"`

	PlatformAssetID      string `json:"platform_asset_id,omitempty"`
	PlatformAssetVersion int64  `json:"platform_asset_version,omitempty"`

	AssetType           AssetType     `json:"asset_type,omitempty"`
	AssetTypeSource     FeatureSource `json:"asset_type_source,omitempty"`
	AssetTypeConfidence Confidence    `json:"asset_type_confidence,omitempty"`

	AnalysisStatus          AnalysisStatus `json:"analysis_status"`
	AnalysisStatusReason    string         `json:"analysis_status_reason,omitempty"`
	AnalysisStatusChangedAt *time.Time     `json:"analysis_status_changed_at,omitempty"`

	Version   int64     `json:"version"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TypeIdentified reports whether AM-004 has produced an answer. Extraction may
// not start before it has: without a type there is no feature system to fill.
func (a Asset) TypeIdentified() bool { return a.AssetType.valid() }

// AssetMapping links a platform creative/ad object to one asset revision.
// A row with no asset is the 待匹配 queue item AM-003 requires.
type AssetMapping struct {
	ID             string                  `json:"id"`
	OrganizationID contract.OrganizationID `json:"organization_id"`
	ProjectID      contract.ProjectID      `json:"project_id"`

	Platform           string `json:"platform"`
	PlatformObjectKind string `json:"platform_object_kind"`
	PlatformObjectID   string `json:"platform_object_id"`
	PlatformObjectName string `json:"platform_object_name,omitempty"`

	AssetID     string        `json:"asset_id,omitempty"`
	Status      MappingStatus `json:"status"`
	MatchSource string        `json:"match_source,omitempty"`
	MatchedBy   string        `json:"matched_by,omitempty"`
	MatchedAt   *time.Time    `json:"matched_at,omitempty"`
	Note        string        `json:"note,omitempty"`

	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FeatureValue carries one extracted value. Kind decides which member is
// meaningful, and must equal the Kind the asset type's schema declares for the
// key — that is what keeps a 时长 from being stored as free text.
type FeatureValue struct {
	Kind   FeatureValueKind `json:"kind"`
	Text   string           `json:"text,omitempty"`
	Terms  []string         `json:"terms,omitempty"`
	Number float64          `json:"number,omitempty"`
	Bool   bool             `json:"bool,omitempty"`
}

// AssetFeature is one feature of one asset in one data layer. The AI inference
// and the human conclusion for the same key are two separate rows (03 §14).
type AssetFeature struct {
	ID             string                  `json:"id"`
	OrganizationID contract.OrganizationID `json:"organization_id"`
	ProjectID      contract.ProjectID      `json:"project_id"`
	AssetID        string                  `json:"asset_id"`

	AssetType AssetType    `json:"asset_type"`
	Key       string       `json:"key"`
	Value     FeatureValue `json:"value"`

	Source      FeatureSource `json:"source"`
	Confidence  Confidence    `json:"confidence,omitempty"`
	ReviewState ReviewState   `json:"review_state"`

	SkillID      string     `json:"skill_id,omitempty"`
	SkillVersion string     `json:"skill_version,omitempty"`
	ExtractedAt  *time.Time `json:"extracted_at,omitempty"`

	Version   int64     `json:"version"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// FeatureInput is one value the caller wants written.
type FeatureInput struct {
	Key   string       `json:"key"`
	Value FeatureValue `json:"value"`
	// Confidence is required on the AI layer and forbidden on the human layer.
	Confidence Confidence `json:"confidence,omitempty"`
	// ReviewState only applies when a human is deciding on an AI inference.
	ReviewState ReviewState `json:"review_state,omitempty"`
}

// validate checks the value against the asset type's own feature system. It is
// the service-boundary form of the MVP §15② guard, so a bad write is rejected
// before it reaches the database rather than only in tests.
func (in FeatureInput) validate(assetType AssetType) error {
	key := strings.TrimSpace(in.Key)
	if key == "" {
		return fmt.Errorf("%w: 特征键不能为空", ErrInvalidRequest)
	}
	if err := ValidateFeatureValue(assetType, key, in.Value.Terms); err != nil {
		return err
	}
	schema, _ := FeatureSchemaFor(assetType)
	field, _ := schema.Field(key)
	if in.Value.Kind != field.Kind {
		return fmt.Errorf("%w: 特征 %q 的取值类型应为 %s，收到 %s",
			ErrInvalidRequest, key, field.Kind, in.Value.Kind)
	}
	switch field.Kind {
	case FeatureKindText:
		if strings.TrimSpace(in.Value.Text) == "" || len(in.Value.Text) > 2000 {
			return fmt.Errorf("%w: 特征 %q 的文本取值为空或超长", ErrInvalidRequest, key)
		}
	case FeatureKindTags, FeatureKindEnum, FeatureKindEnumMul:
		if len(in.Value.Terms) == 0 || len(in.Value.Terms) > 50 {
			return fmt.Errorf("%w: 特征 %q 的取值项数量应在 1..50 之间", ErrInvalidRequest, key)
		}
		for _, term := range in.Value.Terms {
			if strings.TrimSpace(term) == "" || len(term) > 100 {
				return fmt.Errorf("%w: 特征 %q 含有空或超长取值项", ErrInvalidRequest, key)
			}
		}
	case FeatureKindNumber, FeatureKindDuration:
		if in.Value.Number < 0 {
			return fmt.Errorf("%w: 特征 %q 的数值不能为负", ErrInvalidRequest, key)
		}
	}
	return nil
}

// IndexAssetRequest registers a creative revision for analysis (AM-001).
// An empty LineageID starts a new lineage; a known one appends a revision, so
// the same creative keeps one identity across edits.
type IndexAssetRequest struct {
	Title      string          `json:"title"`
	SourceKind AssetSourceKind `json:"source_kind"`

	// Role 留空就是分析对象——手工登记的素材默认是要拿去投的。
	// 后台自动收录的台账素材由 RecordLedgerAsset 显式填 ledger。
	Role AssetRole `json:"role"`

	SourceRef   string `json:"source_ref"`
	SourceJobID string `json:"source_job_id"`
	LineageID   string `json:"lineage_id"`

	PlatformAssetID      string `json:"platform_asset_id"`
	PlatformAssetVersion int64  `json:"platform_asset_version"`

	// AssetType may be supplied when the caller already knows it (a creative
	// job that produced a 数字人 video). Leaving it empty means 待识别.
	AssetType           AssetType     `json:"asset_type"`
	AssetTypeSource     FeatureSource `json:"asset_type_source"`
	AssetTypeConfidence Confidence    `json:"asset_type_confidence"`
}

func (r IndexAssetRequest) validate() error {
	title := strings.TrimSpace(r.Title)
	if title == "" || len(title) > 255 {
		return fmt.Errorf("%w: 素材标题为空或超长", ErrInvalidRequest)
	}
	if !r.SourceKind.valid() {
		return fmt.Errorf("%w: 素材来源必须是 creative、upload 或 external", ErrInvalidRequest)
	}
	if r.Role != "" && !r.Role.valid() {
		return fmt.Errorf("%w: 素材身份必须是 ledger 或 analysis", ErrInvalidRequest)
	}
	if len(r.SourceRef) > 512 || len(r.LineageID) > 96 {
		return ErrInvalidRequest
	}
	if (r.PlatformAssetID == "") != (r.PlatformAssetVersion == 0) {
		return fmt.Errorf("%w: 媒体资产引用要么同时给出 ID 与版本，要么都不给", ErrInvalidRequest)
	}
	if r.AssetType == AssetTypeUnknown {
		if r.AssetTypeSource != "" || r.AssetTypeConfidence != "" {
			return fmt.Errorf("%w: 未给出素材类型时不得声明识别来源或置信度", ErrInvalidRequest)
		}
		return nil
	}
	return validateTypeIdentification(r.AssetType, r.AssetTypeSource, r.AssetTypeConfidence)
}

// IdentifyAssetTypeRequest records the AM-004 answer for one asset.
type IdentifyAssetTypeRequest struct {
	ExpectedVersion int64         `json:"expected_version"`
	AssetType       AssetType     `json:"asset_type"`
	Source          FeatureSource `json:"source"`
	Confidence      Confidence    `json:"confidence"`
	Reason          string        `json:"reason"`
}

func (r IdentifyAssetTypeRequest) validate() error {
	if len(strings.TrimSpace(r.Reason)) > 1000 {
		return ErrInvalidRequest
	}
	return validateTypeIdentification(r.AssetType, r.Source, r.Confidence)
}

// validateTypeIdentification enforces 03 §5 末: an AI answer must carry a
// confidence level, a human answer must not pretend to have one.
func validateTypeIdentification(assetType AssetType, source FeatureSource, confidence Confidence) error {
	if !assetType.valid() {
		return fmt.Errorf("%w: 素材类型必须是 03 §9 AM-004 中的六类之一", ErrInvalidRequest)
	}
	if !source.valid() {
		return fmt.Errorf("%w: 类型识别来源必须是 ai 或 human", ErrInvalidRequest)
	}
	if source == SourceAI && !confidence.valid() {
		return fmt.Errorf("%w: AI 识别必须给出置信级别", ErrInvalidRequest)
	}
	if source == SourceHuman && confidence != "" {
		return fmt.Errorf("%w: 人工识别不带机器置信度", ErrInvalidRequest)
	}
	return nil
}

// RegisterAssetMappingRequest ingests one platform object. It lands in the
// 待匹配 queue unless the caller can already point at an asset revision.
type RegisterAssetMappingRequest struct {
	Platform           string `json:"platform"`
	PlatformObjectKind string `json:"platform_object_kind"`
	PlatformObjectID   string `json:"platform_object_id"`
	PlatformObjectName string `json:"platform_object_name"`
	AssetID            string `json:"asset_id"`
	MatchSource        string `json:"match_source"`
	Note               string `json:"note"`
}

func (r RegisterAssetMappingRequest) validate() error {
	platform, kind, objectID := strings.TrimSpace(r.Platform), strings.TrimSpace(r.PlatformObjectKind), strings.TrimSpace(r.PlatformObjectID)
	if platform == "" || len(platform) > 32 || objectID == "" || len(objectID) > 191 ||
		len(r.PlatformObjectName) > 255 || len(strings.TrimSpace(r.Note)) > 1000 {
		return ErrInvalidRequest
	}
	if kind != "creative" && kind != "ad" {
		return fmt.Errorf("%w: 平台对象类型必须是 creative 或 ad", ErrInvalidRequest)
	}
	if r.AssetID == "" {
		return nil
	}
	if r.MatchSource != "auto" && r.MatchSource != "human" {
		return fmt.Errorf("%w: 已匹配的映射必须说明是自动还是人工匹配", ErrInvalidRequest)
	}
	return nil
}

// ResolveAssetMappingRequest is the human decision on one queue item (AM-003):
// point it at an asset revision, or set it aside with a reason.
type ResolveAssetMappingRequest struct {
	ExpectedVersion int64         `json:"expected_version"`
	Status          MappingStatus `json:"status"`
	AssetID         string        `json:"asset_id"`
	Note            string        `json:"note"`
}

func (r ResolveAssetMappingRequest) validate() error {
	if len(strings.TrimSpace(r.Note)) > 1000 {
		return ErrInvalidRequest
	}
	switch r.Status {
	case MappingMatched:
		if strings.TrimSpace(r.AssetID) == "" {
			return fmt.Errorf("%w: 匹配时必须指定素材版本", ErrInvalidRequest)
		}
		return nil
	case MappingUnmatched, MappingIgnored:
		if strings.TrimSpace(r.AssetID) != "" {
			return fmt.Errorf("%w: 未匹配或已忽略的映射不得保留素材指针", ErrInvalidRequest)
		}
		if r.Status == MappingIgnored && strings.TrimSpace(r.Note) == "" {
			return fmt.Errorf("%w: 忽略一条待匹配记录需要说明原因", ErrInvalidRequest)
		}
		return nil
	}
	return fmt.Errorf("%w: 映射状态必须是 unmatched、matched 或 ignored", ErrInvalidRequest)
}

// ExtractFeaturesRequest writes the AI layer (AM-005). Every value is stamped
// with the Skill that produced it, so a conclusion can be traced back.
type ExtractFeaturesRequest struct {
	ExpectedVersion int64          `json:"expected_version"`
	SkillID         string         `json:"skill_id"`
	SkillVersion    string         `json:"skill_version"`
	Features        []FeatureInput `json:"features"`
}

// PatchFeaturesRequest writes the human layer (AM-006). Confirming an AI value
// unchanged is expressed by repeating it with ReviewState=confirmed.
type PatchFeaturesRequest struct {
	ExpectedVersion int64          `json:"expected_version"`
	Features        []FeatureInput `json:"features"`
	Reason          string         `json:"reason"`
}

// AssetTransitionRequest carries the human reason an asset changed state.
type AssetTransitionRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

// AssetFilter drives the 分析素材库 views. Every visible L2 tab has to change
// the dataset (22 §8.3), so each one maps to a different filter here.
type AssetFilter struct {
	Statuses    []AnalysisStatus  `json:"statuses,omitempty"`
	AssetTypes  []AssetType       `json:"asset_types,omitempty"`
	SourceKinds []AssetSourceKind `json:"source_kinds,omitempty"`

	// Roles 留空等于「只看分析对象」。这条默认值是台账不淹没四个队列的唯一保证：
	// 忘了传的调用方拿到的是分析对象，不是几千条台账。
	Roles []AssetRole `json:"roles,omitempty"`

	LineageID string `json:"lineage_id,omitempty"`

	// Cursor 是上一页最后一条的位置，不透明串，由 ListAssetPage 发出。
	// 台账几千条起，offset 分页翻到第 50 页要数过前面 4900 行；游标只比一次索引。
	Cursor string `json:"cursor,omitempty"`
	// Query 按标题模糊搜。台账里绝大多数素材人只记得个名字，
	// 没有搜索的清单等于让人一页页翻——那就是查不动。
	Query string `json:"q,omitempty"`

	Limit int `json:"limit,omitempty"`
}

// AssetPage 是一页素材。NextCursor 为空表示到底了——
// 前端靠这一个信号决定「加载更多」还显不显示，不必自己数条数。
type AssetPage struct {
	Items      []Asset `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

// UnledgeredPlatformAsset 是平台素材库里有、但台账里还没有的一个素材版本。
// 回填命令按它一条条补，直到查不出来为止。
type UnledgeredPlatformAsset struct {
	OrganizationID contract.OrganizationID
	ProjectID      contract.ProjectID
	AssetID        string
	Version        int64
	SourceType     string
	CreatedAt      time.Time
}

// AssetMappingFilter drives the 待匹配 queue.
type AssetMappingFilter struct {
	Statuses []MappingStatus `json:"statuses,omitempty"`
	Platform string          `json:"platform,omitempty"`
	AssetID  string          `json:"asset_id,omitempty"`
	Limit    int             `json:"limit,omitempty"`
}

// AssetFeatureCoverage answers "how much of this asset's feature system is
// filled", which is what the 待提取 queue sorts on.
type AssetFeatureCoverage struct {
	AssetID string    `json:"asset_id"`
	Type    AssetType `json:"asset_type"`
	Filled  int       `json:"filled"`
	Total   int       `json:"total"`
}

// FeatureMatrixCell is the effective value of one feature for one asset: the
// human conclusion when one exists, otherwise the AI inference. Both layers
// stay visible so the reader can tell them apart (03 §10.3 防误读).
type FeatureMatrixCell struct {
	AssetID     string        `json:"asset_id"`
	Source      FeatureSource `json:"source"`
	Confidence  Confidence    `json:"confidence,omitempty"`
	ReviewState ReviewState   `json:"review_state"`
	Value       FeatureValue  `json:"value"`
}

// FeatureMatrixRow is one comparable variable across the selected assets.
type FeatureMatrixRow struct {
	Key   string              `json:"key"`
	Label string              `json:"label"`
	Group string              `json:"group"`
	Kind  FeatureValueKind    `json:"kind"`
	Cells []FeatureMatrixCell `json:"cells"`
}

// FeatureMatrix compares several assets on the features they actually share.
// Mixing types is allowed, but only the shared keys appear: putting 公众号
// 章节数 next to 视频钩子类型 would be a column that means nothing.
type FeatureMatrix struct {
	Assets     []Asset            `json:"assets"`
	AssetTypes []AssetType        `json:"asset_types"`
	Rows       []FeatureMatrixRow `json:"rows"`
	Disclosure string             `json:"disclosure"`
}

// UpdateAssetTypeInput records the AM-004 answer and the status move it causes
// in one transaction.
type UpdateAssetTypeInput struct {
	OrganizationID  contract.OrganizationID
	ProjectID       contract.ProjectID
	ID              string
	ExpectedVersion int64
	AssetType       AssetType
	Source          FeatureSource
	Confidence      Confidence
	From            []AnalysisStatus
	To              AnalysisStatus
	Reason          string
	Now             time.Time
}

// TransitionAssetInput moves one asset along the 03 §11.1 chain under an
// optimistic version check.
type TransitionAssetInput struct {
	OrganizationID  contract.OrganizationID
	ProjectID       contract.ProjectID
	ID              string
	ExpectedVersion int64
	From            []AnalysisStatus
	To              AnalysisStatus
	Reason          string
	Now             time.Time
}

// UpdateAssetRoleInput 只换身份，不碰 analysis_status。
// 两个维度分开写，是为了让「拉进分析 → 退回台账 → 再拉进分析」全程无损。
type UpdateAssetRoleInput struct {
	OrganizationID  contract.OrganizationID
	ProjectID       contract.ProjectID
	ID              string
	ExpectedVersion int64
	To              AssetRole
	Now             time.Time
}

// UpsertAssetFeaturesInput writes one data layer and advances the asset in the
// same transaction. Only rows in ReplaceLayer are touched, so re-extraction can
// never overwrite a human conclusion (AM-006).
type UpsertAssetFeaturesInput struct {
	OrganizationID  contract.OrganizationID
	ProjectID       contract.ProjectID
	AssetID         string
	ExpectedVersion int64
	ReplaceLayer    FeatureSource
	Features        []AssetFeature
	From            []AnalysisStatus
	To              AnalysisStatus
	Reason          string
	Now             time.Time
}

// AssetRepository is kept separate from Repository on purpose: 分析素材库 and
// 经验库 are different lifecycles with different tables, and splitting them lets
// either be implemented or faked without dragging in the other.
type AssetRepository interface {
	CreateAsset(context.Context, Asset) (Asset, error)
	ListAssets(context.Context, contract.OrganizationID, contract.ProjectID, AssetFilter) ([]Asset, error)
	GetAsset(context.Context, contract.OrganizationID, contract.ProjectID, string) (Asset, error)
	ListAssetLineage(context.Context, contract.OrganizationID, contract.ProjectID, string) ([]Asset, error)
	UpdateAssetType(context.Context, UpdateAssetTypeInput) (Asset, error)
	TransitionAsset(context.Context, TransitionAssetInput) (Asset, error)
	UpdateAssetRole(context.Context, UpdateAssetRoleInput) (Asset, error)
	ListAssetPage(context.Context, contract.OrganizationID, contract.ProjectID, AssetFilter) (AssetPage, error)
	ListUnledgeredPlatformAssets(context.Context, int) ([]UnledgeredPlatformAsset, error)

	CreateAssetMapping(context.Context, AssetMapping) (AssetMapping, error)
	ListAssetMappings(context.Context, contract.OrganizationID, contract.ProjectID, AssetMappingFilter) ([]AssetMapping, error)
	GetAssetMapping(context.Context, contract.OrganizationID, contract.ProjectID, string) (AssetMapping, error)
	ResolveAssetMapping(context.Context, AssetMapping, int64) (AssetMapping, error)

	UpsertAssetFeatures(context.Context, UpsertAssetFeaturesInput) ([]AssetFeature, error)
	ListAssetFeatures(context.Context, contract.OrganizationID, contract.ProjectID, []string, int) ([]AssetFeature, error)
	CountAssetFeaturesByReviewState(context.Context, contract.OrganizationID, contract.ProjectID, string, ReviewState) (int, error)
}

// PosterReader 把一个平台素材版本的封面换成一个可以直接放进 <img src> 的地址。
//
// 洞察自己不抽帧、不存图：那是素材库的事。这里只要一个能看的地址。
type PosterReader interface {
	ReadPoster(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, platformAssetID string, platformAssetVersion int64) (string, error)
}

// IndexAsset registers a creative revision so it can be analysed (AM-001).
func (s Service) IndexAsset(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, request IndexAssetRequest) (Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return Asset{}, err
	}
	if err := request.validate(); err != nil {
		return Asset{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return Asset{}, err
	}
	id, err := s.idGenerator()("insightasset")
	if err != nil {
		return Asset{}, err
	}
	lineageID, revision := id, 1
	if trimmed := strings.TrimSpace(request.LineageID); trimmed != "" {
		existing, listErr := s.Assets.ListAssetLineage(ctx, actor.OrganizationID, projectID, trimmed)
		if listErr != nil {
			return Asset{}, listErr
		}
		if len(existing) == 0 {
			return Asset{}, fmt.Errorf("%w: 素材血缘 %q 不存在", ErrNotFound, trimmed)
		}
		lineageID = trimmed
		for _, item := range existing {
			if item.Revision >= revision {
				revision = item.Revision + 1
			}
		}
	}
	now := s.now()
	// A freshly indexed asset is 待数据 until a type is identified; without a
	// type there is no feature system to extract into (03 §11.1).
	status, reason := AnalysisAwaitingData, "已登记，等待类型识别与投放数据。"
	if request.AssetType.valid() {
		status, reason = AnalysisAnalysable, "登记时已知类型，可开始特征提取。"
	}
	// 手工登记默认是分析对象；台账收录由调用方显式声明 ledger。
	role := request.Role
	if role == "" {
		role = AssetRoleAnalysis
	}
	return s.Assets.CreateAsset(ctx, Asset{
		ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID,
		Role:      role,
		LineageID: lineageID, Revision: revision, Title: strings.TrimSpace(request.Title),
		SourceKind: request.SourceKind, SourceRef: strings.TrimSpace(request.SourceRef),
		SourceJobID:     strings.TrimSpace(request.SourceJobID),
		PlatformAssetID: request.PlatformAssetID, PlatformAssetVersion: request.PlatformAssetVersion,
		AssetType: request.AssetType, AssetTypeSource: request.AssetTypeSource,
		AssetTypeConfidence:  request.AssetTypeConfidence,
		AnalysisStatus:       status,
		AnalysisStatusReason: reason, AnalysisStatusChangedAt: &now,
		Version: 1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	})
}

func (s Service) ListAssets(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, filter AssetFilter) ([]Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	filter, err := normalizeAssetFilter(filter)
	if err != nil {
		return nil, err
	}
	filter.Limit = normalizeLimit(filter.Limit)
	return s.Assets.ListAssets(ctx, actor.OrganizationID, projectID, filter)
}

// ListAssetPage 是台账清单的取数口。ListAssets 一次取完的做法留给分析对象那几十条，
// 台账不能这么取——它和平台素材库一样大。
func (s Service) ListAssetPage(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, filter AssetFilter) (AssetPage, error) {
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return AssetPage{}, err
	}
	filter, err := normalizeAssetFilter(filter)
	if err != nil {
		return AssetPage{}, err
	}
	if filter.Limit < 1 || filter.Limit > assetPageMaxLimit {
		filter.Limit = assetPageDefaultLimit
	}
	return s.Assets.ListAssetPage(ctx, actor.OrganizationID, projectID, filter)
}

// ReadAssetPoster 取一条素材的封面地址。
//
// 取不到就报 ErrNotFound，让前端退回类型图标。封面是锦上添花——
// 为了一张缩略图让整个清单打不开是本末倒置。
func (s Service) ReadAssetPoster(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string) (string, error) {
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return "", err
	}
	asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return "", err
	}
	if asset.PlatformAssetID == "" || asset.PlatformAssetVersion == 0 {
		return "", fmt.Errorf("%w: 这条素材没有对应的平台文件，没有封面可取", ErrNotFound)
	}
	if s.Posters == nil {
		// 也报「没有」而不是报故障：这个环境没配 ffmpeg，抽帧链路整条是关的，
		// 那就是真的没有封面。报 500 的话，一屏几十张缩略图会在日志和监控里
		// 刷出几十条服务器错误，看起来像出事了，其实只是没开这个功能。
		return "", fmt.Errorf("%w: 这个环境没有接封面服务（多半是没配 ffmpeg）", ErrNotFound)
	}
	return s.Posters.ReadPoster(ctx, actor, projectID, asset.PlatformAssetID, asset.PlatformAssetVersion)
}

const (
	assetPageDefaultLimit = 50
	assetPageMaxLimit     = 100
)

// normalizeAssetFilter 校验筛选条件并补上默认身份。两个取数口共用一份，
// 免得哪天只在其中一个上加了校验，另一个成了后门。
func normalizeAssetFilter(filter AssetFilter) (AssetFilter, error) {
	for _, status := range filter.Statuses {
		if !status.valid() {
			return filter, fmt.Errorf("%w: 未知的分析状态 %q", ErrInvalidRequest, string(status))
		}
	}
	for _, assetType := range filter.AssetTypes {
		if !assetType.valid() {
			return filter, fmt.Errorf("%w: 未知的素材类型 %q", ErrInvalidRequest, string(assetType))
		}
	}
	for _, kind := range filter.SourceKinds {
		if !kind.valid() {
			return filter, fmt.Errorf("%w: 未知的素材来源 %q", ErrInvalidRequest, string(kind))
		}
	}
	for _, role := range filter.Roles {
		if !role.valid() {
			return filter, fmt.Errorf("%w: 未知的素材身份 %q", ErrInvalidRequest, string(role))
		}
	}
	// 不传身份就是只看分析对象。台账动辄几千条，默认漏进四个队列会把它们淹掉。
	if len(filter.Roles) == 0 {
		filter.Roles = []AssetRole{AssetRoleAnalysis}
	}
	if len(filter.Query) > 255 {
		return filter, fmt.Errorf("%w: 搜索词过长", ErrInvalidRequest)
	}
	return filter, nil
}

func (s Service) GetAsset(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string) (Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return Asset{}, err
	}
	return s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
}

// ListAssetLineage returns every revision of one creative oldest first, which
// is what the 版本与血缘 view needs to show how a creative evolved (AM-001).
func (s Service) ListAssetLineage(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string) ([]Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return nil, err
	}
	return s.Assets.ListAssetLineage(ctx, actor.OrganizationID, projectID, asset.LineageID)
}

// IdentifyAssetType answers AM-004 and unlocks extraction. Re-identifying is
// allowed while the analysis has not been confirmed, because a wrong type
// would otherwise lock the asset into the wrong feature system forever.
func (s Service) IdentifyAssetType(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request IdentifyAssetTypeRequest) (Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return Asset{}, err
	}
	if err := request.validate(); err != nil {
		return Asset{}, err
	}
	current, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return Asset{}, err
	}
	if current.AssetType.valid() && current.AssetType != request.AssetType {
		// Changing the type would orphan every feature already extracted under
		// the old feature system, so it is only allowed before any of them exist.
		count, countErr := s.Assets.CountAssetFeaturesByReviewState(ctx, actor.OrganizationID, projectID, assetID, "")
		if countErr != nil {
			return Asset{}, countErr
		}
		if count > 0 {
			return Asset{}, fmt.Errorf("%w: 该素材已按%s提取过特征，改判类型需先清空特征",
				ErrInvalidState, current.AssetType.Label())
		}
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = fmt.Sprintf("识别为%s。", request.AssetType.Label())
	}
	return s.Assets.UpdateAssetType(ctx, UpdateAssetTypeInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, ID: assetID,
		ExpectedVersion: request.ExpectedVersion,
		AssetType:       request.AssetType, Source: request.Source, Confidence: request.Confidence,
		From: []AnalysisStatus{AnalysisAwaitingData, AnalysisAwaitingMatch, AnalysisAnalysable},
		To:   AnalysisAnalysable, Reason: reason, Now: s.now(),
	})
}

// RegisterAssetMapping puts one platform object into the AM-003 queue.
func (s Service) RegisterAssetMapping(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, request RegisterAssetMappingRequest) (AssetMapping, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return AssetMapping{}, err
	}
	if err := request.validate(); err != nil {
		return AssetMapping{}, err
	}
	now := s.now()
	value := AssetMapping{
		OrganizationID: actor.OrganizationID, ProjectID: projectID,
		Platform:           strings.TrimSpace(request.Platform),
		PlatformObjectKind: strings.TrimSpace(request.PlatformObjectKind),
		PlatformObjectID:   strings.TrimSpace(request.PlatformObjectID),
		PlatformObjectName: strings.TrimSpace(request.PlatformObjectName),
		Status:             MappingUnmatched, Note: strings.TrimSpace(request.Note),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if assetID := strings.TrimSpace(request.AssetID); assetID != "" {
		if _, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID); err != nil {
			return AssetMapping{}, err
		}
		value.AssetID = assetID
		value.Status = MappingMatched
		value.MatchSource = request.MatchSource
		value.MatchedBy = actor.Principal.ID
		value.MatchedAt = &now
	}
	id, err := s.idGenerator()("assetmapping")
	if err != nil {
		return AssetMapping{}, err
	}
	value.ID = id
	return s.Assets.CreateAssetMapping(ctx, value)
}

func (s Service) ListAssetMappings(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, filter AssetMappingFilter) ([]AssetMapping, error) {
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	for _, status := range filter.Statuses {
		if !status.valid() {
			return nil, fmt.Errorf("%w: 未知的映射状态 %q", ErrInvalidRequest, string(status))
		}
	}
	filter.Limit = normalizeLimit(filter.Limit)
	return s.Assets.ListAssetMappings(ctx, actor.OrganizationID, projectID, filter)
}

// ResolveAssetMapping is the human decision AM-003 requires when automatic
// matching cannot tell which asset a platform object refers to.
func (s Service) ResolveAssetMapping(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, mappingID string, request ResolveAssetMappingRequest) (AssetMapping, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return AssetMapping{}, err
	}
	if err := request.validate(); err != nil {
		return AssetMapping{}, err
	}
	current, err := s.Assets.GetAssetMapping(ctx, actor.OrganizationID, projectID, mappingID)
	if err != nil {
		return AssetMapping{}, err
	}
	if current.Version != request.ExpectedVersion {
		return AssetMapping{}, ErrVersionConflict
	}
	now := s.now()
	next := current
	next.Status = request.Status
	next.Note = strings.TrimSpace(request.Note)
	next.UpdatedAt = now
	switch request.Status {
	case MappingMatched:
		assetID := strings.TrimSpace(request.AssetID)
		if _, getErr := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID); getErr != nil {
			return AssetMapping{}, getErr
		}
		next.AssetID = assetID
		next.MatchSource = "human"
		next.MatchedBy = actor.Principal.ID
		next.MatchedAt = &now
	default:
		next.AssetID = ""
		next.MatchSource = ""
		next.MatchedBy = ""
		next.MatchedAt = nil
	}
	return s.Assets.ResolveAssetMapping(ctx, next, request.ExpectedVersion)
}

// ExtractFeatures writes the AI layer for one asset (AM-005) and moves it to
// 待确认. It never touches the human layer.
func (s Service) ExtractFeatures(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request ExtractFeaturesRequest) ([]AssetFeature, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return nil, err
	}
	asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return nil, err
	}
	if err := s.requireAnalysisRole(asset); err != nil {
		return nil, err
	}
	if !asset.TypeIdentified() {
		return nil, fmt.Errorf("%w: 素材类型待识别，无法确定要提取哪套特征", ErrInvalidState)
	}
	if len(request.Features) == 0 || len(request.Features) > 200 {
		return nil, fmt.Errorf("%w: 单次提取的特征数量应在 1..200 之间", ErrInvalidRequest)
	}
	if strings.TrimSpace(request.SkillID) == "" {
		return nil, fmt.Errorf("%w: AI 提取必须记录产出它的 Skill", ErrInvalidRequest)
	}
	now := s.now()
	features := make([]AssetFeature, 0, len(request.Features))
	for _, input := range request.Features {
		if err := input.validate(asset.AssetType); err != nil {
			return nil, err
		}
		if !input.Confidence.valid() {
			return nil, fmt.Errorf("%w: 特征 %q 缺少 AI 置信级别", ErrInvalidRequest, input.Key)
		}
		id, idErr := s.idGenerator()("assetfeature")
		if idErr != nil {
			return nil, idErr
		}
		extractedAt := now
		features = append(features, AssetFeature{
			ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID, AssetID: asset.ID,
			AssetType: asset.AssetType, Key: strings.TrimSpace(input.Key), Value: input.Value,
			Source: SourceAI, Confidence: input.Confidence, ReviewState: ReviewPending,
			SkillID: strings.TrimSpace(request.SkillID), SkillVersion: strings.TrimSpace(request.SkillVersion),
			ExtractedAt: &extractedAt,
			Version:     1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
		})
	}
	return s.Assets.UpsertAssetFeatures(ctx, UpsertAssetFeaturesInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, AssetID: asset.ID,
		ExpectedVersion: request.ExpectedVersion, ReplaceLayer: SourceAI, Features: features,
		// 分析中 is reserved for the asynchronous extraction job that does not
		// exist yet; the synchronous write path goes 可分析 → 待确认 directly.
		From: []AnalysisStatus{AnalysisAnalysable, AnalysisAnalysing, AnalysisPendingConfirmation, AnalysisNeedsReview},
		To:   AnalysisPendingConfirmation,
		Reason: fmt.Sprintf("Skill %s 提取了 %d 项特征，等待人工确认。",
			strings.TrimSpace(request.SkillID), len(features)),
		Now: now,
	})
}

// PatchFeatures writes the human layer (AM-006). The AI row for the same key
// stays where it is, so the two conclusions remain comparable and the machine
// output is never silently rewritten as a human judgement.
func (s Service) PatchFeatures(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request PatchFeaturesRequest) ([]AssetFeature, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return nil, err
	}
	asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return nil, err
	}
	if err := s.requireAnalysisRole(asset); err != nil {
		return nil, err
	}
	if !asset.TypeIdentified() {
		return nil, fmt.Errorf("%w: 素材类型待识别，无法确定要修改哪套特征", ErrInvalidState)
	}
	if len(request.Features) == 0 || len(request.Features) > 200 {
		return nil, fmt.Errorf("%w: 单次修改的特征数量应在 1..200 之间", ErrInvalidRequest)
	}
	now := s.now()
	features := make([]AssetFeature, 0, len(request.Features))
	for _, input := range request.Features {
		if err := input.validate(asset.AssetType); err != nil {
			return nil, err
		}
		if input.Confidence != "" {
			return nil, fmt.Errorf("%w: 人工结论不带机器置信度", ErrInvalidRequest)
		}
		state := input.ReviewState
		if state == "" {
			state = ReviewConfirmed
		}
		if !state.valid() || state == ReviewPending {
			return nil, fmt.Errorf("%w: 人工结论的复核状态只能是 confirmed、rejected 或 authored", ErrInvalidRequest)
		}
		id, idErr := s.idGenerator()("assetfeature")
		if idErr != nil {
			return nil, idErr
		}
		features = append(features, AssetFeature{
			ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID, AssetID: asset.ID,
			AssetType: asset.AssetType, Key: strings.TrimSpace(input.Key), Value: input.Value,
			Source: SourceHuman, ReviewState: state,
			Version: 1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
		})
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = fmt.Sprintf("人工修改了 %d 项特征。", len(features))
	}
	return s.Assets.UpsertAssetFeatures(ctx, UpsertAssetFeaturesInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, AssetID: asset.ID,
		ExpectedVersion: request.ExpectedVersion, ReplaceLayer: SourceHuman, Features: features,
		From: []AnalysisStatus{AnalysisAnalysable, AnalysisAnalysing, AnalysisPendingConfirmation, AnalysisConfirmed, AnalysisNeedsReview},
		To:   AnalysisPendingConfirmation, Reason: reason, Now: now,
	})
}

func (s Service) ListAssetFeatures(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string) ([]AssetFeature, error) {
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	if _, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID); err != nil {
		return nil, err
	}
	return s.Assets.ListAssetFeatures(ctx, actor.OrganizationID, projectID, []string{assetID}, 0)
}

// ConfirmAssetAnalysis is AM-006's closing step: it refuses while any AI value
// is still unreviewed, so 已确认 always means a person looked at every field.
func (s Service) ConfirmAssetAnalysis(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request AssetTransitionRequest) (Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeConfirm); err != nil {
		return Asset{}, err
	}
	pending, err := s.Assets.CountAssetFeaturesByReviewState(ctx, actor.OrganizationID, projectID, assetID, ReviewPending)
	if err != nil {
		return Asset{}, err
	}
	if pending > 0 {
		return Asset{}, fmt.Errorf("%w: 还有 %d 项 AI 特征未经人工复核", ErrInvalidState, pending)
	}
	total, err := s.Assets.CountAssetFeaturesByReviewState(ctx, actor.OrganizationID, projectID, assetID, "")
	if err != nil {
		return Asset{}, err
	}
	if total == 0 {
		return Asset{}, fmt.Errorf("%w: 该素材还没有任何特征可确认", ErrInvalidState)
	}
	return s.transitionAsset(ctx, actor, projectID, assetID, request,
		[]AnalysisStatus{AnalysisPendingConfirmation, AnalysisNeedsReview}, AnalysisConfirmed, false,
		"分析结论已确认。")
}

// RequestAssetReview flags a confirmed analysis when new data challenges it,
// instead of silently overwriting the conclusion (03 §11.1).
func (s Service) RequestAssetReview(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request AssetTransitionRequest) (Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return Asset{}, err
	}
	return s.transitionAsset(ctx, actor, projectID, assetID, request,
		[]AnalysisStatus{AnalysisConfirmed}, AnalysisNeedsReview, true, "")
}

// RetireAsset is the logical delete: the row and its features stay readable so
// anything that already quoted them remains auditable (03 §11.2).
func (s Service) RetireAsset(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request AssetTransitionRequest) (Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeConfirm); err != nil {
		return Asset{}, err
	}
	return s.transitionAsset(ctx, actor, projectID, assetID, request,
		[]AnalysisStatus{AnalysisAwaitingData, AnalysisAwaitingMatch, AnalysisAnalysable, AnalysisAnalysing,
			AnalysisPendingConfirmation, AnalysisConfirmed, AnalysisNeedsReview},
		AnalysisRetired, true, "")
}

func (s Service) transitionAsset(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request AssetTransitionRequest, from []AnalysisStatus, to AnalysisStatus, reasonRequired bool, fallbackReason string) (Asset, error) {
	reason := strings.TrimSpace(request.Reason)
	if len(reason) > 1000 || (reasonRequired && reason == "") {
		return Asset{}, fmt.Errorf("%w: 该状态变更需要填写原因", ErrInvalidRequest)
	}
	if reason == "" {
		reason = fallbackReason
	}
	return s.Assets.TransitionAsset(ctx, TransitionAssetInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, ID: assetID,
		ExpectedVersion: request.ExpectedVersion, From: from, To: to, Reason: reason, Now: s.now(),
	})
}

// PromoteAssetToAnalysis 把一条台账素材拉进分析。
//
// 这是唯一一条从台账进分析的路，而且必须有人点：台账里绝大多数素材永远不会投流，
// 自动往里拉只会把四个队列重新灌满。
func (s Service) PromoteAssetToAnalysis(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request AssetTransitionRequest) (Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return Asset{}, err
	}
	if len(strings.TrimSpace(request.Reason)) > 1000 {
		return Asset{}, fmt.Errorf("%w: 原因超长", ErrInvalidRequest)
	}
	asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return Asset{}, err
	}
	if asset.Role == AssetRoleAnalysis {
		return Asset{}, fmt.Errorf("%w: 这条素材已经是分析对象", ErrInvalidState)
	}
	return s.Assets.UpdateAssetRole(ctx, UpdateAssetRoleInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, ID: assetID,
		ExpectedVersion: request.ExpectedVersion, To: AssetRoleAnalysis, Now: s.now(),
	})
}

// ReturnAssetToLedger 把拉错的素材退回台账。
//
// 只有从没对上号的素材能退：对上号意味着它有广告对象、有花费、进过归因，
// 这时候退回台账就是把已经产生的数据从队列里藏起来。
func (s Service) ReturnAssetToLedger(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string, request AssetTransitionRequest) (Asset, error) {
	if err := s.assetsReady(actor, projectID, ScopeWrite); err != nil {
		return Asset{}, err
	}
	if len(strings.TrimSpace(request.Reason)) > 1000 {
		return Asset{}, fmt.Errorf("%w: 原因超长", ErrInvalidRequest)
	}
	asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return Asset{}, err
	}
	if asset.Role == AssetRoleLedger {
		return Asset{}, fmt.Errorf("%w: 这条素材本来就在台账里", ErrInvalidState)
	}
	matched, err := s.Assets.ListAssetMappings(ctx, actor.OrganizationID, projectID, AssetMappingFilter{
		AssetID: assetID, Statuses: []MappingStatus{MappingMatched}, Limit: 1,
	})
	if err != nil {
		return Asset{}, err
	}
	if len(matched) > 0 {
		return Asset{}, fmt.Errorf("%w: 这条素材已经和广告对象对上号，有投放数据，不能退回台账", ErrInvalidState)
	}
	return s.Assets.UpdateAssetRole(ctx, UpdateAssetRoleInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, ID: assetID,
		ExpectedVersion: request.ExpectedVersion, To: AssetRoleLedger, Now: s.now(),
	})
}

// GetFeatureMatrix compares several assets on the features they share. Assets
// of different types only line up on their common keys — anything else would
// be a column that puts unrelated variables side by side (MVP §15②).
func (s Service) GetFeatureMatrix(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetIDs []string) (FeatureMatrix, error) {
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return FeatureMatrix{}, err
	}
	if len(assetIDs) == 0 || len(assetIDs) > 50 {
		return FeatureMatrix{}, fmt.Errorf("%w: 特征矩阵一次比较 1..50 个素材", ErrInvalidRequest)
	}
	assets := make([]Asset, 0, len(assetIDs))
	types := make([]AssetType, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
		if err != nil {
			return FeatureMatrix{}, err
		}
		if !asset.TypeIdentified() {
			return FeatureMatrix{}, fmt.Errorf("%w: 素材 %q 类型待识别，无法参与特征对比", ErrInvalidState, assetID)
		}
		assets = append(assets, asset)
		types = append(types, asset.AssetType)
	}
	features, err := s.Assets.ListAssetFeatures(ctx, actor.OrganizationID, projectID, assetIDs, 0)
	if err != nil {
		return FeatureMatrix{}, err
	}
	return buildFeatureMatrix(assets, types, features), nil
}

func buildFeatureMatrix(assets []Asset, types []AssetType, features []AssetFeature) FeatureMatrix {
	shared := SharedFeatureKeys(types...)
	// The human conclusion wins over the AI inference for the same key, but the
	// cell keeps saying which layer it came from (03 §10.3 防误读).
	effective := make(map[string]map[string]AssetFeature, len(assets))
	for _, feature := range features {
		byKey, ok := effective[feature.AssetID]
		if !ok {
			byKey = make(map[string]AssetFeature, len(shared))
			effective[feature.AssetID] = byKey
		}
		if existing, seen := byKey[feature.Key]; seen && existing.Source == SourceHuman {
			continue
		}
		byKey[feature.Key] = feature
	}
	reference, _ := FeatureSchemaFor(types[0])
	rows := make([]FeatureMatrixRow, 0, len(shared))
	for _, key := range shared {
		field, ok := reference.Field(key)
		if !ok {
			continue
		}
		// 没有任何素材填过的行也要留在矩阵里，好看出「这一项大家都没提」。
		// Cells 显式建成空切片而不是 nil，否则 JSON 里会变成 null，前端得再判一次空。
		row := FeatureMatrixRow{Key: key, Label: field.Label, Group: field.Group, Kind: field.Kind, Cells: []FeatureMatrixCell{}}
		for _, asset := range assets {
			feature, ok := effective[asset.ID][key]
			if !ok {
				continue
			}
			row.Cells = append(row.Cells, FeatureMatrixCell{
				AssetID: asset.ID, Source: feature.Source, Confidence: feature.Confidence,
				ReviewState: feature.ReviewState, Value: feature.Value,
			})
		}
		rows = append(rows, row)
	}
	distinct := distinctTypes(types)
	disclosure := "标注为 AI 的取值是模型推断，需人工确认后才可作为结论使用。"
	if len(distinct) > 1 {
		disclosure += "所选素材类型不同，仅比较各类型都有的共同特征。"
	}
	return FeatureMatrix{Assets: assets, AssetTypes: distinct, Rows: rows, Disclosure: disclosure}
}

func distinctTypes(values []AssetType) []AssetType {
	seen := make(map[AssetType]struct{}, len(values))
	result := make([]AssetType, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// CoverageOf reports how complete one asset's feature set is, counting the
// effective value per key so an AI value a human overwrote is not counted twice.
func CoverageOf(asset Asset, features []AssetFeature) AssetFeatureCoverage {
	keys := make([]string, 0, len(features))
	seen := make(map[string]struct{}, len(features))
	for _, feature := range features {
		if feature.AssetID != asset.ID {
			continue
		}
		if _, ok := seen[feature.Key]; ok {
			continue
		}
		seen[feature.Key] = struct{}{}
		keys = append(keys, feature.Key)
	}
	filled, total := FeatureCoverage(asset.AssetType, keys)
	return AssetFeatureCoverage{AssetID: asset.ID, Type: asset.AssetType, Filled: filled, Total: total}
}

// assetsReady mirrors ready() but checks the asset repository instead of the
// experience one, so a deployment can wire either half independently.
func (s Service) assetsReady(actor contract.ActorContext, projectID contract.ProjectID, scope contract.Scope) error {
	if s.Assets == nil || s.Projects == nil {
		return fmt.Errorf("insights asset dependencies are incomplete")
	}
	if actor.OrganizationID == "" || projectID == "" || !actor.HasScope(scope) {
		return fmt.Errorf("%s scope is required", scope)
	}
	return nil
}

// requireAnalysisRole 挡住往台账素材上写特征的一切路径。
//
// 台账是账本：登记在册就够了，它不进队列、不跑归因、不该有任何特征。
// 四条写路径（人工修改、AI 提取、量客观变量、单条分析）各自都要过这道门——
// 少一条，那条路径就成了绕过身份的后门。
func (s Service) requireAnalysisRole(asset Asset) error {
	if asset.Role == AssetRoleLedger {
		return fmt.Errorf("%w: 这条素材在台账里，先「拉进分析」才能提特征", ErrInvalidState)
	}
	return nil
}

func allowsAnalysisStatus(values []AnalysisStatus, value AnalysisStatus) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
