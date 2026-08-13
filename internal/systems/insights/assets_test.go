package insights

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

func TestIndexedAssetWaitsForTypeBeforeItCanBeAnalysed(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	asset, err := service.IndexAsset(context.Background(), actor, "project_1", IndexAssetRequest{
		Title: "夏季新品前贴 15s", SourceKind: AssetSourceCreative, SourceRef: "creativejob_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if asset.AnalysisStatus != AnalysisAwaitingData || asset.TypeIdentified() ||
		asset.LineageID != asset.ID || asset.Revision != 1 {
		t.Fatalf("未识别类型的素材应停在待数据：%#v", asset)
	}
	// 没有类型就没有特征体系，提取必须被拒绝。
	_, err = service.ExtractFeatures(context.Background(), actor, "project_1", asset.ID, ExtractFeaturesRequest{
		ExpectedVersion: asset.Version, SkillID: "skill_1",
		Features: []FeatureInput{{Key: "hook_type", Value: FeatureValue{Kind: FeatureKindEnumMul, Terms: []string{"利益"}}, Confidence: ConfidenceHigh}},
	})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("error=%v", err)
	}
	identified, err := service.IdentifyAssetType(context.Background(), actor, "project_1", asset.ID, IdentifyAssetTypeRequest{
		ExpectedVersion: asset.Version, AssetType: AssetTypePrerollAd, Source: SourceAI, Confidence: ConfidenceHigh,
	})
	if err != nil {
		t.Fatal(err)
	}
	if identified.AnalysisStatus != AnalysisAnalysable || identified.AssetType != AssetTypePrerollAd ||
		identified.Version != asset.Version+1 {
		t.Fatalf("识别类型后应进入可分析：%#v", identified)
	}
}

func TestAIIdentificationMustCarryConfidenceAndHumanMustNot(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	asset := indexedAsset(t, service, actor, AssetTypeUnknown)
	if _, err := service.IdentifyAssetType(context.Background(), actor, "project_1", asset.ID, IdentifyAssetTypeRequest{
		ExpectedVersion: asset.Version, AssetType: AssetTypeBrandAd, Source: SourceAI,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("AI 识别缺置信度应被拒：error=%v", err)
	}
	if _, err := service.IdentifyAssetType(context.Background(), actor, "project_1", asset.ID, IdentifyAssetTypeRequest{
		ExpectedVersion: asset.Version, AssetType: AssetTypeBrandAd, Source: SourceHuman, Confidence: ConfidenceHigh,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("人工识别不应带机器置信度：error=%v", err)
	}
}

// MVP 验收②：不把视频钩子字段套到公众号文章。这条在服务边界就要拦住，
// 而不是等到前端渲染出一个没有意义的对比列。
func TestVideoHookFeatureIsRejectedOnArticleAtServiceBoundary(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	article := indexedAsset(t, service, actor, AssetTypeWechatArticle)
	_, err := service.ExtractFeatures(context.Background(), actor, "project_1", article.ID, ExtractFeaturesRequest{
		ExpectedVersion: article.Version, SkillID: "skill_1",
		Features: []FeatureInput{{Key: "hook_type", Value: FeatureValue{Kind: FeatureKindEnumMul, Terms: []string{"利益"}}, Confidence: ConfidenceHigh}},
	})
	if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "不同类型的特征不可混用") {
		t.Fatalf("error=%v", err)
	}
	// 该素材自己的特征体系里的字段则应当通过。
	stored, err := service.ExtractFeatures(context.Background(), actor, "project_1", article.ID, ExtractFeaturesRequest{
		ExpectedVersion: article.Version, SkillID: "skill_1", SkillVersion: "v1",
		Features: []FeatureInput{{Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"知识"}}, Confidence: ConfidenceMedium}},
	})
	if err != nil || len(stored) != 1 || stored[0].Source != SourceAI || stored[0].ReviewState != ReviewPending {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

func TestExtractionRejectsOffVocabularyAndWrongValueKind(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	article := indexedAsset(t, service, actor, AssetTypeWechatArticle)
	cases := map[string]FeatureInput{
		"取值不在受控词表内":   {Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"随笔"}}, Confidence: ConfidenceHigh},
		"取值类型与字段声明不符": {Key: "article_type", Value: FeatureValue{Kind: FeatureKindText, Text: "知识"}, Confidence: ConfidenceHigh},
		"数值字段写成负数":    {Key: "section_count", Value: FeatureValue{Kind: FeatureKindNumber, Number: -1}, Confidence: ConfidenceHigh},
		"AI 提取缺置信级别":  {Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"知识"}}},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := service.ExtractFeatures(context.Background(), actor, "project_1", article.ID, ExtractFeaturesRequest{
				ExpectedVersion: article.Version, SkillID: "skill_1", Features: []FeatureInput{input},
			})
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

