package insights

import (
	"context"
	"errors"
	"testing"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// stubMediaReader 冒充素材库。facts 就是那个文件探测出来的东西，err 用来
// 模拟素材库整个读不到。
type stubMediaReader struct {
	facts MediaFacts
	err   error
	// calls 记下被问了几次，用来验证「没有文件引用时压根不该去问素材库」。
	calls int
}

func (r *stubMediaReader) ReadMediaFacts(context.Context, contract.ActorContext, contract.ProjectID, string, int64) (MediaFacts, error) {
	r.calls++
	return r.facts, r.err
}

// derivedAsset 登记一条带素材库引用、类型已识别的素材，也就是「从创意导入」
// 之后应有的样子。
func derivedAsset(t *testing.T, service Service, actor contract.ActorContext, assetType AssetType) Asset {
	t.Helper()
	asset, err := service.IndexAsset(context.Background(), actor, "project_1", IndexAssetRequest{
		Title: "夏季新品前贴", SourceKind: AssetSourceCreative,
		PlatformAssetID: "asset_1", PlatformAssetVersion: 3,
		AssetType: assetType, AssetTypeSource: SourceHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	return asset
}

func TestDerivedFeaturesComeFromTheFileAndDoNotMoveTheAsset(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	media := &stubMediaReader{facts: MediaFacts{
		Measured: true, DurationSeconds: 15.0166667, WidthPixels: 1080, HeightPixels: 1920,
	}}
	service.Media = media
	actor := testActor()
	asset := derivedAsset(t, service, actor, AssetTypePrerollAd)

	features, err := service.DeriveFeatures(context.Background(), actor, "project_1", asset.ID,
		DeriveFeaturesRequest{ExpectedVersion: asset.Version})
	if err != nil {
		t.Fatal(err)
	}
	if len(features) != 2 {
		t.Fatalf("时长和画幅两项都该落库：%#v", features)
	}
	byKey := map[string]AssetFeature{}
	for _, feature := range features {
		if feature.Source != SourceDerived {
			t.Fatalf("客观量必须落在 derived 层：%#v", feature)
		}
		// 客观量不带机器置信度——库里的 CHECK 也这么要求
		// （20260812100000_insight_feature_derived.up.sql）。
		if feature.Confidence != "" {
			t.Fatalf("量出来的数不该带置信度：%#v", feature)
		}
		// 不进复核队列：没有推断可复核，是探测填的。
		if feature.ReviewState != ReviewAuthored {
			t.Fatalf("客观量的复核状态应为 authored：%#v", feature)
		}
		if !feature.Source.AdmissibleForAttribution() {
			t.Fatalf("客观量必须能进归因，否则这一层白做：%#v", feature)
		}
		byKey[feature.Key] = feature
	}
	// 15.0166667 秒要收成 15，否则页面上会出现一串小数，而两条实际一样长的
	// 素材永远算不成相似。
	if byKey["duration"].Value.Number != 15 {
		t.Fatalf("时长应取整到一位小数：%#v", byKey["duration"].Value)
	}
	// 画幅落成可比较的挡位，不落像素串——相似度是按取值精确重叠算的。
	if len(byKey["aspect_ratio"].Value.Terms) != 1 || byKey["aspect_ratio"].Value.Terms[0] != aspectPortrait {
		t.Fatalf("1080x1920 应判成竖版：%#v", byKey["aspect_ratio"].Value)
	}

	// 量一次时长不是一个待人复核的结论，素材状态不该被推到「待确认」。
	after := mustGetAsset(t, service, actor, asset.ID)
	if after.AnalysisStatus != asset.AnalysisStatus {
		t.Fatalf("写客观量不该移动素材状态：%s → %s", asset.AnalysisStatus, after.AnalysisStatus)
	}
	// 但版本仍然要走一格：特征写进去了而素材版本没动，下一个并发写入
	// 会拿着过期版本继续写。
	if after.Version != asset.Version+1 {
		t.Fatalf("写入仍须占用一次乐观锁：%d → %d", asset.Version, after.Version)
	}
}

func TestDerivedLayerRefusesWhenTheProbeDidNotSucceed(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	service.Media = &stubMediaReader{facts: MediaFacts{Unavailable: "素材库探测这个文件失败了"}}
	actor := testActor()
	asset := derivedAsset(t, service, actor, AssetTypePrerollAd)

	_, err := service.DeriveFeatures(context.Background(), actor, "project_1", asset.ID,
		DeriveFeaturesRequest{ExpectedVersion: asset.Version})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("探测没成功就不该写：error=%v", err)
	}
	// 探测失败时那几个数是零值。落一条「时长 0 秒」进客观可测层，
	// 就是把一次故障伪装成一条测量结论——归因还会认它。
	features, listErr := service.ListAssetFeatures(context.Background(), actor, "project_1", asset.ID)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(features) != 0 {
		t.Fatalf("失败的探测不该留下任何一行：%#v", features)
	}
}

func TestDerivedLayerNeedsAFileReferenceBeforeItAsksTheAssetLibrary(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	media := &stubMediaReader{facts: MediaFacts{Measured: true, DurationSeconds: 15, WidthPixels: 1080, HeightPixels: 1920}}
	service.Media = media
	actor := testActor()
	// 手工登记的素材没有 platform_asset_id：洞察这边只有一条索引，没有文件。
	asset := indexedAsset(t, service, actor, AssetTypePrerollAd)

	_, err := service.DeriveFeatures(context.Background(), actor, "project_1", asset.ID,
		DeriveFeaturesRequest{ExpectedVersion: asset.Version})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("没有文件引用应当被拒：error=%v", err)
	}
	if media.calls != 0 {
		t.Fatalf("没有引用就不该去问素材库，实际问了 %d 次", media.calls)
	}
}

func TestImageTextAssetsHaveNothingToMeasure(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	service.Media = &stubMediaReader{facts: MediaFacts{Measured: true, DurationSeconds: 15, WidthPixels: 1080, HeightPixels: 1920}}
	actor := testActor()
	// 小红书图文的特征体系里没有时长，也没有画幅。
	asset := derivedAsset(t, service, actor, AssetTypeXiaohongshuNote)

	_, err := service.DeriveFeatures(context.Background(), actor, "project_1", asset.ID,
		DeriveFeaturesRequest{ExpectedVersion: asset.Version})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("图文类没有可测变量，应当明说而不是写一条空的：error=%v", err)
	}
}

