package insights

import "testing"

// 阈值印只有一个合法来源：这条经验留的是复盘里哪一条发现。
//
// 这一组用例守的是同一件事的两面——该盖的时候盖上（否则改完阈值之后没人说得清
// 一条老经验当初按什么标准判的），不该盖的时候一个号码都不许出现（人自己敲的一句
// 结论盖上「按第 N 版阈值判定」，等于拿阈值给一个人工判断背书）。
func TestInheritJudgementStampsOnlyWhenTheFindingSaysSo(t *testing.T) {
	version := int64(7)
	stamped := judgeAt(ResolvedThresholds{Version: version}, ConfidenceSufficient, "样本够、差距稳。")
	report := InsightReport{Digest: []ReportFinding{
		{
			Kind: SectionAssetPerformance, Text: "首图单一利益点的点击率更高。",
			Judgement: stamped, Origin: OriginPinned,
			Dimension: "comparisons", Variable: "首图卖点数", SourceRef: "asset_a",
		},
		{
			Kind: SectionAssetPerformance, Text: "这条被人删了。",
			Judgement: stamped, Origin: OriginSystem,
			Dimension: "trends", Variable: "", SourceRef: "asset_b",
			Dropped: true,
		},
	}}
	source := &FindingRef{Dimension: "comparisons", Variable: "首图卖点数", SourceRef: "asset_a"}

	cases := []struct {
		name    string
		request CreateExperienceRequest
		// wantVersion 为 nil 表示「这一条不许盖印」。
		wantVersion    *int64
		wantConfidence ConfidenceLevel
	}{
		{
			name:        "挑了发现且没另填档位：整条判定原样继承",
			request:     CreateExperienceRequest{SourceFinding: source},
			wantVersion: &version, wantConfidence: ConfidenceSufficient,
		},
		{
			name:        "挑了发现且填的是同一档：还是系统那一次判的，照样盖印",
			request:     CreateExperienceRequest{SourceFinding: source, Confidence: ConfidenceSufficient},
			wantVersion: &version, wantConfidence: ConfidenceSufficient,
		},
		{
			name:        "挑了发现但人改成了另一档：听人的，不盖印",
			request:     CreateExperienceRequest{SourceFinding: source, Confidence: ConfidenceDirectional},
			wantVersion: nil, wantConfidence: ConfidenceDirectional,
		},
		{
			name:        "没挑发现，自己敲一句结论：这里根本没有阈值参与过",
			request:     CreateExperienceRequest{Confidence: ConfidenceSufficient},
			wantVersion: nil, wantConfidence: ConfidenceSufficient,
		},
		{
			name:        "指了一条空的发现：等同于没挑",
			request:     CreateExperienceRequest{SourceFinding: &FindingRef{}, Confidence: ConfidenceSufficient},
			wantVersion: nil, wantConfidence: ConfidenceSufficient,
		},
		{
			name: "指的那条已经被人删了：宁可空着，不猜",
			request: CreateExperienceRequest{
				SourceFinding: &FindingRef{Dimension: "trends", SourceRef: "asset_b"},
				Confidence:    ConfidenceSufficient,
			},
			wantVersion: nil, wantConfidence: ConfidenceSufficient,
		},
		{
			name: "指的那条报告里没有：同样不猜",
			request: CreateExperienceRequest{
				SourceFinding: &FindingRef{Dimension: "fatigue", SourceRef: "asset_z"},
				Confidence:    ConfidenceSufficient,
			},
			wantVersion: nil, wantConfidence: ConfidenceSufficient,
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			confidence := item.request.Confidence
			if confidence == "" {
				confidence = ConfidenceSufficient
			}
			got := inheritJudgement(report, item.request, confidence)
			if got.Confidence != item.wantConfidence {
				t.Fatalf("档位应为 %q，实际 %q", item.wantConfidence, got.Confidence)
			}
			if got.Verdict != item.wantConfidence.Verdict() || got.VerdictLabel != got.Verdict.Label() {
				t.Fatalf("档位和收敛结果对不上：%#v", got)
			}
			assertThresholdVersion(t, got.ThresholdVersion, item.wantVersion)
		})
	}
}

// 修订改的是结论的说法，不是重新算了一遍。档位没动，阈值印就跟着搬过来；
// 档位动了，那是人自己判的，旧号码必须摘掉——留着会让人以为系统按第 N 版重算过。
func TestRevisedJudgementKeepsStampOnlyWhenConfidenceUnchanged(t *testing.T) {
	version := int64(3)
	source := Experience{Judgement: judgeAt(ResolvedThresholds{Version: version}, ConfidenceSufficient, "")}

	kept := revisedJudgement(source, ConfidenceSufficient)
	assertThresholdVersion(t, kept.ThresholdVersion, &version)

	dropped := revisedJudgement(source, ConfidenceDirectional)
	if dropped.Confidence != ConfidenceDirectional {
		t.Fatalf("修订要听新档位的，实际 %q", dropped.Confidence)
	}
	assertThresholdVersion(t, dropped.ThresholdVersion, nil)

	// 前一版本来就没有号码（人自己填的档位、老数据），修订也变不出一个来。
	blank := revisedJudgement(Experience{Judgement: judge(ConfidenceSufficient, "")}, ConfidenceSufficient)
	assertThresholdVersion(t, blank.ThresholdVersion, nil)
}

func assertThresholdVersion(t *testing.T, got *int64, want *int64) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Fatalf("这一条不许盖阈值印，实际盖了第 %d 版", *got)
	case want != nil && got == nil:
		t.Fatalf("应盖第 %d 版阈值印，实际一个号码都没有", *want)
	case want != nil && *got != *want:
		t.Fatalf("阈值印应为第 %d 版，实际第 %d 版", *want, *got)
	}
}