// AM-006 与 §14：人工结论与 AI 推断分层存储，重新提取只覆盖 AI 那一行。
func TestHumanConclusionSurvivesReExtraction(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	article := indexedAsset(t, service, actor, AssetTypeWechatArticle)
	extracted, err := service.ExtractFeatures(context.Background(), actor, "project_1", article.ID, ExtractFeaturesRequest{
		ExpectedVersion: article.Version, SkillID: "skill_1",
		Features: []FeatureInput{{Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"知识"}}, Confidence: ConfidenceLow}},
	})
	if err != nil {
		t.Fatal(err)
	}
	current := mustGetAsset(t, service, actor, article.ID)
	patched, err := service.PatchFeatures(context.Background(), actor, "project_1", article.ID, PatchFeaturesRequest{
		ExpectedVersion: current.Version,
		Features:        []FeatureInput{{Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"案例"}}}},
		Reason:          "通读全文后判断这是案例复盘，不是知识科普。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(extracted) != 1 || len(patched) != 2 {
		t.Fatalf("同一特征的 AI 层与人工层应各占一行：extracted=%d patched=%d", len(extracted), len(patched))
	}
	current = mustGetAsset(t, service, actor, article.ID)
	if _, err = service.ExtractFeatures(context.Background(), actor, "project_1", article.ID, ExtractFeaturesRequest{
		ExpectedVersion: current.Version, SkillID: "skill_2",
		Features: []FeatureInput{{Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"叙事"}}, Confidence: ConfidenceHigh}},
	}); err != nil {
		t.Fatal(err)
	}
	features, err := service.ListAssetFeatures(context.Background(), actor, "project_1", article.ID)
	if err != nil {
		t.Fatal(err)
	}
	byLayer := map[FeatureSource][]string{}
	for _, feature := range features {
		byLayer[feature.Source] = feature.Value.Terms
	}
	if len(features) != 2 || byLayer[SourceHuman][0] != "案例" || byLayer[SourceAI][0] != "叙事" {
		t.Fatalf("重新提取只应覆盖 AI 层：%#v", features)
	}
}

// 推翻 AI 判断同样算复核过：人工结论行上的 rejected 记录的是「我不认可机器的取值」，
// 而不是「这项还没人看」。
func TestOverturningAnAIValueAlsoCountsAsReviewed(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	article := indexedAsset(t, service, actor, AssetTypeWechatArticle)
	if _, err := service.ExtractFeatures(context.Background(), actor, "project_1", article.ID, ExtractFeaturesRequest{
		ExpectedVersion: article.Version, SkillID: "skill_1",
		Features: []FeatureInput{{Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"知识"}}, Confidence: ConfidenceLow}},
	}); err != nil {
		t.Fatal(err)
	}
	current := mustGetAsset(t, service, actor, article.ID)
	if _, err := service.PatchFeatures(context.Background(), actor, "project_1", article.ID, PatchFeaturesRequest{
		ExpectedVersion: current.Version,
		Features: []FeatureInput{{
			Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"案例"}},
			ReviewState: ReviewRejected,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	current = mustGetAsset(t, service, actor, article.ID)
	confirmed, err := service.ConfirmAssetAnalysis(context.Background(), actor, "project_1", article.ID, AssetTransitionRequest{
		ExpectedVersion: current.Version,
	})
	if err != nil || confirmed.AnalysisStatus != AnalysisConfirmed {
		t.Fatalf("confirmed=%#v err=%v", confirmed, err)
	}
}

func TestPatchFeaturesRejectsMachineConfidenceOnHumanLayer(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	article := indexedAsset(t, service, actor, AssetTypeWechatArticle)
	_, err := service.PatchFeatures(context.Background(), actor, "project_1", article.ID, PatchFeaturesRequest{
		ExpectedVersion: article.Version,
		Features: []FeatureInput{{
			Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"案例"}},
			Confidence: ConfidenceHigh,
		}},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error=%v", err)
	}
}

// AM-006：「已确认」必须意味着每一项 AI 结论都被人看过。
func TestAnalysisCannotBeConfirmedWhileAIValuesAreUnreviewed(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	article := indexedAsset(t, service, actor, AssetTypeWechatArticle)
	if _, err := service.ExtractFeatures(context.Background(), actor, "project_1", article.ID, ExtractFeaturesRequest{
		ExpectedVersion: article.Version, SkillID: "skill_1",
		Features: []FeatureInput{
			{Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"知识"}}, Confidence: ConfidenceLow},
			{Key: "section_count", Value: FeatureValue{Kind: FeatureKindNumber, Number: 5}, Confidence: ConfidenceHigh},
		},
	}); err != nil {
		t.Fatal(err)
	}
	current := mustGetAsset(t, service, actor, article.ID)
	if current.AnalysisStatus != AnalysisPendingConfirmation {
		t.Fatalf("提取后应进入待确认：%#v", current)
	}
	_, err := service.ConfirmAssetAnalysis(context.Background(), actor, "project_1", article.ID, AssetTransitionRequest{
		ExpectedVersion: current.Version,
	})
	if !errors.Is(err, ErrInvalidState) || !strings.Contains(err.Error(), "未经人工复核") {
		t.Fatalf("error=%v", err)
	}
	if _, err = service.PatchFeatures(context.Background(), actor, "project_1", article.ID, PatchFeaturesRequest{
		ExpectedVersion: current.Version,
		Features: []FeatureInput{
			{Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"知识"}}, ReviewState: ReviewConfirmed},
			{Key: "section_count", Value: FeatureValue{Kind: FeatureKindNumber, Number: 5}, ReviewState: ReviewConfirmed},
		},
	}); err != nil {
		t.Fatal(err)
	}
	current = mustGetAsset(t, service, actor, article.ID)
	confirmed, err := service.ConfirmAssetAnalysis(context.Background(), actor, "project_1", article.ID, AssetTransitionRequest{
		ExpectedVersion: current.Version,
	})
	if err != nil || confirmed.AnalysisStatus != AnalysisConfirmed {
		t.Fatalf("confirmed=%#v err=%v", confirmed, err)
	}
}