func TestDerivedLayerFailsLoudlyWithoutTheAssetLibrary(t *testing.T) {
	t.Parallel()
	service := testAssetService() // Media 为 nil
	actor := testActor()
	asset := derivedAsset(t, service, actor, AssetTypePrerollAd)

	// 没接素材库时直接失败，不能退化成「按类型猜一个默认时长」——
	// 猜出来的数标着「客观可测」，比没有这一层坏得多。
	if _, err := service.DeriveFeatures(context.Background(), actor, "project_1", asset.ID,
		DeriveFeaturesRequest{ExpectedVersion: asset.Version}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("没接素材库应当直接失败：error=%v", err)
	}
}

func TestAspectBucketRoundsNearSquareToSquare(t *testing.T) {
	t.Parallel()
	cases := []struct {
		width, height int
		want          string
	}{
		{1080, 1920, aspectPortrait},
		{1920, 1080, aspectLandscape},
		{1080, 1080, aspectSquare},
		// 编码器凑出来的尺寸：人眼看就是方的，不该因为差几个像素判成竖版。
		{1080, 1084, aspectSquare},
		{0, 1920, ""},
		{1080, 0, ""},
	}
	for _, item := range cases {
		if got := aspectBucket(item.width, item.height); got != item.want {
			t.Fatalf("%dx%d 应判成 %q，实际 %q", item.width, item.height, item.want, got)
		}
	}
}

// 效果广告的三类都得有这两项，否则「量出来的时长」在数字人和爆款复刻上无处可放。
func TestPerformanceAdTypesCarryTheMeasurableChannelFields(t *testing.T) {
	t.Parallel()
	for _, assetType := range []AssetType{
		AssetTypeBrandAd, AssetTypeDigitalHumanAd, AssetTypePrerollAd, AssetTypeHitReplicaAd,
	} {
		schema, ok := FeatureSchemaFor(assetType)
		if !ok {
			t.Fatalf("%s 没有特征体系", assetType)
		}
		duration, hasDuration := schema.Field("duration")
		if !hasDuration || duration.Kind != FeatureKindDuration || duration.Unit != "秒" {
			t.Fatalf("%s 缺可测的时长字段：%#v", assetType, duration)
		}
		aspect, hasAspect := schema.Field("aspect_ratio")
		if !hasAspect || aspect.Kind != FeatureKindEnum {
			t.Fatalf("%s 缺可测的画幅字段：%#v", assetType, aspect)
		}
		// 键重复的话 Field() 只返回第一条，而 Groups() 会把同一组列两遍。
		seen := map[string]int{}
		for _, field := range schema.Fields {
			seen[field.Key]++
			if seen[field.Key] > 1 {
				t.Fatalf("%s 的特征键 %q 重复了", assetType, field.Key)
			}
		}
	}
}
