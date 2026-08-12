package insights

import (
	"errors"
	"testing"
)

// Every identifiable type (AM-004) must have a feature system (AM-005); an
// asset whose type is known but whose schema is missing cannot be extracted.
func TestEveryAssetTypeHasSchema(t *testing.T) {
	types := AllAssetTypes()
	if len(types) != 6 {
		t.Fatalf("expected the 6 types named in AM-004, got %d", len(types))
	}
	for _, assetType := range types {
		schema, ok := FeatureSchemaFor(assetType)
		if !ok {
			t.Fatalf("%s has no feature schema", assetType)
		}
		if len(schema.Fields) == 0 {
			t.Fatalf("%s has an empty feature schema", assetType)
		}
		if schema.Source == "" {
			t.Fatalf("%s schema has no PRD source marker", assetType)
		}
		seen := make(map[string]struct{}, len(schema.Fields))
		for _, field := range schema.Fields {
			if field.Group == "" || field.Label == "" {
				t.Fatalf("%s field %q is missing group or label", assetType, field.Key)
			}
			if _, dup := seen[field.Key]; dup {
				t.Fatalf("%s declares field %q twice", assetType, field.Key)
			}
			seen[field.Key] = struct{}{}
		}
	}
}

// 03 §15② 「不把视频钩子字段套到公众号文章」 is the acceptance criterion this
// guards. The previous single flat feature shape could not express it at all.
func TestVideoHookFieldRejectedOnArticle(t *testing.T) {
	if err := ValidateFeatureValue(AssetTypeWechatArticle, "hook_type", []string{"反差"}); err == nil {
		t.Fatal("expected 公众号图文 to reject the performance-ad hook field")
	} else if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}

	// The same key is legitimate on a performance ad.
	if err := ValidateFeatureValue(AssetTypePrerollAd, "hook_type", []string{"反差"}); err != nil {
		t.Fatalf("广告前贴 should accept hook_type: %v", err)
	}
}

func TestValidateFeatureValue(t *testing.T) {
	t.Run("类型未识别时拒绝写特征", func(t *testing.T) {
		if err := ValidateFeatureValue(AssetTypeUnknown, "cta", nil); err == nil {
			t.Fatal("expected extraction to be blocked before type identification")
		}
	})

	t.Run("受控词表外的取值被拒绝", func(t *testing.T) {
		err := ValidateFeatureValue(AssetTypeWechatArticle, "article_type", []string{"随笔"})
		if err == nil {
			t.Fatal("expected off-vocabulary term to be rejected")
		}
	})

	t.Run("受控词表内的取值通过", func(t *testing.T) {
		if err := ValidateFeatureValue(AssetTypeWechatArticle, "article_type", []string{"案例"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("词表未配置时放行", func(t *testing.T) {
		// opening_style ships without a vocabulary: the administrator maintains it
		// (03 §5 末). Governance must not block extraction.
		if err := ValidateFeatureValue(AssetTypeXiaohongshuNote, "opening_style", []string{"任意开场"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("单选字段拒绝多个取值", func(t *testing.T) {
		err := ValidateFeatureValue(AssetTypeWechatArticle, "article_type", []string{"案例", "知识"})
		if err == nil {
			t.Fatal("expected single-choice field to reject two terms")
		}
	})

	t.Run("多选字段接受多个取值", func(t *testing.T) {
		err := ValidateFeatureValue(AssetTypeXiaohongshuNote, "title_angles", []string{"数字", "反差"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// The three performance-ad types share the 效果广告 base (§5.4) and each add
// their own pack; the packs must not leak across types.
func TestPerformanceAdVariantsShareBaseButNotPacks(t *testing.T) {
	shared := SharedFeatureKeys(AssetTypeDigitalHumanAd, AssetTypePrerollAd, AssetTypeHitReplicaAd)
	for _, key := range []string{"hook_type", "selling_points", "presenter", "experiment_variables"} {
		if !contains(shared, key) {
			t.Fatalf("expected %q in the shared 效果广告 base, shared=%v", key, shared)
		}
	}
	for _, key := range []string{"ai_disclosure", "mute_comprehensible", "copyright_risk"} {
		if contains(shared, key) {
			t.Fatalf("variant-only field %q must not be shared across performance-ad types", key)
		}
	}

	if err := ValidateFeatureValue(AssetTypePrerollAd, "ai_disclosure", nil); err == nil {
		t.Fatal("广告前贴 must not accept the 数字人 AI disclosure field")
	}
	if err := ValidateFeatureValue(AssetTypeDigitalHumanAd, "ai_disclosure", nil); err != nil {
		t.Fatalf("数字人 should accept ai_disclosure: %v", err)
	}
}

// A cross-type 特征矩阵 may only pivot on shared keys. 小红书图文 and 公众号图文
// are both 图文 but their feature systems are separate by design, so mixing them
// must not silently produce columns.
func TestSharedFeatureKeysAcrossUnrelatedTypes(t *testing.T) {
	if shared := SharedFeatureKeys(AssetTypeXiaohongshuNote, AssetTypeWechatArticle); len(shared) != 0 {
		t.Fatalf("expected no shared pivot between the two 图文 systems, got %v", shared)
	}
	// 品牌广告 和 广告前贴 只在**量得出来的**那两项上通用：时长和画幅。
	//
	// 这两项原来只有品牌广告有（§5.3 渠道适配），效果广告那边一项都没有，于是
	// 两类视频素材之间一个可比较的维度都没有。补齐之后它们成了唯一的跨类型透视
	// 列——而这恰恰是应该的：口播、钩子这些是各自体系里的词，含义不通用；
	// 「15 秒」和「竖版」在哪类素材上都是同一件事。
	shared := SharedFeatureKeys(AssetTypeBrandAd, AssetTypePrerollAd)
	if len(shared) != 2 || !contains(shared, "duration") || !contains(shared, "aspect_ratio") {
		t.Fatalf("品牌广告与广告前贴只应在时长和画幅上通用，实际 %v", shared)
	}
}

func TestFeatureCoverage(t *testing.T) {
	schema, _ := FeatureSchemaFor(AssetTypeHitReplicaAd)
	filled, total := FeatureCoverage(AssetTypeHitReplicaAd, []string{"hook_type", "cta", "copyright_risk", "not_a_field"})
	if total != len(schema.Fields) {
		t.Fatalf("total should be the schema size, got %d want %d", total, len(schema.Fields))
	}
	if filled != 3 {
		t.Fatalf("unknown keys must not count towards coverage, got %d", filled)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