func TestConfirmingAnalysisRequiresConfirmScope(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	writer := testActor()
	writer.Scopes = []contract.Scope{ScopeRead, ScopeWrite}
	article := indexedAsset(t, service, testActor(), AssetTypeWechatArticle)
	_, err := service.ConfirmAssetAnalysis(context.Background(), writer, "project_1", article.ID, AssetTransitionRequest{
		ExpectedVersion: article.Version,
	})
	if err == nil || !strings.Contains(err.Error(), string(ScopeConfirm)) {
		t.Fatalf("error=%v", err)
	}
}

// AM-001：同一素材的多个版本共用一条血缘，版本号递增。
func TestNewRevisionJoinsTheSameLineage(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	first := indexedAsset(t, service, actor, AssetTypeBrandAd)
	second, err := service.IndexAsset(context.Background(), actor, "project_1", IndexAssetRequest{
		Title: "品牌 TVC 二版", SourceKind: AssetSourceCreative, LineageID: first.LineageID,
		AssetType: AssetTypeBrandAd, AssetTypeSource: SourceHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.LineageID != first.LineageID || second.Revision != 2 || second.ID == first.ID {
		t.Fatalf("新版本应加入同一血缘：first=%#v second=%#v", first, second)
	}
	lineage, err := service.ListAssetLineage(context.Background(), actor, "project_1", second.ID)
	if err != nil || len(lineage) != 2 || lineage[0].Revision != 1 || lineage[1].Revision != 2 {
		t.Fatalf("lineage=%#v err=%v", lineage, err)
	}
	if _, err = service.IndexAsset(context.Background(), actor, "project_1", IndexAssetRequest{
		Title: "指向不存在的血缘", SourceKind: AssetSourceUpload, LineageID: "insightasset_missing",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error=%v", err)
	}
}

// AM-003：无法自动匹配的平台对象进入待匹配队列，由人决定指向哪个素材版本。
func TestUnmatchedPlatformObjectWaitsInQueueUntilAHumanResolvesIt(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	asset := indexedAsset(t, service, actor, AssetTypePrerollAd)
	mapping, err := service.RegisterAssetMapping(context.Background(), actor, "project_1", RegisterAssetMappingRequest{
		Platform: "demo_platform", PlatformObjectKind: "creative", PlatformObjectID: "cr-8821",
		PlatformObjectName: "夏季新品前贴-A",
	})
	if err != nil {
		t.Fatal(err)
	}
	if mapping.Status != MappingUnmatched || mapping.AssetID != "" {
		t.Fatalf("未匹配的映射不应带素材指针：%#v", mapping)
	}
	queue, err := service.ListAssetMappings(context.Background(), actor, "project_1", AssetMappingFilter{
		Statuses: []MappingStatus{MappingUnmatched},
	})
	if err != nil || len(queue) != 1 {
		t.Fatalf("queue=%#v err=%v", queue, err)
	}
	if _, err = service.ResolveAssetMapping(context.Background(), actor, "project_1", mapping.ID, ResolveAssetMappingRequest{
		ExpectedVersion: mapping.Version, Status: MappingMatched,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("匹配必须指定素材：error=%v", err)
	}
	resolved, err := service.ResolveAssetMapping(context.Background(), actor, "project_1", mapping.ID, ResolveAssetMappingRequest{
		ExpectedVersion: mapping.Version, Status: MappingMatched, AssetID: asset.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != MappingMatched || resolved.AssetID != asset.ID ||
		resolved.MatchSource != "human" || resolved.MatchedBy != "user_1" {
		t.Fatalf("resolved=%#v", resolved)
	}
	if _, err = service.ResolveAssetMapping(context.Background(), actor, "project_1", mapping.ID, ResolveAssetMappingRequest{
		ExpectedVersion: mapping.Version, Status: MappingIgnored, Note: "同一素材的重复上报。",
	}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("error=%v", err)
	}
}

// 混类型比较只保留共同特征，不同类型的字段不会被拼进同一列（MVP②）。
func TestFeatureMatrixOnlyComparesSharedFeaturesAcrossTypes(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	preroll := indexedAsset(t, service, actor, AssetTypePrerollAd)
	digital := indexedAsset(t, service, actor, AssetTypeDigitalHumanAd)
	for _, asset := range []Asset{preroll, digital} {
		if _, err := service.ExtractFeatures(context.Background(), actor, "project_1", asset.ID, ExtractFeaturesRequest{
			ExpectedVersion: asset.Version, SkillID: "skill_1",
			Features: []FeatureInput{{Key: "hook_type", Value: FeatureValue{Kind: FeatureKindEnumMul, Terms: []string{"反差"}}, Confidence: ConfidenceMedium}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// 数字人特有字段：只写在数字人素材上。
	current := mustGetAsset(t, service, actor, digital.ID)
	if _, err := service.ExtractFeatures(context.Background(), actor, "project_1", digital.ID, ExtractFeaturesRequest{
		ExpectedVersion: current.Version, SkillID: "skill_1",
		Features: []FeatureInput{{Key: "ai_disclosure", Value: FeatureValue{Kind: FeatureKindBool, Bool: true}, Confidence: ConfidenceHigh}},
	}); err != nil {
		t.Fatal(err)
	}
	matrix, err := service.GetFeatureMatrix(context.Background(), actor, "project_1", []string{preroll.ID, digital.ID})
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]int{}
	for _, row := range matrix.Rows {
		keys[row.Key] = len(row.Cells)
	}
	if _, ok := keys["ai_disclosure"]; ok {
		t.Fatalf("数字人专属字段不应出现在混类型矩阵中：%#v", matrix.Rows)
	}
	if keys["hook_type"] != 2 {
		t.Fatalf("共同字段应同时列出两个素材：%#v", matrix.Rows)
	}
	if len(matrix.AssetTypes) != 2 || !strings.Contains(matrix.Disclosure, "仅比较各类型都有的共同特征") {
		t.Fatalf("混类型对比必须写明口径：%#v", matrix)
	}
}

func TestFeatureMatrixPrefersHumanConclusionButKeepsTheLayerVisible(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	article := indexedAsset(t, service, actor, AssetTypeWechatArticle)
	if _, err := service.ExtractFeatures(context.Background(), actor, "project_1", article.ID, ExtractFeaturesRequest{
		ExpectedVersion: article.Version, SkillID: "skill_1",
		Features: []FeatureInput{{Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"知识"}}, Confidence: ConfidenceLow}},
	}); err != nil {
		t.Fatal(err)
	}
	current := mustGetAsset(t, service, actor, article.ID)
	if _, err := service.PatchFeatures(context.Background(), actor, "project_1", article.ID, PatchFeaturesRequest{
		ExpectedVersion: current.Version,
		Features:        []FeatureInput{{Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"案例"}}}},
	}); err != nil {
		t.Fatal(err)
	}
	matrix, err := service.GetFeatureMatrix(context.Background(), actor, "project_1", []string{article.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range matrix.Rows {
		if row.Key != "article_type" {
			continue
		}
		if len(row.Cells) != 1 || row.Cells[0].Source != SourceHuman || row.Cells[0].Value.Terms[0] != "案例" {
			t.Fatalf("人工结论应覆盖同名 AI 推断并标明来源：%#v", row)
		}
		return
	}
	t.Fatalf("矩阵缺少 article_type 行：%#v", matrix.Rows)
}

func TestRetiredAssetStaysReadable(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	asset := indexedAsset(t, service, actor, AssetTypeBrandAd)
	if _, err := service.RetireAsset(context.Background(), actor, "project_1", asset.ID, AssetTransitionRequest{
		ExpectedVersion: asset.Version,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("失效需要原因：error=%v", err)
	}
	retired, err := service.RetireAsset(context.Background(), actor, "project_1", asset.ID, AssetTransitionRequest{
		ExpectedVersion: asset.Version, Reason: "源文件已下线。",
	})
	if err != nil || retired.AnalysisStatus != AnalysisRetired {
		t.Fatalf("retired=%#v err=%v", retired, err)
	}
	stored, err := service.ListAssets(context.Background(), actor, "project_1", AssetFilter{
		Statuses: []AnalysisStatus{AnalysisRetired},
	})
	if err != nil || len(stored) != 1 {
		t.Fatalf("逻辑失效必须保留可读行：stored=%#v err=%v", stored, err)
	}
}

func TestAssetListFiltersActuallyNarrowTheDataset(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	indexedAsset(t, service, actor, AssetTypeWechatArticle)
	indexedAsset(t, service, actor, AssetTypeBrandAd)
	if _, err := service.IndexAsset(context.Background(), actor, "project_1", IndexAssetRequest{
		Title: "外部引用的竞品图文", SourceKind: AssetSourceExternal,
	}); err != nil {
		t.Fatal(err)
	}
	all, err := service.ListAssets(context.Background(), actor, "project_1", AssetFilter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("all=%#v err=%v", all, err)
	}
	awaiting, err := service.ListAssets(context.Background(), actor, "project_1", AssetFilter{
		Statuses: []AnalysisStatus{AnalysisAwaitingData},
	})
	if err != nil || len(awaiting) != 1 || awaiting[0].SourceKind != AssetSourceExternal {
		t.Fatalf("awaiting=%#v err=%v", awaiting, err)
	}
	byType, err := service.ListAssets(context.Background(), actor, "project_1", AssetFilter{
		AssetTypes: []AssetType{AssetTypeBrandAd},
	})
	if err != nil || len(byType) != 1 || byType[0].AssetType != AssetTypeBrandAd {
		t.Fatalf("byType=%#v err=%v", byType, err)
	}
	if _, err = service.ListAssets(context.Background(), actor, "project_1", AssetFilter{
		Statuses: []AnalysisStatus{"不存在的状态"},
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error=%v", err)
	}
}

func TestChangingTypeAfterExtractionIsRefused(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	actor := testActor()
	article := indexedAsset(t, service, actor, AssetTypeWechatArticle)
	if _, err := service.ExtractFeatures(context.Background(), actor, "project_1", article.ID, ExtractFeaturesRequest{
		ExpectedVersion: article.Version, SkillID: "skill_1",
		Features: []FeatureInput{{Key: "article_type", Value: FeatureValue{Kind: FeatureKindEnum, Terms: []string{"知识"}}, Confidence: ConfidenceLow}},
	}); err != nil {
		t.Fatal(err)
	}
	current := mustGetAsset(t, service, actor, article.ID)
	_, err := service.IdentifyAssetType(context.Background(), actor, "project_1", article.ID, IdentifyAssetTypeRequest{
		ExpectedVersion: current.Version, AssetType: AssetTypeXiaohongshuNote, Source: SourceHuman,
	})
	if !errors.Is(err, ErrInvalidState) || !strings.Contains(err.Error(), "改判类型需先清空特征") {
		t.Fatalf("error=%v", err)
	}
}

func TestCoverageCountsEffectiveValuesOnce(t *testing.T) {
	t.Parallel()
	asset := Asset{ID: "insightasset_1", AssetType: AssetTypeWechatArticle}
	coverage := CoverageOf(asset, []AssetFeature{
		{AssetID: asset.ID, Key: "article_type", Source: SourceAI},
		{AssetID: asset.ID, Key: "article_type", Source: SourceHuman},
		{AssetID: asset.ID, Key: "section_count", Source: SourceAI},
		{AssetID: "insightasset_other", Key: "word_count", Source: SourceAI},
	})
	schema, _ := FeatureSchemaFor(AssetTypeWechatArticle)
	if coverage.Filled != 2 || coverage.Total != len(schema.Fields) {
		t.Fatalf("同一特征的两层只算一项：%#v", coverage)
	}
}

func indexedAsset(t *testing.T, service Service, actor contract.ActorContext, assetType AssetType) Asset {
	t.Helper()
	request := IndexAssetRequest{Title: "测试素材", SourceKind: AssetSourceCreative}
	if assetType != AssetTypeUnknown {
		request.AssetType = assetType
		request.AssetTypeSource = SourceHuman
	}
	asset, err := service.IndexAsset(context.Background(), actor, "project_1", request)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func mustGetAsset(t *testing.T, service Service, actor contract.ActorContext, assetID string) Asset {
	t.Helper()
	asset, err := service.GetAsset(context.Background(), actor, "project_1", assetID)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func testAssetService() Service {
	sequence := 0
	return Service{
		Assets: &memoryAssetRepository{
			assets:   map[string]Asset{},
			mappings: map[string]AssetMapping{},
			features: map[string]AssetFeature{},
		},
		Projects: testProjects{},
		Now:      func() time.Time { return time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC) },
		NewID: func(prefix string) (string, error) {
			sequence++
			return fmt.Sprintf("%s_%d", prefix, sequence), nil
		},
	}
}

// memoryAssetRepository mirrors the guarantees the MySQL implementation gets
// from its constraints: optimistic versions, allowed source states, and one row
// per (asset, feature, layer).
type memoryAssetRepository struct {
	assets   map[string]Asset
	mappings map[string]AssetMapping
	features map[string]AssetFeature
}

func featureLayerKey(assetID, key string, source FeatureSource) string {
	return assetID + "|" + key + "|" + string(source)
}

func (r *memoryAssetRepository) CreateAsset(_ context.Context, value Asset) (Asset, error) {
	r.assets[value.ID] = value
	return value, nil
}

func (r *memoryAssetRepository) ListAssets(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, filter AssetFilter) ([]Asset, error) {
	values := make([]Asset, 0)
	for _, asset := range r.assets {
		if asset.OrganizationID != organizationID || asset.ProjectID != projectID {
			continue
		}
		if len(filter.Statuses) > 0 && !allowsAnalysisStatus(filter.Statuses, asset.AnalysisStatus) {
			continue
		}
		if len(filter.AssetTypes) > 0 && !containsAssetType(filter.AssetTypes, asset.AssetType) {
			continue
		}
		if len(filter.SourceKinds) > 0 && !containsSourceKind(filter.SourceKinds, asset.SourceKind) {
			continue
		}
		if len(filter.Roles) > 0 && !containsAssetRole(filter.Roles, asset.Role) {
			continue
		}
		if filter.LineageID != "" && asset.LineageID != filter.LineageID {
			continue
		}
		values = append(values, asset)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	if filter.Limit > 0 && len(values) > filter.Limit {
		values = values[:filter.Limit]
	}
	return values, nil
}

func (r *memoryAssetRepository) GetAsset(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (Asset, error) {
	asset, ok := r.assets[id]
	if !ok || asset.OrganizationID != organizationID || asset.ProjectID != projectID {
		return Asset{}, ErrNotFound
	}
	return asset, nil
}

func (r *memoryAssetRepository) ListAssetLineage(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, lineageID string) ([]Asset, error) {
	values := make([]Asset, 0)
	for _, asset := range r.assets {
		if asset.OrganizationID == organizationID && asset.ProjectID == projectID && asset.LineageID == lineageID {
			values = append(values, asset)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Revision < values[j].Revision })
	return values, nil
}

func (r *memoryAssetRepository) UpdateAssetType(ctx context.Context, input UpdateAssetTypeInput) (Asset, error) {
	asset, err := r.GetAsset(ctx, input.OrganizationID, input.ProjectID, input.ID)
	if err != nil {
		return Asset{}, err
	}
	if asset.Version != input.ExpectedVersion {
		return Asset{}, ErrVersionConflict
	}
	if !allowsAnalysisStatus(input.From, asset.AnalysisStatus) {
		return Asset{}, ErrInvalidState
	}
	asset.AssetType = input.AssetType
	asset.AssetTypeSource = input.Source
	asset.AssetTypeConfidence = input.Confidence
	asset.AnalysisStatus = input.To
	asset.AnalysisStatusReason = input.Reason
	asset.AnalysisStatusChangedAt = &input.Now
	asset.Version++
	asset.UpdatedAt = input.Now
	r.assets[asset.ID] = asset
	return asset, nil
}

func (r *memoryAssetRepository) TransitionAsset(ctx context.Context, input TransitionAssetInput) (Asset, error) {
	asset, err := r.GetAsset(ctx, input.OrganizationID, input.ProjectID, input.ID)
	if err != nil {
		return Asset{}, err
	}
	if asset.Version != input.ExpectedVersion {
		return Asset{}, ErrVersionConflict
	}
	if !allowsAnalysisStatus(input.From, asset.AnalysisStatus) {
		return Asset{}, fmt.Errorf("%w: 素材当前是%s", ErrInvalidState, asset.AnalysisStatus.Label())
	}
	asset.AnalysisStatus = input.To
	asset.AnalysisStatusReason = input.Reason
	asset.AnalysisStatusChangedAt = &input.Now
	asset.Version++
	asset.UpdatedAt = input.Now
	r.assets[asset.ID] = asset
	return asset, nil
}

func (r *memoryAssetRepository) UpdateAssetRole(ctx context.Context, input UpdateAssetRoleInput) (Asset, error) {
	asset, err := r.GetAsset(ctx, input.OrganizationID, input.ProjectID, input.ID)
	if err != nil {
		return Asset{}, err
	}
	if asset.Version != input.ExpectedVersion {
		return Asset{}, ErrVersionConflict
	}
	if asset.Role == input.To {
		return asset, nil
	}
	asset.Role = input.To
	asset.Version++
	asset.UpdatedAt = input.Now
	r.assets[asset.ID] = asset
	return asset, nil
}

func (r *memoryAssetRepository) CreateAssetMapping(_ context.Context, value AssetMapping) (AssetMapping, error) {
	r.mappings[value.ID] = value
	return value, nil
}

func (r *memoryAssetRepository) ListAssetMappings(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, filter AssetMappingFilter) ([]AssetMapping, error) {
	values := make([]AssetMapping, 0)
	for _, mapping := range r.mappings {
		if mapping.OrganizationID != organizationID || mapping.ProjectID != projectID {
			continue
		}
		if len(filter.Statuses) > 0 && !containsMappingStatus(filter.Statuses, mapping.Status) {
			continue
		}
		if filter.Platform != "" && mapping.Platform != filter.Platform {
			continue
		}
		if filter.AssetID != "" && mapping.AssetID != filter.AssetID {
			continue
		}
		values = append(values, mapping)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return values, nil
}

func (r *memoryAssetRepository) GetAssetMapping(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (AssetMapping, error) {
	mapping, ok := r.mappings[id]
	if !ok || mapping.OrganizationID != organizationID || mapping.ProjectID != projectID {
		return AssetMapping{}, ErrNotFound
	}
	return mapping, nil
}

func (r *memoryAssetRepository) ResolveAssetMapping(_ context.Context, value AssetMapping, expectedVersion int64) (AssetMapping, error) {
	stored, ok := r.mappings[value.ID]
	if !ok {
		return AssetMapping{}, ErrNotFound
	}
	if stored.Version != expectedVersion {
		return AssetMapping{}, ErrVersionConflict
	}
	value.Version = stored.Version + 1
	r.mappings[value.ID] = value
	return value, nil
}

func (r *memoryAssetRepository) UpsertAssetFeatures(ctx context.Context, input UpsertAssetFeaturesInput) ([]AssetFeature, error) {
	if _, err := r.TransitionAsset(ctx, TransitionAssetInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, ID: input.AssetID,
		ExpectedVersion: input.ExpectedVersion, From: input.From, To: input.To,
		Reason: input.Reason, Now: input.Now,
	}); err != nil {
		return nil, err
	}
	for _, feature := range input.Features {
		layer := featureLayerKey(feature.AssetID, feature.Key, feature.Source)
		if existing, ok := r.features[layer]; ok {
			feature.ID = existing.ID
			feature.Version = existing.Version + 1
			feature.CreatedAt = existing.CreatedAt
		}
		r.features[layer] = feature
	}
	return r.ListAssetFeatures(ctx, input.OrganizationID, input.ProjectID, []string{input.AssetID}, 0)
}

func (r *memoryAssetRepository) ListAssetFeatures(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, assetIDs []string, limit int) ([]AssetFeature, error) {
	wanted := make(map[string]struct{}, len(assetIDs))
	for _, assetID := range assetIDs {
		wanted[assetID] = struct{}{}
	}
	values := make([]AssetFeature, 0)
	for _, feature := range r.features {
		if feature.OrganizationID != organizationID || feature.ProjectID != projectID {
			continue
		}
		if len(wanted) > 0 {
			if _, ok := wanted[feature.AssetID]; !ok {
				continue
			}
		}
		values = append(values, feature)
	}
	sort.Slice(values, func(i, j int) bool {
		left := featureLayerKey(values[i].AssetID, values[i].Key, values[i].Source)
		right := featureLayerKey(values[j].AssetID, values[j].Key, values[j].Source)
		return left < right
	})
	if limit > 0 && len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}

func (r *memoryAssetRepository) CountAssetFeaturesByReviewState(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, assetID string, state ReviewState) (int, error) {
	count := 0
	for _, feature := range r.features {
		if feature.OrganizationID != organizationID || feature.ProjectID != projectID || feature.AssetID != assetID {
			continue
		}
		if state != "" {
			if feature.ReviewState != state || feature.Source != SourceAI {
				continue
			}
			if _, reviewed := r.features[featureLayerKey(assetID, feature.Key, SourceHuman)]; reviewed {
				continue
			}
		}
		count++
	}
	return count, nil
}

func containsAssetType(values []AssetType, value AssetType) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func containsAssetRole(values []AssetRole, value AssetRole) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func containsSourceKind(values []AssetSourceKind, value AssetSourceKind) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func containsMappingStatus(values []MappingStatus, value MappingStatus) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func TestAssetRoleValidAndLabel(t *testing.T) {
	if !AssetRoleLedger.valid() || !AssetRoleAnalysis.valid() {
		t.Fatal("台账与分析对象都应是合法身份")
	}
	if AssetRole("archive").valid() {
		t.Fatal("身份只有两种，第三种必须被拒")
	}
	if AssetRoleLedger.Label() != "台账" || AssetRoleAnalysis.Label() != "分析对象" {
		t.Fatalf("身份的中文名不对：%q / %q", AssetRoleLedger.Label(), AssetRoleAnalysis.Label())
	}
}

func TestIndexAssetDefaultsToAnalysisRole(t *testing.T) {
	t.Parallel()
	service, actor := testAssetService(), testActor()
	asset, err := service.IndexAsset(context.Background(), actor, "project_1", IndexAssetRequest{
		Title: "投放成片 A", SourceKind: AssetSourceUpload,
	})
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	if asset.Role != AssetRoleAnalysis {
		t.Fatalf("不填 role 时应默认是分析对象，得到 %q", asset.Role)
	}
}

func TestIndexAssetRejectsUnknownRole(t *testing.T) {
	t.Parallel()
	service, actor := testAssetService(), testActor()
	_, err := service.IndexAsset(context.Background(), actor, "project_1", IndexAssetRequest{
		Title: "投放成片 B", SourceKind: AssetSourceUpload, Role: AssetRole("archive"),
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("未知身份应被拒，得到 %v", err)
	}
}

func TestListAssetsHidesLedgerByDefault(t *testing.T) {
	t.Parallel()
	service, actor := testAssetService(), testActor()
	ctx := context.Background()
	if _, err := service.IndexAsset(ctx, actor, "project_1", IndexAssetRequest{
		Title: "分析对象", SourceKind: AssetSourceUpload,
	}); err != nil {
		t.Fatalf("登记分析对象失败：%v", err)
	}
	if _, err := service.IndexAsset(ctx, actor, "project_1", IndexAssetRequest{
		Title: "台账素材", SourceKind: AssetSourceUpload, Role: AssetRoleLedger,
	}); err != nil {
		t.Fatalf("登记台账素材失败：%v", err)
	}

	// 不给 roles 就只看分析对象：四个队列和红点靠这条默认值，绝不能把几千条台账数进去。
	values, err := service.ListAssets(ctx, actor, "project_1", AssetFilter{})
	if err != nil {
		t.Fatalf("列素材失败：%v", err)
	}
	for _, value := range values {
		if value.Role != AssetRoleAnalysis {
			t.Fatalf("默认列表混进了 %q：%s", value.Role, value.Title)
		}
	}

	ledger, err := service.ListAssets(ctx, actor, "project_1", AssetFilter{Roles: []AssetRole{AssetRoleLedger}})
	if err != nil {
		t.Fatalf("列台账失败：%v", err)
	}
	if len(ledger) != 1 || ledger[0].Title != "台账素材" {
		t.Fatalf("显式要台账时应只拿到台账，得到 %d 条", len(ledger))
	}
}

func TestPromoteAssetToAnalysisKeepsProgress(t *testing.T) {
	t.Parallel()
	service, actor := testAssetService(), testActor()
	ctx := context.Background()
	asset, err := service.IndexAsset(ctx, actor, "project_1", IndexAssetRequest{
		Title: "台账里的成片", SourceKind: AssetSourceUpload, Role: AssetRoleLedger,
	})
	if err != nil {
		t.Fatalf("登记台账素材失败：%v", err)
	}
	before := asset.AnalysisStatus

	promoted, err := service.PromoteAssetToAnalysis(ctx, actor, "project_1", asset.ID,
		AssetTransitionRequest{ExpectedVersion: asset.Version, Reason: "这条要投了"})
	if err != nil {
		t.Fatalf("拉进分析失败：%v", err)
	}
	if promoted.Role != AssetRoleAnalysis {
		t.Fatalf("拉进分析后身份应是分析对象，得到 %q", promoted.Role)
	}
	// 身份换了，进度不清零——这是 role 独立于 analysis_status 的全部意义。
	if promoted.AnalysisStatus != before {
		t.Fatalf("拉进分析不该动分析进度：%q -> %q", before, promoted.AnalysisStatus)
	}
}

func TestReturnAssetToLedgerRefusesMatchedAsset(t *testing.T) {
	t.Parallel()
	service, actor := testAssetService(), testActor()
	ctx := context.Background()
	asset, err := service.IndexAsset(ctx, actor, "project_1", IndexAssetRequest{
		Title: "已经对上号的成片", SourceKind: AssetSourceUpload,
	})
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	mapping, err := service.RegisterAssetMapping(ctx, actor, "project_1", RegisterAssetMappingRequest{
		Platform: "douyin", PlatformObjectKind: "creative", PlatformObjectID: "cr_1", PlatformObjectName: "计划一",
	})
	if err != nil {
		t.Fatalf("登记映射失败：%v", err)
	}
	if _, err := service.ResolveAssetMapping(ctx, actor, "project_1", mapping.ID, ResolveAssetMappingRequest{
		ExpectedVersion: mapping.Version, Status: MappingMatched, AssetID: asset.ID, Note: "人工对上",
	}); err != nil {
		t.Fatalf("对号失败：%v", err)
	}

	// 对上号意味着它有广告对象、有花费。这时候退回台账等于把已经产生的数据藏起来。
	_, err = service.ReturnAssetToLedger(ctx, actor, "project_1", asset.ID,
		AssetTransitionRequest{ExpectedVersion: asset.Version, Reason: "看错了"})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("对上号的素材不该能退回台账，得到 %v", err)
	}
}

func TestReturnAssetToLedgerAllowsUnmatchedAsset(t *testing.T) {
	t.Parallel()
	service, actor := testAssetService(), testActor()
	ctx := context.Background()
	asset, err := service.IndexAsset(ctx, actor, "project_1", IndexAssetRequest{
		Title: "拉错了的素材", SourceKind: AssetSourceUpload,
	})
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	returned, err := service.ReturnAssetToLedger(ctx, actor, "project_1", asset.ID,
		AssetTransitionRequest{ExpectedVersion: asset.Version, Reason: "这条其实没投"})
	if err != nil {
		t.Fatalf("退回台账失败：%v", err)
	}
	if returned.Role != AssetRoleLedger {
		t.Fatalf("退回后身份应是台账，得到 %q", returned.Role)
	}
}
