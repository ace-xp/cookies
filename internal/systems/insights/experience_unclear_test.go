package insights

import (
	"context"
	"errors"
	"testing"
)

// ❓算不出来的那条发现，留不成经验。
//
// 记一笔可以钉它——「这一轮连这个都判断不了」本身是这一轮的事实，而且它自带下一步
// （找相似素材把样本做厚）。但经验是一句主张，会被下一轮当依据引用；把一条
// 「判断不了」变成经验，等于凭空造出一个从来没算出来过的结论，而它在经验库里
// 长得和真结论一模一样，谁也认不出来。
func TestUnclearFindingCannotBecomeAnExperience(t *testing.T) {
	t.Parallel()
	service := testService()
	actor := testActor()
	ctx := context.Background()

	report, err := service.CreateReport(ctx, actor, "project_1", CreateReportRequest{ExecutionID: "deliveryexecution_1"})
	if err != nil {
		t.Fatal(err)
	}

	// 直接往仓储里塞两条定格发现：一条算不出来，一条能归因。走完整分析链路造一条
	// ❓要先把样本量压到阈值以下，那是在测阈值，不是在测这条规则。
	store := service.Repository.(*memoryRepository)
	stored := store.reports[report.ID]
	stored.Digest = []ReportFinding{
		{
			Text: "15 秒版本和 30 秒版本谁更好，这一轮判断不了。", Origin: OriginPinned,
			Dimension: "comparisons", Variable: "duration",
			Judgement: judge(ConfidenceLowSample, "样本太少，差异存不存在都说不准。"),
		},
		{
			Text: "开场露脸的一组转化更好。", Origin: OriginPinned,
			Dimension: "drivers", Variable: "opening_face",
			Judgement: judge(ConfidenceSufficient, "样本充分、区间不重叠。"),
		},
	}
	store.reports[report.ID] = stored

	if stored.Digest[0].Verdict != VerdictUnclear {
		t.Fatalf("夹具没造出「算不出来」这一档：%#v", stored.Digest[0].Judgement)
	}
	if stored.Digest[1].Verdict != VerdictExplained {
		t.Fatalf("夹具没造出「能归因」这一档：%#v", stored.Digest[1].Judgement)
	}

	report, err = service.ConfirmReport(ctx, actor, "project_1", report.ID, report.Version)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.CreateExperience(ctx, actor, "project_1", report.ID, report.Version, CreateExperienceRequest{
		Conclusion: "短视频统一做 15 秒。",
		Conditions: []string{"小红书图文"},
		CardType:   CardStatistic,
		Confidence: ConfidenceSufficient,
		DataBasis:  DataBasis{AssetCount: 6, SampleSize: 42000, Metrics: []string{"点击率"}},
		SourceFinding: &FindingRef{
			Dimension: "comparisons", Variable: "duration",
		},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("从「算不出来」的发现留经验应该被拒，得到 %v", err)
	}

	// 同一份报告里那条能归因的照样留得成——这条规则拦的是那一条发现，不是这份报告。
	experience, err := service.CreateExperience(ctx, actor, "project_1", report.ID, report.Version, CreateExperienceRequest{
		Conclusion: "开场三秒露脸。",
		Conditions: []string{"小红书图文"},
		CardType:   CardStatistic,
		Confidence: ConfidenceSufficient,
		DataBasis:  DataBasis{AssetCount: 6, SampleSize: 42000, Metrics: []string{"点击率"}},
		SourceFinding: &FindingRef{
			Dimension: "drivers", Variable: "opening_face",
		},
	})
	if err != nil {
		t.Fatalf("能归因的那条不该被这条规则拦住：%v", err)
	}
	if experience.Verdict != VerdictExplained {
		t.Errorf("经验应该继承那条发现的判定，得到 %q", experience.Verdict)
	}
}
