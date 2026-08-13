package insights

import (
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 提交复盘。
//
// 这一步做四件事，缺一件这份复盘都不完整：
//  1. 记下这一轮的一句话摘要——全流程唯一能写它的地方；
//  2. 记下这份复盘算哪次投放（可以没有，见下）；
//  3. 把系统发现定格进去，和人一路记的那几笔合并去重；
//  4. 状态改成已确认，从此不可改。
//
// 四件事必须在一条 UPDATE 里。分开做的话，中间断电会留下一份「已确认但没有系统发现」
// 的报告，而它看起来和正常的一模一样——没人会怀疑那份复盘漏了东西。

// SubmitReviewRequest 是提交复盘的入参。
type SubmitReviewRequest struct {
	// ExecutionID 是这份复盘算哪次投放。**可以为空。**
	//
	// 原来这里是必填。理由听上去成立（复盘本来就该对应一轮真实投放），但它把
	// 「还没跑过投放」的人整个挡在门外：草稿是记一笔自动建的，人记了一屏发现，
	// 到提交这一步才被告知「先去跑一次投放」——而他要复盘的恰恰是没投出去的
	// 那批素材分析。doc22 §6.4 验收 5 只要求「可以创建当前 Project 的复盘报告」，
	// 没有这个前置；03 AM-015 也没有。
	//
	// 空着不是含糊：报告详情会明写「这一轮没挂投放执行」，而不是留白。
	ExecutionID string `json:"execution_id"`
	// Summary 是这一轮的一句话摘要，比如「7 月中旬短视频三版对比」。
	//
	// 它是复盘列表主列上唯一能把几份复盘区分开的东西——没有它，列表就是一列
	// 「（这一轮还没写摘要）」，人只能靠时间戳猜哪份是哪份。提交之后报告不可改，
	// 所以这也是唯一能写它的时刻，界面上必须说清这一点。
	//
	// 留空是允许的：逼人写一句话，换来的多半是「复盘」两个字。留空时沿用报告
	// 已有的摘要（模拟投放建的报告自带一句），不会把已有的抹成空。
	Summary string `json:"summary"`
	// ExpectedVersion 防并发覆盖：两个人同时开着这份草稿，后提交的那个
	// 会把先提交的删改抹掉，而两边都不会看到任何提示。
	ExpectedVersion int64 `json:"expected_version"`
}

// SubmitReportInput 是提交那一下要写进库里的全部东西。
//
// 用结构体不用一串位置参数：executionID 和 summary 都是 string 而且挨着，传反了
// 编译器一声不吭，结果是复盘挂到了别的投放上、摘要变成一串 ID，两边都不会报错。
type SubmitReportInput struct {
	OrganizationID  contract.OrganizationID
	ProjectID       contract.ProjectID
	ReportID        string
	ExpectedVersion int64
	// ExecutionID 可以是空串：这一轮没有对应的投放执行。
	ExecutionID string
	// Summary 已经由调用方兜过底（人没写就是报告原来那句），仓储直接写。
	Summary string
	Digest  []ReportFinding
	ActorID string
	At      time.Time
}

// summaryLimit 是摘要的字数上限。它是列表主列上的一行，超过这个长度的不是摘要，
// 是把复盘正文粘进了标题——列表会被撑成一堵墙，反而更认不出哪份是哪份。
const summaryLimit = 200

func (r SubmitReviewRequest) Validate() error {
	if r.ExpectedVersion <= 0 {
		return ErrInvalidRequest
	}
	if len([]rune(strings.TrimSpace(r.Summary))) > summaryLimit {
		return ErrInvalidRequest
	}
	return nil
}

// hasContent 判断这份复盘要不要出现在列表里。
//
// 空草稿是「记一笔」自动建了但人什么都没记的残留。它和「人记了又全删了」不一样：
// 后者是一个明确的决定——这一轮我看过，什么都不值得留——清掉它等于抹掉那个决定。
// 所以判据是 digest 长度，不是「有几条没被删」。
func hasContent(report InsightReport) bool {
	if report.Status != ReportDraft {
		return true
	}
	return len(report.Digest) > 0
}

// checkSubmittable 单独拆出来，是为了让「什么样的复盘能提交」这条规则
// 能被直接测到，不用先造一个仓储。
func checkSubmittable(report InsightReport, expectedVersion int64) error {
	if report.Status != ReportDraft {
		return ErrInvalidState
	}
	if report.Version != expectedVersion {
		return ErrVersionConflict
	}
	return nil
}
