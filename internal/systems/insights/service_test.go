package insights

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

func TestConfirmedReportBecomesReusableExperienceAndPreLaunchReference(t *testing.T) {
	t.Parallel()
	service := testService()
	actor := testActor()
	report, err := service.CreateReport(context.Background(), actor, "project_1", CreateReportRequest{
		ExecutionID: "deliveryexecution_1",
		Summary:     "蓝色主视觉获得更高的模拟点击意向",
		Findings:    []string{"首图信息密度适中", "标题利益点明确"},
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err = service.ConfirmReport(context.Background(), actor, "project_1", report.ID, report.Version)
	if err != nil {
		t.Fatal(err)
	}
	experience, err := service.CreateExperience(context.Background(), actor, "project_1", report.ID, report.Version, CreateExperienceRequest{
		Conclusion:      "面对新品种草时，首图保持单一利益点。",
		Conditions:      []string{"小红书图文", "新品首发"},
		Counterexamples: []string{"复杂参数对比内容"},
		// 「统计观察 + 充分」收敛出 ✅ 能归因，才过得了下游默认引用集的第二道闸。
		CardType:   CardStatistic,
		Confidence: ConfidenceSufficient,
		DataBasis:  DataBasis{AssetCount: 6, SampleSize: 42000, Metrics: []string{"点击率"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if experience.Status != ExperiencePending || experience.LineageID != experience.ID || experience.Revision != 1 {
		t.Fatalf("a deposited conclusion must start as 待确认: %#v", experience)
	}
	pending, err := service.GetPreLaunch(context.Background(), actor, "project_1", PreLaunchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending.ExperienceReferences) != 0 {
		t.Fatalf("unconfirmed experience must not be quotable: %#v", pending)
	}
	experience, err = service.ConfirmExperience(context.Background(), actor, "project_1", experience.ID, experience.Version)
	if err != nil {
		t.Fatal(err)
	}
	preLaunch, err := service.GetPreLaunch(context.Background(), actor, "project_1", PreLaunchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if experience.Status != ExperienceConfirmed || len(preLaunch.ExperienceReferences) != 1 ||
		preLaunch.ExperienceReferences[0].ID != experience.ID {
		t.Fatalf("experience=%#v prelaunch=%#v", experience, preLaunch)
	}
}

func TestRetiredExperienceStopsBeingQuotableButStaysAuditable(t *testing.T) {
	t.Parallel()
	service := testService()
	actor := testActor()
	experience := confirmedExperience(t, service, actor)
	retired, err := service.RetireExperience(context.Background(), actor, "project_1", experience.ID, ExperienceTransitionRequest{
		ExpectedVersion: experience.Version, Reason: "平台口径变更，结论不再成立。",
	})
	if err != nil {
		t.Fatal(err)
	}
	preLaunch, err := service.GetPreLaunch(context.Background(), actor, "project_1", PreLaunchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	audits, err := service.ListExperienceAudits(context.Background(), actor, "project_1", experience.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if retired.Status != ExperienceRetired || len(preLaunch.ExperienceReferences) != 0 {
		t.Fatalf("retired=%#v prelaunch=%#v", retired, preLaunch)
	}
	if len(audits) != 3 || audits[2].FromStatus != ExperienceConfirmed || audits[2].ToStatus != ExperienceRetired ||
		audits[2].Reason != "平台口径变更，结论不再成立。" {
		t.Fatalf("retirement must leave an attributable trail: %#v", audits)
	}
	stored, err := service.ListExperiences(context.Background(), actor, "project_1", ExperienceRetired, 50)
	if err != nil || len(stored) != 1 {
		t.Fatalf("logical delete must keep the row readable: values=%#v err=%v", stored, err)
	}
}

func TestRetiringExperienceRequiresReasonAndConfirmScope(t *testing.T) {
	t.Parallel()
	service := testService()
	actor := testActor()
	experience := confirmedExperience(t, service, actor)
	if _, err := service.RetireExperience(context.Background(), actor, "project_1", experience.ID, ExperienceTransitionRequest{
		ExpectedVersion: experience.Version,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error=%v", err)
	}
	writer := testActor()
	writer.Scopes = []contract.Scope{ScopeRead, ScopeWrite}
	_, err := service.RetireExperience(context.Background(), writer, "project_1", experience.ID, ExperienceTransitionRequest{
		ExpectedVersion: experience.Version, Reason: "无确认权限也应被拒绝。",
	})
	if err == nil || !strings.Contains(err.Error(), string(ScopeConfirm)) {
		t.Fatalf("error=%v", err)
	}
}

// 被质疑的经验挂个标记，不从引用集里撤下来。
//
// 老做法是把它转成「待复审」状态，于是它当场从所有下游消失。但「有人觉得该重新
// 看一眼」和「这条结论不成立」是两回事——前者是提醒，后者才是决定。让提醒顺手
// 撤掉一条正在被引用的经验，等于让任何一个人的怀疑单方面改写别人的依据。
func TestChallengedExperienceIsFlaggedNotWithdrawn(t *testing.T) {
	t.Parallel()
	service := testService()
	actor := testActor()
	experience := confirmedExperience(t, service, actor)
	flagged, err := service.RequestExperienceReview(context.Background(), actor, "project_1", experience.ID, ExperienceTransitionRequest{
		ExpectedVersion: experience.Version, Reason: "新一轮数据与该结论冲突。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if flagged.Status != ExperienceConfirmed || !flagged.NeedsReview {
		t.Fatalf("标复审不该改状态：%#v", flagged)
	}
	if !flagged.Quotable() || flagged.ReviewHint() == "" {
		t.Fatalf("标了复审仍然在用，但界面上要说出来：%#v", flagged)
	}
	preLaunch, err := service.GetPreLaunch(context.Background(), actor, "project_1", PreLaunchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(preLaunch.ExperienceReferences) == 0 {
		t.Fatalf("标了复审的经验仍然该出现在投前参考里：%#v", preLaunch)
	}
	// 重新看过、还成立，就是再确认一次；确认要把标记摘掉，否则它会一直挂着。
	back, err := service.ConfirmExperience(context.Background(), actor, "project_1", flagged.ID, flagged.Version)
	if err != nil || back.Status != ExperienceConfirmed || back.NeedsReview {
		t.Fatalf("复审完成后标记要摘掉：%#v err=%v", back, err)
	}
}

func TestRevisionSupersedesPredecessorOnlyAfterItIsConfirmed(t *testing.T) {
	t.Parallel()
	service := testService()
	actor := testActor()
	original := confirmedExperience(t, service, actor)
	revision, err := service.ReviseExperience(context.Background(), actor, "project_1", original.ID, ReviseExperienceRequest{
		ExpectedVersion: original.Version,
		Conclusion:      "面对新品种草时，首图保持单一利益点，并在标题重复该利益点。",
		Conditions:      []string{"小红书图文", "新品首发"},
		Counterexamples: []string{"复杂参数对比内容"},
		Reason:          "补充标题层面的适用条件。",
		// 修订不重述依据的话，档位会掉回默认的「假设 + 方向性」——那是有意的，
		// 改了说法却没说清凭什么，本来就不该继续顶着 ✅。这条测的是版本接替，
		// 所以把依据照抄过来，让前后两版停在同一档。
		CardType:   CardStatistic,
		Confidence: ConfidenceSufficient,
		DataBasis:  DataBasis{AssetCount: 6, SampleSize: 42000, Metrics: []string{"点击率"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision.Revision != 2 || revision.LineageID != original.LineageID || revision.SupersedesID != original.ID ||
		revision.Status != ExperiencePending {
		t.Fatalf("revision=%#v original=%#v", revision, original)
	}
	stillOriginal, err := service.GetPreLaunch(context.Background(), actor, "project_1", PreLaunchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stillOriginal.ExperienceReferences) != 1 || stillOriginal.ExperienceReferences[0].ID != original.ID {
		t.Fatalf("an unconfirmed revision must not replace the live conclusion: %#v", stillOriginal)
	}
	confirmed, err := service.ConfirmExperience(context.Background(), actor, "project_1", revision.ID, revision.Version)
	if err != nil {
		t.Fatal(err)
	}
	quotable, err := service.GetPreLaunch(context.Background(), actor, "project_1", PreLaunchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(quotable.ExperienceReferences) != 1 || quotable.ExperienceReferences[0].ID != confirmed.ID {
		t.Fatalf("confirming a revision must leave exactly one quotable conclusion: %#v", quotable)
	}
	lineage, err := service.ListExperienceLineage(context.Background(), actor, "project_1", confirmed.ID)
	if err != nil {
		t.Fatal(err)
	}
	superseded, err := service.ListExperiences(context.Background(), actor, "project_1", ExperienceRetired, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(lineage) != 2 || len(superseded) != 1 || superseded[0].ID != original.ID ||
		superseded[0].SupersededByID != confirmed.ID {
		t.Fatalf("lineage=%#v superseded=%#v", lineage, superseded)
	}
}

func TestReferenceFeedbackOnlyAttachesToQuotableExperience(t *testing.T) {
	t.Parallel()
	service := testService()
	actor := testActor()
	experience := confirmedExperience(t, service, actor)
	reference, err := service.RecordExperienceReference(context.Background(), actor, "project_1", experience.ID, RecordExperienceReferenceRequest{
		ConsumerKind: "creative_task", ConsumerID: "creativetask_1", Outcome: ReferenceAdopted,
		Note: "首图按该结论收敛为单一利益点。",
	})
	if err != nil {
		t.Fatal(err)
	}
	values, err := service.ListExperienceReferences(context.Background(), actor, "project_1", experience.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].ID != reference.ID || values[0].Outcome != ReferenceAdopted {
		t.Fatalf("references=%#v", values)
	}
	if _, err := service.RetireExperience(context.Background(), actor, "project_1", experience.ID, ExperienceTransitionRequest{
		ExpectedVersion: experience.Version, Reason: "结论失效。",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RecordExperienceReference(context.Background(), actor, "project_1", experience.ID, RecordExperienceReferenceRequest{
		ConsumerKind: "creative_task", ConsumerID: "creativetask_2", Outcome: ReferenceReferenced,
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("error=%v", err)
	}
	kept, err := service.ListExperienceReferences(context.Background(), actor, "project_1", experience.ID, 50)
	if err != nil || len(kept) != 1 {
		t.Fatalf("existing reference history must survive retirement: %#v err=%v", kept, err)
	}
}

func TestProjectReferenceListSpansAllExperiences(t *testing.T) {
	t.Parallel()
	service := testService()
	actor := testActor()
	first := confirmedExperience(t, service, actor)
	second := confirmedExperience(t, service, actor)
	for _, item := range []struct {
		experienceID string
		consumerID   string
	}{{first.ID, "creativetask_1"}, {second.ID, "creativetask_2"}} {
		if _, err := service.RecordExperienceReference(context.Background(), actor, "project_1", item.experienceID, RecordExperienceReferenceRequest{
			ConsumerKind: "creative_task", ConsumerID: item.consumerID, Outcome: ReferenceAdopted,
		}); err != nil {
			t.Fatal(err)
		}
	}
	values, err := service.ListProjectExperienceReferences(context.Background(), actor, "project_1", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 {
		t.Fatalf("引用记录视图需要一次拿到全项目的引用: %#v", values)
	}
}

func TestConcludedExperimentReportsBackToItsSourceExperience(t *testing.T) {
	t.Parallel()
	// 「拿去验证」把假设送进实验中心，实验下了结论就必须回到这条经验上。
	// 少了这一步，一条被推翻的假设在经验库里还是原样躺着，下一个人照着它继续做。
	service := testService()
	actor := testActor()
	experience := confirmedExperience(t, service, actor)
	experiment := Experiment{
		ID: "insightexperiment_1", OrganizationID: actor.OrganizationID, ProjectID: "project_1",
		Title: "钩子类型 A/B", SourceExperienceID: experience.ID,
		Status: ExperimentConcluded, Verdict: VerdictRefuted,
		Interpretation: "换成问题式开场之后点击率反而更低，这条假设先不要外推。",
	}
	service.noteExperimentOnSourceExperience(context.Background(), actor, "project_1", experiment)

	values, err := service.ListExperienceReferences(context.Background(), actor, "project_1", experience.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("实验下结论后经验上应有一条引用记录: %#v", values)
	}
	got := values[0]
	// 判定是「推翻」，引用结果就得是「没采纳」——记成引用过，等于把证伪的结果藏起来。
	if got.ConsumerKind != "experiment" || got.ConsumerID != experiment.ID || got.Outcome != ReferenceRejected {
		t.Fatalf("reference=%#v", got)
	}
	if !strings.Contains(got.Note, "钩子类型 A/B") || !strings.Contains(got.Note, "推翻这条假设") ||
		!strings.Contains(got.Note, experiment.Interpretation) {
		t.Fatalf("备注要能独立读懂是哪次实验、判了什么、人怎么解读: %q", got.Note)
	}

	// 空白新建的实验没有出处，不该凭空往哪条经验上记一笔。
	service.noteExperimentOnSourceExperience(context.Background(), actor, "project_1", Experiment{
		ID: "insightexperiment_2", Status: ExperimentConcluded, Verdict: VerdictSupported,
	})
	all, err := service.ListProjectExperienceReferences(context.Background(), actor, "project_1", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("没有出处的实验不该产生引用记录: %#v", all)
	}
}

func TestExperimentVerdictMapsToReferenceOutcome(t *testing.T) {
	t.Parallel()
	// 「看不出差别」既没证实也没推翻。把它记成采纳或没采纳，都是替人下了实验没下的判断。
	for verdict, want := range map[ExperimentVerdict]ExperienceReferenceOutcome{
		VerdictSupported:    ReferenceAdopted,
		VerdictRefuted:      ReferenceRejected,
		VerdictInconclusive: ReferenceReferenced,
	} {
		if got := experimentReferenceOutcome(verdict); got != want {
			t.Fatalf("verdict=%q outcome=%q want=%q", verdict, got, want)
		}
	}
}

func TestExperienceTransitionRejectsStaleVersion(t *testing.T) {
	t.Parallel()
	service := testService()
	actor := testActor()
	experience := confirmedExperience(t, service, actor)
	if _, err := service.RetireExperience(context.Background(), actor, "project_1", experience.ID, ExperienceTransitionRequest{
		ExpectedVersion: experience.Version - 1, Reason: "版本已过期。",
	}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("error=%v", err)
	}
}

func TestExperienceInheritsTheReportWindow(t *testing.T) {
	t.Parallel()
	// 经验卡上「统计窗口」这一格，录入表单里没有，也不该有——报告已经定格了
	// 这个判断基于哪一段数据。不自动带过来，这一格就永远是空的，
	// 引用这条经验的人无从判断它说的是哪一段时间的事。
	service := testService()
	actor := testActor()
	repository, ok := service.Repository.(*memoryRepository)
	if !ok {
		t.Fatal("测试仓库类型变了")
	}
	report, err := service.CreateReport(context.Background(), actor, "project_1", CreateReportRequest{ExecutionID: "deliveryexecution_1"})
	if err != nil {
		t.Fatal(err)
	}
	// 窗口本来是建报告时由投后分析那一步定格的。这里直接补上，
	// 免得把整条分析链拖进这个用例。
	stored := repository.reports[report.ID]
	stored.WindowStart, stored.WindowEnd = "2026-07-01", "2026-07-20"
	repository.reports[report.ID] = stored
	report, err = service.ConfirmReport(context.Background(), actor, "project_1", report.ID, report.Version)
	if err != nil {
		t.Fatal(err)
	}
	experience, err := service.CreateExperience(context.Background(), actor, "project_1", report.ID, report.Version, CreateExperienceRequest{
		Conclusion: "面对新品种草时，首图保持单一利益点。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if experience.DataBasis.WindowStart == nil || experience.DataBasis.WindowEnd == nil {
		t.Fatalf("经验要继承报告的统计窗口，实际是 %#v", experience.DataBasis)
	}
	if got := experience.DataBasis.WindowStart.Format("2006-01-02"); got != "2026-07-01" {
		t.Fatalf("窗口起点=%s", got)
	}
	if got := experience.DataBasis.WindowEnd.Format("2006-01-02"); got != "2026-07-20" {
		t.Fatalf("窗口终点=%s", got)
	}
	revision, err := service.ReviseExperience(context.Background(), actor, "project_1", experience.ID, ReviseExperienceRequest{
		ExpectedVersion: experience.Version,
		Conclusion:      "面对新品种草时，首图保持单一利益点，并在标题重复该利益点。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision.DataBasis.WindowStart == nil || revision.DataBasis.WindowEnd == nil {
		t.Fatalf("修订改的是说法不是数据来源，窗口不能在这一步丢掉：%#v", revision.DataBasis)
	}
}

func TestRevisingAnUnconfirmedExperienceRetiresIt(t *testing.T) {
	t.Parallel()
	// 修订一条还没确认的经验时，前身必须当场退休。两版同时挂在待确认队列里，
	// 看上去就是两条各自独立的经验；确认了旧的那一版，刚补进去的内容全丢，
	// 而且没有任何地方会提示丢了。
	service := testService()
	actor := testActor()
	report, err := service.CreateReport(context.Background(), actor, "project_1", CreateReportRequest{ExecutionID: "deliveryexecution_1"})
	if err != nil {
		t.Fatal(err)
	}
	report, err = service.ConfirmReport(context.Background(), actor, "project_1", report.ID, report.Version)
	if err != nil {
		t.Fatal(err)
	}
	original, err := service.CreateExperience(context.Background(), actor, "project_1", report.ID, report.Version, CreateExperienceRequest{
		Conclusion: "面对新品种草时，首图保持单一利益点。",
	})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := service.ReviseExperience(context.Background(), actor, "project_1", original.ID, ReviseExperienceRequest{
		ExpectedVersion: original.Version,
		Conclusion:      "面对新品种草时，首图保持单一利益点，并在标题重复该利益点。",
		Reason:          "补充标题层面的适用条件。",
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.ListExperiences(context.Background(), actor, "project_1", ExperiencePending, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != revision.ID {
		t.Fatalf("待确认队列里只该剩最新那一版，实际是 %#v", pending)
	}
	lineage, err := service.ListExperienceLineage(context.Background(), actor, "project_1", revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(lineage) != 2 {
		t.Fatalf("两版都要留在同一条脉络里可查，实际是 %#v", lineage)
	}
	previous := lineage[0]
	if previous.ID != original.ID || previous.Status != ExperienceRetired || previous.SupersededByID != revision.ID {
		t.Fatalf("前身要退休并指向新版本，实际是 %#v", previous)
	}
	// 退休了就不能再从它身上修订，否则会分出一条谁也说不清的支线。
	if _, err := service.ReviseExperience(context.Background(), actor, "project_1", original.ID, ReviseExperienceRequest{
		ExpectedVersion: previous.Version, Conclusion: "再改一版。",
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("error=%v", err)
	}
}

func confirmedExperience(t *testing.T, service Service, actor contract.ActorContext) Experience {
	t.Helper()
	report, err := service.CreateReport(context.Background(), actor, "project_1", CreateReportRequest{ExecutionID: "deliveryexecution_1"})
	if err != nil {
		t.Fatal(err)
	}
	report, err = service.ConfirmReport(context.Background(), actor, "project_1", report.ID, report.Version)
	if err != nil {
		t.Fatal(err)
	}
	// 写死「统计观察 + 充分」而不是用默认值：默认落在「假设 + 方向性」，
	// 那一档收敛出来是 👁 只是观察，进不了下游的默认引用集。用它做夹具的话，
	// 这些测的是状态流转的用例会因为判定档位不够而失败，读的人会以为
	// 是状态流转坏了。
	experience, err := service.CreateExperience(context.Background(), actor, "project_1", report.ID, report.Version, CreateExperienceRequest{
		Conclusion:      "面对新品种草时，首图保持单一利益点。",
		Conditions:      []string{"小红书图文"},
		Counterexamples: []string{"复杂参数对比内容"},
		CardType:        CardStatistic,
		Confidence:      ConfidenceSufficient,
		DataBasis:       DataBasis{AssetCount: 6, SampleSize: 42000, Metrics: []string{"点击率"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	experience, err = service.ConfirmExperience(context.Background(), actor, "project_1", experience.ID, experience.Version)
	if err != nil {
		t.Fatal(err)
	}
	return experience
}

func TestInsightsRejectsExperienceFromUnconfirmedReport(t *testing.T) {
	t.Parallel()
	service := testService()
	actor := testActor()
	report, _ := service.CreateReport(context.Background(), actor, "project_1", CreateReportRequest{
		ExecutionID: "deliveryexecution_1", Summary: "摘要", Findings: []string{"发现"},
	})
	if _, err := service.CreateExperience(context.Background(), actor, "project_1", report.ID, report.Version, CreateExperienceRequest{
		Conclusion: "结论", Conditions: []string{}, Counterexamples: []string{},
	}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("error=%v", err)
	}
}

func TestCreateReportDerivesFindingsFromSimulatedMetricSnapshot(t *testing.T) {
	t.Parallel()
	service := testService()
	report, err := service.CreateReport(context.Background(), testActor(), "project_1", CreateReportRequest{
		ExecutionID: "deliveryexecution_1",
		Summary:     "client supplied summary must be ignored",
		Findings:    []string{"client supplied finding must be ignored"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.IsSimulated || report.MetricSnapshotID != "deliverymetric_1" ||
		report.CreativePackageID != "creativepackage_1" {
		t.Fatalf("report lineage=%#v", report)
	}
	if strings.Contains(report.Summary, "client supplied") || strings.Contains(strings.Join(report.Findings, " "), "client supplied") {
		t.Fatalf("report must be server-derived: %#v", report)
	}
	if len(report.Digest) != 0 || report.WindowStart != "" || report.WindowEnd != "" {
		t.Fatalf("不带窗口的报告不该有汇总：%#v", report)
	}
}

// 带窗口的报告必须真的把三处结论取回来。取不到时要报错，不能悄悄建成一份空报告——
// 人在投后分析页点的是「定格这一屏」，拿到一份没有内容的报告比拿到错误更难发现。
func TestCreateReportWithWindowFailsRatherThanFreezingNothing(t *testing.T) {
	t.Parallel()
	service := testService() // 没有 Connectors / Assets / Experiments
	_, err := service.CreateReport(context.Background(), testActor(), "project_1", CreateReportRequest{
		ExecutionID: "deliveryexecution_1",
		Window: MetricWindow{
			Start: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
		},
	})
	if err == nil {
		t.Fatal("窗口分析取不到时，报告不该创建成功")
	}
}

func TestDropReportFindingMarksInsteadOfRemoving(t *testing.T) {
	t.Parallel()
	repository := &memoryRepository{reports: map[string]InsightReport{}, experiences: map[string]Experience{}}
	service := testService()
	service.Repository = repository
	actor := testActor()

	report, err := service.CreateReport(context.Background(), actor, "project_1", CreateReportRequest{
		ExecutionID: "deliveryexecution_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	// 直接塞进仓储：这条用例要验的是删减本身，不是汇总怎么算出来的。
	seeded := repository.reports[report.ID]
	seeded.Digest = []ReportFinding{
		{Kind: SectionAssetPerformance, Text: "首图单一利益点的组明显更好。"},
		{Kind: SectionExperiment, Text: "实验 A 支持这一点。"},
	}
	repository.reports[report.ID] = seeded

	dropped, err := service.DropReportFinding(context.Background(), actor, "project_1", report.ID, seeded.Version, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped.Digest) != 2 || !dropped.Digest[1].Dropped || dropped.Digest[0].Dropped {
		t.Fatalf("删减应只打标记不删条目：%#v", dropped.Digest)
	}
	if dropped.Version != seeded.Version+1 {
		t.Fatalf("version=%d", dropped.Version)
	}

	restored, err := service.DropReportFinding(context.Background(), actor, "project_1", report.ID, dropped.Version, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Digest[1].Dropped {
		t.Fatalf("放回去应清掉标记：%#v", restored.Digest)
	}

	if _, err := service.DropReportFinding(context.Background(), actor, "project_1", report.ID, restored.Version, 9, true); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("越界下标 error=%v", err)
	}
	if _, err := service.DropReportFinding(context.Background(), actor, "project_1", report.ID, restored.Version-1, 0, true); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("版本冲突 error=%v", err)
	}

	confirmed, err := service.ConfirmReport(context.Background(), actor, "project_1", report.ID, restored.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DropReportFinding(context.Background(), actor, "project_1", report.ID, confirmed.Version, 0, true); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("已确认的报告不该还能删减 error=%v", err)
	}
}

func testActor() contract.ActorContext {
	return contract.ActorContext{
		OrganizationID: "org_1",
		Principal:      contract.Principal{Kind: contract.PrincipalUser, ID: "user_1"},
		Scopes:         []contract.Scope{ScopeRead, ScopeWrite, ScopeConfirm},
	}
}

func testService() Service {
	sequence := 0
	return Service{
		Repository: &memoryRepository{reports: map[string]InsightReport{}, experiences: map[string]Experience{}},
		Projects:   testProjects{},
		Delivery:   testDelivery{},
		Now:        func() time.Time { return time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC) },
		NewID: func(prefix string) (string, error) {
			sequence++
			return fmt.Sprintf("%s_%d", prefix, sequence), nil
		},
	}
}

type testProjects struct{}

// 返回的东西要跟真实的 project.Service 一样过得了 ValidateBrandBound——
// 那边出口就校验了品牌与产品列表。桩比真实实现宽松的话，一个组不出合法
// 项目上下文的 bug 在单测里全绿，到线上才被供应商层拒掉。
func (testProjects) RequireActiveContext(_ context.Context, actor contract.ActorContext, projectID contract.ProjectID) (contract.ProjectContext, error) {
	brandID := contract.BrandID("brand_1")
	value := contract.ProjectContext{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, ProjectContextVersion: 1,
		BrandID: &brandID, ProductIDs: []contract.ProductID{"product_1"},
	}
	if err := value.ValidateBrandBound(); err != nil {
		panic(err)
	}
	return value, nil
}

type testDelivery struct{}

func (testDelivery) ReadExecution(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, id string) (DeliveryExecutionSnapshot, error) {
	return DeliveryExecutionSnapshot{
		ID: id, ChangeSetID: "deliverychangeset_1", PlanID: "deliveryplan_1",
		Mode: "local_simulation", EvidenceID: "deliveryevidence_1",
		CreativePackageID: "creativepackage_1",
		MetricSnapshot: &DeliveryMetricSnapshot{
			ID: "deliverymetric_1", DatasetVersion: "preroll-demo/v1", Source: "demo_fixture",
			IsSimulated: true, Currency: "CNY",
			RawMetrics: RawMetrics{Impressions: 10000, Clicks: 420, Conversions: 31, SpendCents: 50000},
		},
		EvidenceSummary: "本地模拟执行完成，无真实广告平台写入。",
	}, nil
}

func (testDelivery) ListExecutions(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, _ int) ([]DeliveryExecutionSnapshot, error) {
	value, _ := (testDelivery{}).ReadExecution(context.Background(), contract.ActorContext{}, "", "deliveryexecution_1")
	return []DeliveryExecutionSnapshot{value}, nil
}

type memoryRepository struct {
	reports     map[string]InsightReport
	experiences map[string]Experience
	order       []string
	references  []ExperienceReference
	audits      []ExperienceAudit
}

func (r *memoryRepository) CreateReport(_ context.Context, value InsightReport) (InsightReport, error) {
	r.reports[value.ID] = value
	return value, nil
}
func (r *memoryRepository) ListReports(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, _ int) ([]InsightReport, error) {
	values := make([]InsightReport, 0)
	for _, value := range r.reports {
		if value.OrganizationID == organizationID && value.ProjectID == projectID {
			values = append(values, value)
		}
	}
	return values, nil
}
func (r *memoryRepository) GetReport(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (InsightReport, error) {
	value, ok := r.reports[id]
	if !ok || value.OrganizationID != organizationID || value.ProjectID != projectID {
		return InsightReport{}, ErrNotFound
	}
	return value, nil
}
func (r *memoryRepository) FindDraftByWindow(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, windowStart, windowEnd string) (InsightReport, error) {
	// map 遍历顺序是随机的，所以这里按 ID 取最小的那份，而不是「碰到的第一份」。
	// 不定序的伪造仓储会让「记两笔进同一份草稿」这类测试偶发失败。
	var found InsightReport
	for _, value := range r.reports {
		if value.OrganizationID != organizationID || value.ProjectID != projectID ||
			value.Status != ReportDraft || value.WindowStart != windowStart || value.WindowEnd != windowEnd {
			continue
		}
		if found.ID == "" || value.ID < found.ID {
			found = value
		}
	}
	if found.ID == "" {
		return InsightReport{}, ErrNotFound
	}
	return found, nil
}
func (r *memoryRepository) ConfirmReport(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string, expectedVersion int64, actorID string, now time.Time) (InsightReport, error) {
	value, err := r.GetReport(context.Background(), organizationID, projectID, id)
	if err != nil {
		return InsightReport{}, err
	}
	if value.Version != expectedVersion {
		return InsightReport{}, ErrVersionConflict
	}
	value.Status = ReportConfirmed
	value.Version++
	value.ConfirmedBy = actorID
	value.ConfirmedAt = &now
	value.UpdatedAt = now
	r.reports[id] = value
	return value, nil
}
func (r *memoryRepository) PurgeEmptyDrafts(_ context.Context, before time.Time) (int64, error) {
	purged := int64(0)
	for id, value := range r.reports {
		if value.Status == ReportDraft && len(value.Digest) == 0 && value.CreatedAt.Before(before) {
			delete(r.reports, id)
			purged++
		}
	}
	return purged, nil
}
func (r *memoryRepository) SubmitReport(_ context.Context, input SubmitReportInput) (InsightReport, error) {
	value, err := r.GetReport(context.Background(), input.OrganizationID, input.ProjectID, input.ReportID)
	if err != nil {
		return InsightReport{}, err
	}
	if value.Status != ReportDraft {
		return InsightReport{}, ErrInvalidState
	}
	if value.Version != input.ExpectedVersion {
		return InsightReport{}, ErrVersionConflict
	}
	value.ExecutionID = input.ExecutionID
	value.Summary = input.Summary
	value.Digest = input.Digest
	value.Status = ReportConfirmed
	value.Version++
	value.ConfirmedBy = input.ActorID
	value.ConfirmedAt = &input.At
	value.UpdatedAt = input.At
	r.reports[input.ReportID] = value
	return value, nil
}
func (r *memoryRepository) UpdateReportDigest(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string, expectedVersion int64, digest []ReportFinding, now time.Time) (InsightReport, error) {
	value, err := r.GetReport(context.Background(), organizationID, projectID, id)
	if err != nil {
		return InsightReport{}, err
	}
	if value.Version != expectedVersion {
		return InsightReport{}, ErrVersionConflict
	}
	if value.Status != ReportDraft {
		return InsightReport{}, ErrInvalidState
	}
	value.Digest = digest
	value.Version++
	value.UpdatedAt = now
	r.reports[id] = value
	return value, nil
}
func (r *memoryRepository) CreateExperience(_ context.Context, value Experience, audit ExperienceAudit) (Experience, error) {
	r.experiences[value.ID] = value
	r.order = append(r.order, value.ID)
	r.audits = append(r.audits, audit)
	return value, nil
}
func (r *memoryRepository) ReviseExperience(ctx context.Context, input ReviseExperienceInput) (Experience, error) {
	value, err := r.CreateExperience(ctx, input.Value, input.Audit)
	if err != nil || input.RetireSource == nil {
		return value, err
	}
	retired, err := r.TransitionExperience(ctx, *input.RetireSource)
	if err != nil {
		return Experience{}, err
	}
	retired.SupersededByID = value.ID
	r.experiences[retired.ID] = retired
	return value, nil
}
func (r *memoryRepository) ListExperiences(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, status ExperienceStatus, _ int) ([]Experience, error) {
	values := make([]Experience, 0)
	for _, id := range r.order {
		value := r.experiences[id]
		if value.OrganizationID != organizationID || value.ProjectID != projectID {
			continue
		}
		if status != "" && value.Status != status {
			continue
		}
		values = append(values, value)
	}
	return values, nil
}
func (r *memoryRepository) GetExperience(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (Experience, error) {
	value, ok := r.experiences[id]
	if !ok || value.OrganizationID != organizationID || value.ProjectID != projectID {
		return Experience{}, ErrNotFound
	}
	return value, nil
}
func (r *memoryRepository) ListExperienceLineage(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, lineageID string) ([]Experience, error) {
	values := make([]Experience, 0)
	for _, id := range r.order {
		value := r.experiences[id]
		if value.OrganizationID == organizationID && value.ProjectID == projectID && value.LineageID == lineageID {
			values = append(values, value)
		}
	}
	return values, nil
}
func (r *memoryRepository) TransitionExperience(ctx context.Context, input TransitionExperienceInput) (Experience, error) {
	value, err := r.GetExperience(ctx, input.OrganizationID, input.ProjectID, input.ID)
	if err != nil {
		return Experience{}, err
	}
	if value.Version != input.ExpectedVersion {
		return Experience{}, ErrVersionConflict
	}
	if !containsStatus(input.From, value.Status) {
		return Experience{}, ErrInvalidState
	}
	r.audits = append(r.audits, ExperienceAudit{
		ID: input.AuditID, OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		ExperienceID: value.ID, FromStatus: value.Status, ToStatus: input.To,
		Reason: input.Reason, ActorID: input.ActorID, CreatedAt: input.Now,
	})
	value.Status = input.To
	value.StatusReason = input.Reason
	value.StatusChangedBy = input.ActorID
	value.StatusChangedAt = &input.Now
	value.Version++
	value.UpdatedAt = input.Now
	r.experiences[value.ID] = value
	return value, nil
}
func (r *memoryRepository) FlagExperienceForReview(_ context.Context, input FlagExperienceReviewInput) (Experience, error) {
	value, ok := r.experiences[input.ID]
	if !ok || value.OrganizationID != input.OrganizationID || value.ProjectID != input.ProjectID {
		return Experience{}, ErrNotFound
	}
	if value.Version != input.ExpectedVersion {
		return Experience{}, ErrVersionConflict
	}
	if value.Status != ExperienceConfirmed {
		return Experience{}, ErrInvalidState
	}
	r.audits = append(r.audits, ExperienceAudit{
		ID: input.AuditID, OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		ExperienceID: value.ID, FromStatus: ExperienceConfirmed, ToStatus: ExperienceConfirmed,
		Reason: input.Reason, ActorID: input.ActorID, CreatedAt: input.Now,
	})
	value.NeedsReview = input.NeedsReview
	value.StatusReason = input.Reason
	value.StatusChangedBy = input.ActorID
	value.StatusChangedAt = &input.Now
	value.Version++
	value.UpdatedAt = input.Now
	r.experiences[value.ID] = value
	return value, nil
}
func (r *memoryRepository) ConfirmExperience(ctx context.Context, input ConfirmExperienceInput) (Experience, error) {
	value, err := r.TransitionExperience(ctx, TransitionExperienceInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, ID: input.ID,
		ExpectedVersion: input.ExpectedVersion,
		From:            []ExperienceStatus{ExperiencePending, ExperienceConfirmed},
		To:              ExperienceConfirmed, ActorID: input.ActorID, Now: input.Now, AuditID: input.AuditID,
	})
	if err != nil {
		return Experience{}, err
	}
	value.ConfirmedBy = input.ActorID
	value.ConfirmedAt = &input.Now
	value.NeedsReview = false
	r.experiences[value.ID] = value
	if value.SupersedesID == "" {
		return value, nil
	}
	previous, err := r.GetExperience(ctx, input.OrganizationID, input.ProjectID, value.SupersedesID)
	if err != nil || previous.Status == ExperienceRetired {
		return value, nil
	}
	superseded, err := r.TransitionExperience(ctx, TransitionExperienceInput{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, ID: previous.ID,
		ExpectedVersion: previous.Version,
		From:            []ExperienceStatus{ExperiencePending, ExperienceConfirmed},
		To:              ExperienceRetired, Reason: "已被第 " + strconv.Itoa(value.Revision) + " 版取代。",
		ActorID: input.ActorID, Now: input.Now, AuditID: input.SupersedeAuditID,
	})
	if err != nil {
		return Experience{}, err
	}
	superseded.SupersededByID = value.ID
	r.experiences[superseded.ID] = superseded
	return value, nil
}
func (r *memoryRepository) CreateExperienceReference(_ context.Context, value ExperienceReference) (ExperienceReference, error) {
	r.references = append(r.references, value)
	return value, nil
}
func (r *memoryRepository) ListExperienceReferences(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, experienceID string, _ int) ([]ExperienceReference, error) {
	values := make([]ExperienceReference, 0)
	for _, value := range r.references {
		if value.OrganizationID != organizationID || value.ProjectID != projectID {
			continue
		}
		// 空 experienceID 表示"整个项目的引用记录"，与 MySQL 实现保持一致。
		if experienceID != "" && value.ExperienceID != experienceID {
			continue
		}
		values = append(values, value)
	}
	return values, nil
}
func (r *memoryRepository) ListExperienceAudits(_ context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, experienceID string, _ int) ([]ExperienceAudit, error) {
	values := make([]ExperienceAudit, 0)
	for _, value := range r.audits {
		if value.OrganizationID == organizationID && value.ProjectID == projectID && value.ExperienceID == experienceID {
			values = append(values, value)
		}
	}
	return values, nil
}

func containsStatus(values []ExperienceStatus, value ExperienceStatus) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

// 下游默认引用有两道闸：状态在用，判定能归因。
//
// 只看状态是不够的。一条「👁 只是观察」的经验也可以被人确认——确认的是
// 「这个观察值得记下来」，不是「这个因果成立」。让它进默认引用集，下一轮就会
// 有人照着一个没排除混杂的观察去做素材，而他不会知道自己在赌。
func TestOnlyExplainedExperiencesAreReusableByDefault(t *testing.T) {
	t.Parallel()

	explained := Experience{Status: ExperienceConfirmed, Judgement: judge(ConfidenceSufficient, "")}
	if !explained.Reusable() {
		t.Error("在用且能归因的经验应该可默认引用")
	}

	observed := Experience{Status: ExperienceConfirmed, Judgement: judge(ConfidenceDirectional, "")}
	if observed.Reusable() {
		t.Error("只是观察的经验不该进默认引用集")
	}

	pending := Experience{Status: ExperiencePending, Judgement: judge(ConfidenceSufficient, "")}
	if pending.Reusable() {
		t.Error("还没人确认的经验不该被引用")
	}
}

// 标了「该看一眼了」的经验仍然在用。这正是把它从状态改成标记的理由：
// 读的地方只认 confirmed，不会因为漏判一个状态就让它凭空消失。
func TestFlaggedExperienceStaysUsable(t *testing.T) {
	t.Parallel()

	value := Experience{Status: ExperienceConfirmed, NeedsReview: true, Judgement: judge(ConfidenceSufficient, "")}
	if !value.Reusable() {
		t.Error("标了复审的经验仍然在用，仍然可引用")
	}
	if value.ReviewHint() == "" {
		t.Error("标了复审就要在界面上说出来，否则这个标记等于没有")
	}
	if value.StatusLabel() != "在用" {
		t.Errorf("状态标签应该是「在用」，得到 %q", value.StatusLabel())
	}
}

func TestStatusLabelsAreTheThreeAgreedWords(t *testing.T) {
	t.Parallel()

	cases := map[ExperienceStatus]string{
		ExperiencePending:   "待定",
		ExperienceConfirmed: "在用",
		ExperienceRetired:   "停用",
	}
	for status, want := range cases {
		if got := (Experience{Status: status}).StatusLabel(); got != want {
			t.Errorf("%s 的标签应该是 %q，得到 %q", status, want, got)
		}
	}
}
