package insights

import "testing"

// 「历史模式」那一屏的标题是「哪些内容特征反复有效」，导语是「比只被提过一次
// 可信得多」。这一组用例守的就是这两句话：能被称作「反复」的，必须真的出现在
// 至少两条结论里，而且名字得先落在特征体系里——否则同一个意思的两种写法会各占
// 一个「1 条」的桶，永远攒不到反复（03 §5 末：受控词表由管理员维护，避免同义词
// 碎片化）。
func TestFeaturePatternsFoldSynonymsAndGateRepetition(t *testing.T) {
	values := []Experience{
		featureExperience("键名写法", []string{"hook_type"}, "抖音"),
		featureExperience("中文名写法", []string{"钩子类型"}, "视频号"),
		featureExperience("大小写和空格", []string{"  Hook_Type "}, "抖音"),
		featureExperience("只提过一次的入表特征", []string{"subtitle_density"}, "抖音"),
		featureExperience("没入表的自由文本一", []string{"首图卖点数"}, "抖音"),
		featureExperience("没入表的自由文本二", []string{"首图卖点数"}, "抖音"),
		featureExperience("有歧义的中文名", []string{"主题"}, "抖音"),
		featureExperience("空白不成桶", []string{"   "}, "抖音"),
	}

	patterns := buildFeaturePatterns(values)
	byLabel := map[string]FeaturePattern{}
	for _, pattern := range patterns {
		if _, dup := byLabel[pattern.Label]; dup {
			t.Fatalf("同一个名字出现了两个桶：%q", pattern.Label)
		}
		byLabel[pattern.Label] = pattern
	}

	hook, ok := byLabel["钩子类型"]
	if !ok {
		t.Fatalf("键名和中文名要归到同一格，实际桶是 %#v", patterns)
	}
	if hook.Feature != "hook_type" || !hook.Governed {
		t.Fatalf("入表的特征要给出键名并标为已入表，实际 %#v", hook)
	}
	if hook.CardCount != 3 || !hook.Repeated {
		t.Fatalf("三种写法要合成 3 条并算「反复」，实际 %#v", hook)
	}
	if len(hook.Channels) != 2 {
		t.Fatalf("渠道要合起来，实际 %#v", hook.Channels)
	}

	// 入了表但只被提过一次的，照样显示，只是不叫「反复」——藏起来的话，
	// 屏幕会空着，而人有权知道「有这么个特征，被提到过一次」。
	if single := byLabel["字幕密度"]; single.CardCount != 1 || single.Repeated || !single.Governed {
		t.Fatalf("只提过一次的入表特征不该算反复，实际 %#v", single)
	}

	// 没入表的即使凑巧被提了两次也不算反复：没人担保「首图卖点数」和
	// 「首图卖点个数」已经合成一个桶了，这时候说它反复出现过是没有依据的。
	free := byLabel["首图卖点数"]
	if free.CardCount != 2 {
		t.Fatalf("自由文本按原样分桶，实际 %#v", free)
	}
	if free.Governed || free.Repeated {
		t.Fatalf("没入表的不算「反复」，实际 %#v", free)
	}

	// 「主题」在公众号图文里是 headline_theme、在品牌广告里是 theme。猜一个等于
	// 把两类素材的结论混进同一个桶，所以宁可不认。
	if ambiguous := byLabel["主题"]; ambiguous.Governed {
		t.Fatalf("有歧义的中文名不该被认成某一格，实际 %#v", ambiguous)
	}

	if _, ok := byLabel[""]; ok {
		t.Fatal("空白特征名不该成桶")
	}

	// 够得上「反复」的排在最前面，人一眼看到的就是真正反复出现的那些。
	if !patterns[0].Repeated {
		t.Fatalf("反复的要排在最前，实际第一条是 %#v", patterns[0])
	}
}

func featureExperience(conclusion string, features []string, channel string) Experience {
	return Experience{
		Conclusion:    conclusion,
		Judgement:     judge(ConfidenceSufficient, ""),
		Applicability: Applicability{Channels: []string{channel}},
		ContentBasis:  ContentBasis{Features: features},
	}
}
