package insights

import (
	"errors"
	"strings"
	"testing"
)

// 系统发现在**提交那一刻**才定格进去，不是草稿一建就补。
//
// 草稿是活的：人一边看分析一边往里记，这中间窗口没变但数据每天在变。
// 一建就补的话，人记第一笔时补进来的那批，到提交的时候数字早就不是那个数了，
// 而报告上不会写它是哪天算的。
func TestSubmitReviewFreezesSystemFindingsAtSubmitTime(t *testing.T) {
	t.Parallel()

	pinned := []ReportFinding{{
		Text: "15 秒版本点击率更高。", Origin: OriginPinned,
		Dimension: "comparisons", Variable: "duration",
		Judgement: judge(ConfidenceSufficient, "样本充分、区间不重叠。"),
	}}
	system := []ReportFinding{
		{Text: "时长 15s 组更高。", Origin: OriginSystem, Dimension: "comparisons", Variable: "duration"},
		{Text: "开场有人脸的一组转化更好。", Origin: OriginSystem, Dimension: "drivers", Variable: "opening_face"},
	}

	frozen := mergeFindings(pinned, system)
	if len(frozen) != 2 {
		t.Fatalf("定格结果应该是 2 条，得到 %d 条", len(frozen))
	}
	if frozen[0].Origin != OriginPinned {
		t.Error("人记的排在前面")
	}
}

func TestSubmitReviewRequestValidation(t *testing.T) {
	t.Parallel()

	valid := SubmitReviewRequest{ExecutionID: "exec_1", ExpectedVersion: 3}
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法请求被拒了：%v", err)
	}

	// 没挂投放执行照样能提交。原来这里是必填，结果是「还没跑过投放」的人
	// 记了一屏发现之后被挡在提交那一步——而他要复盘的正是那批还没投出去的素材。
	// doc22 §6.4 验收 5 只要求「可以创建当前 Project 的复盘报告」，没有这个前置。
	noExecution := SubmitReviewRequest{ExpectedVersion: 3}
	if err := noExecution.Validate(); err != nil {
		t.Errorf("没挂投放执行不该拦住提交：%v", err)
	}

	noVersion := SubmitReviewRequest{ExecutionID: "exec_1"}
	if err := noVersion.Validate(); err == nil {
		t.Error("没有版本号的提交应该被拒——并发编辑会静默覆盖")
	}

	// 摘要是列表主列上的一行。粘一整段进来，列表会被撑成一堵墙，
	// 比原来那列「（这一轮还没写摘要）」更认不出哪份是哪份。
	longSummary := SubmitReviewRequest{ExpectedVersion: 3, Summary: strings.Repeat("摘", summaryLimit+1)}
	if err := longSummary.Validate(); err == nil {
		t.Error("超长摘要应该被拒")
	}
	atLimit := SubmitReviewRequest{ExpectedVersion: 3, Summary: strings.Repeat("摘", summaryLimit)}
	if err := atLimit.Validate(); err != nil {
		t.Errorf("正好到上限的摘要不该被拒：%v", err)
	}
}

// 已提交的复盘不能再提交一次。第二次提交会用今天的数据重新定格一遍系统发现，
// 而引用了第一版的人手上那份就变成假的了。
func TestSubmitReviewRejectsAlreadyConfirmedReports(t *testing.T) {
	t.Parallel()

	report := InsightReport{Status: ReportConfirmed, Version: 1}
	if err := checkSubmittable(report, 1); !errors.Is(err, ErrInvalidState) {
		t.Errorf("已确认的复盘应该拒绝提交，得到 %v", err)
	}
}

// 空草稿不该出现在复盘列表里。
//
// 草稿是「记一笔」自动建的（P1），人从来不主动建复盘。所以一份没有任何发现的草稿，
// 意味着人只是打开看了看——把它列出来，复盘列表很快就会被一堆空壳填满，
// 而真正有内容的那几份混在里面找不着。
func TestEmptyDraftsAreHiddenFromTheList(t *testing.T) {
	t.Parallel()

	empty := InsightReport{Status: ReportDraft, Digest: []ReportFinding{}}
	if hasContent(empty) {
		t.Error("没有任何发现的草稿应该算没被碰过")
	}

	touched := InsightReport{Status: ReportDraft, Digest: []ReportFinding{{Text: "记了一笔"}}}
	if !hasContent(touched) {
		t.Error("有发现的草稿应该显示")
	}

	// 人把唯一那条删了，草稿仍然算被碰过：他做过一个明确的决定，
	// 这份草稿代表「这一轮我看过，什么都不值得留」。清掉它等于抹掉那个决定。
	emptied := InsightReport{Status: ReportDraft, Digest: []ReportFinding{{Text: "记了一笔", Dropped: true}}}
	if !hasContent(emptied) {
		t.Error("删空了的草稿仍然算被碰过")
	}

	// 已确认的复盘不管有没有发现都要显示——它是这一轮的正式记录。
	confirmed := InsightReport{Status: ReportConfirmed, Digest: []ReportFinding{}}
	if !hasContent(confirmed) {
		t.Error("已确认的复盘永远显示")
	}
}

func TestSubmitReviewChecksVersion(t *testing.T) {
	t.Parallel()

	report := InsightReport{Status: ReportDraft, Version: 5}
	if err := checkSubmittable(report, 3); !errors.Is(err, ErrVersionConflict) {
		t.Errorf("版本对不上应该报冲突，得到 %v", err)
	}
	if err := checkSubmittable(report, 5); err != nil {
		t.Errorf("版本对得上应该放行，得到 %v", err)
	}
}
