// Package insights owns evidence-backed reports and reusable experience.
package insights

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/ids"
	"github.com/shikanon/cookies/internal/systems/insights/skills"
)

const (
	ScopeRead    contract.Scope = "insights.read"
	ScopeWrite   contract.Scope = "insights.write"
	ScopeConfirm contract.Scope = "insights.confirm"
)

var (
	ErrNotFound        = errors.New("insights resource not found")
	ErrInvalidRequest  = errors.New("insights request is invalid")
	ErrInvalidState    = errors.New("insights resource is not in a state that allows this action")
	ErrVersionConflict = errors.New("insights resource version conflict")

	// ErrUnderstandingPending 是「模型正在看这条视频，还没看完」。
	//
	// 单独一个哨兵，不并进 ErrInvalidState：那个的意思是「这事现在做不了」，
	// 人看到会去改点什么；这个的意思是「这事正在做」，人只需要等。两者在界面上
	// 该长得完全不一样——一个是红的要人处理，一个是转圈的让人喝口水。
	ErrUnderstandingPending = errors.New("insights media understanding is still running")
)

type ReportStatus string

const (
	ReportDraft     ReportStatus = "draft"
	ReportConfirmed ReportStatus = "confirmed"
)

// ExperienceStatus 是经验的生命周期：待定 -> 在用 -> 停用。停用是逻辑删除，
// 引用过它的地方仍然查得到，所以一条不再能用的结论仍然是可审计的。
//
// 三态。「待复审」不在这里——它是「在用」上的一个标记（NeedsReview），
// 不是一个独立状态：被标记的经验仍然在用、仍然能被引用，只是该重新看一眼。
// 做成状态的话，每个读经验的地方都得判断「confirmed 或者 needs_review」，
// 漏一处那条经验就在某个页面上凭空消失了。
type ExperienceStatus string

const (
	ExperiencePending   ExperienceStatus = "pending"
	ExperienceConfirmed ExperienceStatus = "confirmed"
	ExperienceRetired   ExperienceStatus = "retired"
)

func (s ExperienceStatus) valid() bool {
	switch s {
	case ExperiencePending, ExperienceConfirmed, ExperienceRetired:
		return true
	}
	return false
}

// ExperienceReferenceOutcome records what a downstream consumer did with a
// confirmed experience (AM-014).
type ExperienceReferenceOutcome string

const (
	ReferenceReferenced ExperienceReferenceOutcome = "referenced"
	ReferenceAdopted    ExperienceReferenceOutcome = "adopted"
	ReferenceModified   ExperienceReferenceOutcome = "modified"
	ReferenceRejected   ExperienceReferenceOutcome = "rejected"
)

func (o ExperienceReferenceOutcome) valid() bool {
	switch o {
	case ReferenceReferenced, ReferenceAdopted, ReferenceModified, ReferenceRejected:
		return true
	}
	return false
}

type DeliveryExecutionSnapshot struct {
	ID                string                  `json:"id"`
	ChangeSetID       string                  `json:"change_set_id"`
	PlanID            string                  `json:"plan_id"`
	CreativePackageID string                  `json:"creative_package_id"`
	Mode              string                  `json:"mode"`
	EvidenceID        string                  `json:"evidence_id"`
	EvidenceSummary   string                  `json:"evidence_summary"`
	MetricSnapshot    *DeliveryMetricSnapshot `json:"metric_snapshot,omitempty"`
}

type RawMetrics struct {
	Impressions int64 `json:"impressions"`
	Clicks      int64 `json:"clicks"`
	Conversions int64 `json:"conversions"`
	SpendCents  int64 `json:"spend_cents"`
}

type DeliveryMetricSnapshot struct {
	ID             string     `json:"id"`
	Source         string     `json:"source"`
	IsSimulated    bool       `json:"is_simulated"`
	DatasetVersion string     `json:"dataset_version"`
	Currency       string     `json:"currency"`
	RawMetrics     RawMetrics `json:"raw_metrics"`
}

type CreateReportRequest struct {
	ExecutionID string   `json:"execution_id"`
	Summary     string   `json:"summary"`
	Findings    []string `json:"findings"`

	// Window 是要定格的数据窗口。来自投后分析页当前选的窗口——「看到什么定格什么」，
	// 不让后端偷偷默认（20 §4.1：数据窗口必须能被看到）。后端自己挑一个窗口的话，
	// 人看到的那一屏和报告里定格的会是两回事，而两边都写着同一个日期区间。
	Window MetricWindow `json:"window"`
}

func (r CreateReportRequest) Validate() error {
	if strings.TrimSpace(r.ExecutionID) == "" {
		return ErrInvalidRequest
	}
	if !r.Window.Start.IsZero() && !r.Window.End.IsZero() && r.Window.End.Before(r.Window.Start) {
		return ErrInvalidRequest
	}
	return nil
}

// PinFindingRequest 是「记一笔」的入参。
//
// 注意这里**没有** confidence 也没有 verdict：判定是后端回查出来的。
// Text 也是可选的——人可以补一句自己的话，但不补的话用系统给的措辞，
// 而不是让前端把屏幕上的字传上来。
type PinFindingRequest struct {
	Window    MetricWindow `json:"window"`
	Dimension string       `json:"dimension"`
	SourceRef string       `json:"source_ref,omitempty"`
	Variable  string       `json:"variable,omitempty"`
	// Text 是人自己补的一句话，最多 500 字。留空则用系统给这一条的措辞。
	Text string `json:"text,omitempty"`
}

func (r PinFindingRequest) Validate() error {
	if r.Window.Start.IsZero() || r.Window.End.IsZero() || r.Window.End.Before(r.Window.Start) {
		return ErrInvalidRequest
	}
	if _, ok := analysisDimensions[r.Dimension]; !ok {
		return ErrInvalidRequest
	}
	// 两个主语都空就回查不到任何一条，只会记下一条没有出处的文字。
	if strings.TrimSpace(r.SourceRef) == "" && strings.TrimSpace(r.Variable) == "" &&
		r.Dimension != "overview" {
		return ErrInvalidRequest
	}
	if len(r.Text) > 500 {
		return ErrInvalidRequest
	}
	return nil
}

type InsightReport struct {
	ID                string                  `json:"id"`
	OrganizationID    contract.OrganizationID `json:"organization_id"`
	ProjectID         contract.ProjectID      `json:"project_id"`
	ExecutionID       string                  `json:"execution_id"`
	DeliveryMode      string                  `json:"delivery_mode"`
	EvidenceID        string                  `json:"evidence_id"`
	EvidenceSummary   string                  `json:"evidence_summary"`
	MetricSnapshotID  string                  `json:"metric_snapshot_id"`
	CreativePackageID string                  `json:"creative_package_id"`
	IsSimulated       bool                    `json:"is_simulated"`
	DatasetVersion    string                  `json:"dataset_version"`
	Status            ReportStatus            `json:"status"`
	Summary           string                  `json:"summary"`
	Findings          []string                `json:"findings"`

	// Digest 是定格的四块发现。Findings（[]string）保留，是旧报告的兼容读法。
	Digest []ReportFinding `json:"digest"`
	// 定格的数据窗口。报告存的是「在这个时点、基于这份数据窗口、做了这个判断」。
	// 旧报告没有窗口，这两个字段为空——空比编一个进去诚实。
	WindowStart string `json:"window_start,omitempty"`
	WindowEnd   string `json:"window_end,omitempty"`

	Version     int64      `json:"version"`
	CreatedBy   string     `json:"created_by"`
	ConfirmedBy string     `json:"confirmed_by,omitempty"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateExperienceRequest struct {
	Conclusion      string   `json:"conclusion"`
	Conditions      []string `json:"conditions"`
	Counterexamples []string `json:"counterexamples"`

	// 洞察卡剩下的五个字段（03 §8.1）。类型和置信有默认值，是为了让老调用方
	// 不填也能过——但默认落在最保守的一格（假设 / 方向性），不替录入的人做判断。
	CardType          InsightCardType `json:"card_type"`
	Confidence        ConfidenceLevel `json:"confidence"`
	RecommendedAction string          `json:"recommended_action"`
	Applicability     Applicability   `json:"applicability"`
	DataBasis         DataBasis       `json:"data_basis"`
	ContentBasis      ContentBasis    `json:"content_basis"`
}

// withCardDefaults 补上没填的类型和置信。放在这里而不是各调用点，
// 是为了让「不填等于假设 + 方向性」只有一处定义。
func (r CreateExperienceRequest) withCardDefaults() CreateExperienceRequest {
	if strings.TrimSpace(string(r.CardType)) == "" {
		r.CardType = CardHypothesis
	}
	if strings.TrimSpace(string(r.Confidence)) == "" {
		r.Confidence = ConfidenceDirectional
	}
	return r
}

func (r CreateExperienceRequest) Validate() error {
	if strings.TrimSpace(r.Conclusion) == "" || len(r.Conclusion) > 2000 ||
		len(r.Conditions) > 20 || len(r.Counterexamples) > 20 {
		return ErrInvalidRequest
	}
	for _, values := range [][]string{r.Conditions, r.Counterexamples} {
		for _, value := range values {
			if strings.TrimSpace(value) == "" || len(value) > 500 {
				return ErrInvalidRequest
			}
		}
	}
	filled := r.withCardDefaults()
	if err := validateApplicability(filled.Applicability); err != nil {
		return err
	}
	if err := validateContentBasis(filled.ContentBasis); err != nil {
		return err
	}
	return validateCard(filled.CardType, filled.Confidence, filled.RecommendedAction, filled.DataBasis)
}

type Experience struct {
	ID                     string                  `json:"id"`
	OrganizationID         contract.OrganizationID `json:"organization_id"`
	ProjectID              contract.ProjectID      `json:"project_id"`
	LineageID              string                  `json:"lineage_id"`
	Revision               int                     `json:"revision"`
	SupersedesID           string                  `json:"supersedes_id,omitempty"`
	SupersededByID         string                  `json:"superseded_by_id,omitempty"`
	ReportID               string                  `json:"report_id"`
	SourceExecutionID      string                  `json:"source_execution_id"`
	SourceEvidenceID       string                  `json:"source_evidence_id"`
	SourceMetricSnapshotID string                  `json:"source_metric_snapshot_id"`
	Conclusion             string                  `json:"conclusion"`
	Conditions             []string                `json:"conditions"`
	Counterexamples        []string                `json:"counterexamples"`
	CardType               InsightCardType         `json:"card_type"`
	// Judgement 内嵌而不是只留一个 Confidence：一条经验能不能被下游默认引用，
	// 取决于三档里的哪一档，而三档只能由 judge() 从 Confidence 收敛出来。
	// 摆一个裸 Confidence 让每个读的地方自己换算，迟早出现两页给出不同档位。
	Judgement
	RecommendedAction string           `json:"recommended_action"`
	Applicability     Applicability    `json:"applicability"`
	DataBasis         DataBasis        `json:"data_basis"`
	ContentBasis      ContentBasis     `json:"content_basis"`
	Status            ExperienceStatus `json:"status"`
	// NeedsReview 是「该看一眼了」。它不影响这条经验能不能用，只影响界面怎么显示。
	NeedsReview     bool       `json:"needs_review"`
	StatusReason    string     `json:"status_reason"`
	StatusChangedBy string     `json:"status_changed_by"`
	StatusChangedAt *time.Time `json:"status_changed_at,omitempty"`
	ConfirmedBy     string     `json:"confirmed_by,omitempty"`
	ConfirmedAt     *time.Time `json:"confirmed_at,omitempty"`
	Version         int64      `json:"version"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// Reusable 决定下游能不能默认引用这条经验。**两道闸，缺一不可。**
//
// 状态在用只说明有人认可它值得记下来；判定能归因才说明这个因果排除过混杂。
// 一条「👁 只是观察」的经验也可能被确认——确认的是「这个观察值得记」，
// 不是「照着做会有同样的结果」。放它进默认引用集，下一轮就会有人照着一个
// 没排除混杂的观察去做素材，而他不会知道自己在赌。
//
// 要引用 👁 的经验不是不行，但必须由人显式点名，不能是默认发生的。
func (e Experience) Reusable() bool {
	return e.Quotable() && e.Verdict == VerdictExplained
}

// Quotable 是第一道闸单独拿出来：这条经验能不能被人点名引用。
//
// 待定的还没人认可，停用的已经作废，这两种谁都不该往外抄。但一条 👁 只是观察的
// 在用经验是可以被点名引用的——人看了它的依据，自己决定用，那是他的判断。
// Quotable 管的是「允不允许人引用」，Reusable 管的是「系统要不要默认替他引用」。
func (e Experience) Quotable() bool {
	return e.Status == ExperienceConfirmed
}

func (e Experience) StatusLabel() string {
	switch e.Status {
	case ExperiencePending:
		return "待定"
	case ExperienceConfirmed:
		return "在用"
	case ExperienceRetired:
		return "停用"
	}
	return string(e.Status)
}

// ReviewHint 只在界面上用。空串表示不用提示。
func (e Experience) ReviewHint() string {
	if e.NeedsReview {
		return "该看一眼了"
	}
	return ""
}

// ExperienceAudit is the append-only trail behind PRD §11.2. Nothing is
// physically deleted, so every status change stays attributable.
type ExperienceAudit struct {
	ID             string                  `json:"id"`
	OrganizationID contract.OrganizationID `json:"organization_id"`
	ProjectID      contract.ProjectID      `json:"project_id"`
	ExperienceID   string                  `json:"experience_id"`
	FromStatus     ExperienceStatus        `json:"from_status"`
	ToStatus       ExperienceStatus        `json:"to_status"`
	Reason         string                  `json:"reason"`
	ActorID        string                  `json:"actor_id"`
	CreatedAt      time.Time               `json:"created_at"`
}

type ExperienceReference struct {
	ID             string                     `json:"id"`
	OrganizationID contract.OrganizationID    `json:"organization_id"`
	ProjectID      contract.ProjectID         `json:"project_id"`
	ExperienceID   string                     `json:"experience_id"`
	ConsumerKind   string                     `json:"consumer_kind"`
	ConsumerID     string                     `json:"consumer_id"`
	Outcome        ExperienceReferenceOutcome `json:"outcome"`
	Note           string                     `json:"note"`
	Version        int64                      `json:"version"`
	CreatedBy      string                     `json:"created_by"`
	CreatedAt      time.Time                  `json:"created_at"`
	UpdatedAt      time.Time                  `json:"updated_at"`
}

// ExperienceTransitionRequest carries the human reason a conclusion moved
// state. Rejection, review and retirement all require one.
type ExperienceTransitionRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

func (r ExperienceTransitionRequest) validate(reasonRequired bool) error {
	reason := strings.TrimSpace(r.Reason)
	if len(reason) > 1000 || (reasonRequired && reason == "") {
		return ErrInvalidRequest
	}
	return nil
}

// ReviseExperienceRequest supersedes a conclusion instead of overwriting it
// (PRD §7.6). The revision starts as 待确认 and only replaces its predecessor
// once someone confirms it.
type ReviseExperienceRequest struct {
	ExpectedVersion int64    `json:"expected_version"`
	Conclusion      string   `json:"conclusion"`
	Conditions      []string `json:"conditions"`
	Counterexamples []string `json:"counterexamples"`
	Reason          string   `json:"reason"`

	CardType          InsightCardType `json:"card_type"`
	Confidence        ConfidenceLevel `json:"confidence"`
	RecommendedAction string          `json:"recommended_action"`
	Applicability     Applicability   `json:"applicability"`
	DataBasis         DataBasis       `json:"data_basis"`
	ContentBasis      ContentBasis    `json:"content_basis"`
}

// card 把修订请求折回创建请求，让两条路径共用同一套校验和默认值。
func (r ReviseExperienceRequest) card() CreateExperienceRequest {
	return CreateExperienceRequest{
		Conclusion: r.Conclusion, Conditions: r.Conditions, Counterexamples: r.Counterexamples,
		CardType: r.CardType, Confidence: r.Confidence, RecommendedAction: r.RecommendedAction,
		Applicability: r.Applicability, DataBasis: r.DataBasis, ContentBasis: r.ContentBasis,
	}.withCardDefaults()
}

func (r ReviseExperienceRequest) validate() error {
	if err := r.card().Validate(); err != nil {
		return err
	}
	if len(strings.TrimSpace(r.Reason)) > 1000 {
		return ErrInvalidRequest
	}
	return nil
}

type RecordExperienceReferenceRequest struct {
	ConsumerKind string                     `json:"consumer_kind"`
	ConsumerID   string                     `json:"consumer_id"`
	Outcome      ExperienceReferenceOutcome `json:"outcome"`
	Note         string                     `json:"note"`
}

func (r RecordExperienceReferenceRequest) validate() error {
	kind, id := strings.TrimSpace(r.ConsumerKind), strings.TrimSpace(r.ConsumerID)
	if kind == "" || len(kind) > 32 || id == "" || len(id) > 96 ||
		!r.Outcome.valid() || len(strings.TrimSpace(r.Note)) > 1000 {
		return ErrInvalidRequest
	}
	return nil
}

// PreLaunchInsight 与投前洞察的投影逻辑一起放在 prelaunch.go。

type PerformanceOverview struct {
	ProjectID  contract.ProjectID          `json:"project_id"`
	Executions []DeliveryExecutionSnapshot `json:"executions"`
	Disclosure string                      `json:"disclosure"`
}

type ActiveProjectResolver interface {
	RequireActiveContext(context.Context, contract.ActorContext, contract.ProjectID) (contract.ProjectContext, error)
}

// DeliveryReader is Insights' only dependency on Delivery.
type DeliveryReader interface {
	ReadExecution(context.Context, contract.ActorContext, contract.ProjectID, string) (DeliveryExecutionSnapshot, error)
	ListExecutions(context.Context, contract.ActorContext, contract.ProjectID, int) ([]DeliveryExecutionSnapshot, error)
}

// TransitionExperienceInput moves one experience between lifecycle states and
// appends the matching audit row in the same transaction.
type TransitionExperienceInput struct {
	OrganizationID  contract.OrganizationID
	ProjectID       contract.ProjectID
	ID              string
	ExpectedVersion int64
	From            []ExperienceStatus
	To              ExperienceStatus
	Reason          string
	ActorID         string
	Now             time.Time
	AuditID         string
}

// FlagExperienceReviewInput 只动 needs_review 那一格，状态不变。
//
// 审计仍然照写一条，from_status 和 to_status 都是 confirmed——状态确实没变，
// 审计如实记。要在审计里看出发生了什么，看 reason。
type FlagExperienceReviewInput struct {
	OrganizationID  contract.OrganizationID
	ProjectID       contract.ProjectID
	ID              string
	ExpectedVersion int64
	NeedsReview     bool
	Reason          string
	ActorID         string
	Now             time.Time
	AuditID         string
}

// ConfirmExperienceInput confirms a revision and, when it supersedes an older
// one, retires the predecessor atomically so two revisions are never reusable
// at the same time.
type ConfirmExperienceInput struct {
	OrganizationID   contract.OrganizationID
	ProjectID        contract.ProjectID
	ID               string
	ExpectedVersion  int64
	ActorID          string
	Now              time.Time
	AuditID          string
	SupersedeAuditID string
}

// ReviseExperienceInput appends a revision to a lineage. RetireSource is set
// only when the predecessor is still 待确认 — an unconfirmed row nobody can
// quote yet, so retiring it costs nothing and keeps the candidate queue down to
// one version per conclusion. A confirmed predecessor stays live instead: it is
// what downstream still quotes, and it only hands over when the revision itself
// is confirmed (ConfirmExperience).
type ReviseExperienceInput struct {
	Value        Experience
	Audit        ExperienceAudit
	RetireSource *TransitionExperienceInput
}

type Repository interface {
	CreateReport(context.Context, InsightReport) (InsightReport, error)
	ListReports(context.Context, contract.OrganizationID, contract.ProjectID, int) ([]InsightReport, error)
	GetReport(context.Context, contract.OrganizationID, contract.ProjectID, string) (InsightReport, error)
	// FindDraftByWindow 按 (项目 + 窗口) 找那份还没提交的复盘草稿。
	// 「记一笔」不问人要往哪记——问了等于要求人在看数据之前先声明意图。
	FindDraftByWindow(context.Context, contract.OrganizationID, contract.ProjectID, string, string) (InsightReport, error)
	ConfirmReport(context.Context, contract.OrganizationID, contract.ProjectID, string, int64, string, time.Time) (InsightReport, error)
	// SubmitReport 在一条 UPDATE 里补执行 ID、写入定格后的 digest、置为已确认。
	// 拆成三次调用会留下「已确认但没有系统发现」的报告，而它看起来和正常的一模一样。
	SubmitReport(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID,
		reportID string, expectedVersion int64, executionID string, digest []ReportFinding,
		actorID string, at time.Time) (InsightReport, error)
	UpdateReportDigest(context.Context, contract.OrganizationID, contract.ProjectID, string, int64, []ReportFinding, time.Time) (InsightReport, error)
	// PurgeEmptyDrafts 删掉 before 之前建的、一条发现都没有的草稿。
	// 它们是「记一笔」建了但人什么都没记的残留，不删会一直占着
	// (项目 + 窗口) 的唯一键。
	PurgeEmptyDrafts(ctx context.Context, before time.Time) (int64, error)
	CreateExperience(context.Context, Experience, ExperienceAudit) (Experience, error)
	ReviseExperience(context.Context, ReviseExperienceInput) (Experience, error)
	ListExperiences(context.Context, contract.OrganizationID, contract.ProjectID, ExperienceStatus, int) ([]Experience, error)
	GetExperience(context.Context, contract.OrganizationID, contract.ProjectID, string) (Experience, error)
	ListExperienceLineage(context.Context, contract.OrganizationID, contract.ProjectID, string) ([]Experience, error)
	TransitionExperience(context.Context, TransitionExperienceInput) (Experience, error)
	FlagExperienceForReview(context.Context, FlagExperienceReviewInput) (Experience, error)
	ConfirmExperience(context.Context, ConfirmExperienceInput) (Experience, error)
	CreateExperienceReference(context.Context, ExperienceReference) (ExperienceReference, error)
	ListExperienceReferences(context.Context, contract.OrganizationID, contract.ProjectID, string, int) ([]ExperienceReference, error)
	ListExperienceAudits(context.Context, contract.OrganizationID, contract.ProjectID, string, int) ([]ExperienceAudit, error)
}

type Service struct {
	Repository Repository
	// Assets backs 分析素材库 and 内容分析. It is a separate interface from
	// Repository because the two lifecycles share nothing but the module.
	Assets AssetRepository
	// ExternalAssets 存的是平台外的素材证据，和 Assets 分成两个接口。
	// 并成一个的话，下一个实现 AssetRepository 的人会以为外部素材是素材的一种，
	// 而共享素材库里的东西是可以被拿去投放的——外部素材没有那份授权。
	ExternalAssets ExternalAssetRepository
	// Connectors backs 数据接入 and the Canonical daily metrics 投后分析 reads.
	Connectors ConnectorRepository
	// Runs 记录每次分析任务的输入、方法、模型版本和结果（03 §344 验收 12）。
	// 与 Assets 分开：分析任务是只增不改的流水，素材是有状态机的实体。
	Runs AnalysisRunRepository
	// Experiments 支撑实验中心。与 Repository 分开的理由同上：
	// 实验有自己的状态机（设计中 → 已开跑 → 已下结论），和复盘报告没有共同点。
	Experiments ExperimentRepository
	Projects    ActiveProjectResolver
	Delivery    DeliveryReader
	// Media 是客观可测层的唯一入口：从素材库读文件已经探测出来的元数据。
	// 为 nil 时 DeriveFeatures 直接失败，不会退化成「按类型猜一个默认时长」。
	Media MediaReader
	// Understanding 是视频语义特征的入口：把视频交给多模态，拿回带帧号的证据。
	// 为 nil 时视频类退回老路（人填画面描述），不是报错——没接多模态的环境里，
	// 提取仍然应该做得成，只是做得糙。
	Understanding MediaUnderstander
	// Text 是特征提取唯一的模型出口。为 nil 时提取直接失败，
	// 不会退化成模板产出——库里一条编造的特征，代价远大于一次失败的提取。
	Text TextGenerator
	// TextModelAlias 是能力别名，不是供应商的模型 ID（10 §凭据只存引用）。
	TextModelAlias string
	// Skills 为 nil 时用内嵌的默认注册表；测试可以换成自己的。
	Skills *skills.Registry
	// Thresholds 存判定阈值的历史版本。为 nil 时判定跑代码里的出厂设定——
	// 阈值仓储没配好不该让整个分析链路瘫掉，跑默认值至少还能出结论，
	// 而版本号 0 会在页面上如实显示成「出厂设定」。
	Thresholds ThresholdRepository
	NewID      ids.Generator
	Now        func() time.Time
}

func (s Service) CreateReport(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, request CreateReportRequest) (InsightReport, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return InsightReport{}, err
	}
	if err := request.Validate(); err != nil {
		return InsightReport{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return InsightReport{}, err
	}
	execution, err := s.Delivery.ReadExecution(ctx, actor, projectID, request.ExecutionID)
	if err != nil {
		return InsightReport{}, err
	}
	if execution.MetricSnapshot == nil || !execution.MetricSnapshot.IsSimulated ||
		execution.MetricSnapshot.Source != "demo_fixture" {
		return InsightReport{}, ErrInvalidState
	}
	id, err := s.idGenerator()("insightreport")
	if err != nil {
		return InsightReport{}, err
	}
	now := s.now()
	summary, findings := summarizeSimulatedMetrics(*execution.MetricSnapshot)

	// 报告不产生新分析，它组织已有分析（20 §118）。这里把投后分析、实验、经验三处
	// 已经算完的结论取过来定格。窗口为空时跳过——旧调用方（以及只想留一份投放数字
	// 存档的场景）不该因为这个新增字段而突然创建失败。
	digest := make([]ReportFinding, 0)
	windowStart, windowEnd := "", ""
	if !request.Window.Start.IsZero() && !request.Window.End.IsZero() {
		analysis, err := s.GetPerformanceAnalysis(ctx, actor, projectID, request.Window)
		if err != nil {
			return InsightReport{}, err
		}
		experiments, err := s.concludedExperimentsInWindow(ctx, actor, projectID, request.Window)
		if err != nil {
			return InsightReport{}, err
		}
		experiences, err := s.ListExperiences(ctx, actor, projectID, ExperienceConfirmed, 50)
		if err != nil {
			return InsightReport{}, err
		}
		digest = buildReportDigest(analysis, experiments, experiences)
		windowStart = request.Window.Start.Format("2006-01-02")
		windowEnd = request.Window.End.Format("2006-01-02")
	}

	return s.Repository.CreateReport(ctx, InsightReport{
		ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID,
		ExecutionID: execution.ID, DeliveryMode: execution.Mode, EvidenceID: execution.EvidenceID,
		EvidenceSummary:  execution.EvidenceSummary,
		MetricSnapshotID: execution.MetricSnapshot.ID, CreativePackageID: execution.CreativePackageID,
		IsSimulated: true, DatasetVersion: execution.MetricSnapshot.DatasetVersion, Status: ReportDraft,
		Summary: summary, Findings: findings,
		Digest: digest, WindowStart: windowStart, WindowEnd: windowEnd,
		Version: 1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	})
}

// PinFinding 把分析页上的一条结论钉进本轮复盘草稿。
//
// 草稿是自动建的：不问人「要往哪份复盘记」。问了等于要求人在看数据之前先声明意图
// ——而记一笔的价值恰恰在于人是看到了才决定要留的。
func (s Service) PinFinding(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, request PinFindingRequest) (InsightReport, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return InsightReport{}, err
	}
	if err := request.Validate(); err != nil {
		return InsightReport{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return InsightReport{}, err
	}

	analysis, err := s.GetPerformanceAnalysis(ctx, actor, projectID, request.Window)
	if err != nil {
		return InsightReport{}, err
	}
	judgement, text, ok := findJudgement(analysis, request.Dimension, request.SourceRef, request.Variable)
	if !ok {
		// 屏幕上没有这一条，说明前端传的主语和当前窗口对不上——多半是窗口换了
		// 但按钮没重渲染。宁可报错，也不记一条没有判定的发现。
		return InsightReport{}, ErrInvalidRequest
	}
	if custom := strings.TrimSpace(request.Text); custom != "" {
		text = custom
	}

	now := s.now()
	finding := ReportFinding{
		Kind:      analysisDimensions[request.Dimension],
		Text:      text,
		Judgement: judgement,
		Origin:    OriginPinned,
		Dimension: request.Dimension,
		Variable:  request.Variable,
		SourceRef: request.SourceRef,
		PinnedBy:  actor.Principal.ID,
		PinnedAt:  &now,
	}

	windowStart := request.Window.Start.Format("2006-01-02")
	windowEnd := request.Window.End.Format("2006-01-02")

	// 这一段是读-改-写，而写要带读到的版本号。记一笔的请求里没有版本号可带——
	// 人按按钮的时候屏幕上根本没有「草稿」这个东西，草稿是这一下按出来的。所以
	// 版本对不上不是「两个人对着同一份报告各改各的」，是同一个人手快连按了两下，
	// 后一下读到了前一下写进去之前的版本。这种情况重读一次再写就对了；直接报错
	// 的话，人看到的是「记一笔失败」，而他什么也没做错。
	return retryContendedWrite(func() (InsightReport, error) {
		draft, err := s.Repository.FindDraftByWindow(ctx, actor.OrganizationID, projectID, windowStart, windowEnd)
		if errors.Is(err, ErrNotFound) {
			return s.createDraftWithFinding(ctx, actor, projectID, windowStart, windowEnd, finding, now)
		}
		if err != nil {
			return InsightReport{}, err
		}
		return s.Repository.UpdateReportDigest(ctx, actor.OrganizationID, projectID, draft.ID,
			draft.Version, mergePinnedFinding(draft.Digest, finding), now)
	})
}

// pinAttempts 是记一笔最多读-改-写几次。三次足够盖住一个人连按几下按钮；
// 再多就不是手快了，是写路径本身坏了，那时候报错比挂住更好查。
const pinAttempts = 3

// errDraftRaced 说的是「这个窗口的草稿刚被另一次记一笔建走了」。它不往外抛，
// 只用来告诉重试再读一次——外面看到它没有意义，人按的是记一笔，不是建报告。
var errDraftRaced = errors.New("insights: 这个窗口的草稿被另一次记一笔抢先建了")

func retryContendedWrite(attempt func() (InsightReport, error)) (InsightReport, error) {
	var err error
	for i := 0; i < pinAttempts; i++ {
		var value InsightReport
		value, err = attempt()
		if err == nil {
			return value, nil
		}
		if !errors.Is(err, ErrVersionConflict) && !errors.Is(err, errDraftRaced) {
			return value, err
		}
	}
	// 试满了还在抢。这时候只能让人再按一次，但得报一个他看得懂、前端也认识的错——
	// errDraftRaced 是内部约定，漏出去会变成一个 500。
	return InsightReport{}, ErrVersionConflict
}

// mergePinnedFinding 把这一笔并进草稿已有的发现里。
//
// 同一条记两次是常见的误操作（换了个视图又看到同一个结论）。覆盖而不是追加：
// 人第二次记的时候补的那句话，应该是他现在想说的那句。覆盖也是原地覆盖，
// 复盘页上那几行的先后是人自己记出来的，不该因为补了一句话就跳到最后一行。
//
// 按 pinKey 比而不是 dedupeKey：趋势和疲劳没有变量，拿 dedupeKey 比的话
// A 版和 B 版的趋势会被当成同一条，记完 A 版再记 B 版，A 版无声消失，
// 而两个按钮上都写着「已记一笔」。
func mergePinnedFinding(digest []ReportFinding, finding ReportFinding) []ReportFinding {
	merged := make([]ReportFinding, 0, len(digest)+1)
	replaced := false
	for _, existing := range digest {
		if existing.Origin == OriginPinned && existing.pinKey() != "" &&
			existing.pinKey() == finding.pinKey() {
			merged, replaced = append(merged, finding), true
			continue
		}
		merged = append(merged, existing)
	}
	if !replaced {
		merged = append(merged, finding)
	}
	return merged
}

// createDraftWithFinding 建一份只有这一条发现的空草稿。
//
// 它不走 CreateReport：CreateReport 必须挂一次投放执行，而记一笔发生在人还在看
// 数据的时候，那时候还没到「这份复盘算哪次投放」这个问题。执行 ID 在复盘页提交
// 之前补上。
func (s Service) createDraftWithFinding(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, windowStart, windowEnd string,
	finding ReportFinding, now time.Time) (InsightReport, error) {
	id, err := s.idGenerator()("insightreport")
	if err != nil {
		return InsightReport{}, err
	}
	value, err := s.Repository.CreateReport(ctx, InsightReport{
		ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID,
		Status: ReportDraft, Digest: []ReportFinding{finding},
		WindowStart: windowStart, WindowEnd: windowEnd,
		Findings:  []string{},
		Version:   1,
		CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	})
	if errors.Is(err, ErrInvalidState) {
		// (项目 + 窗口) 上有唯一键，撞上它说明这个窗口的草稿刚被建走了。
		// 记一笔这条路上只有一种可能：同一个人连按了两下，前一下先落地。
		// 退回去重读一次，把这一笔追加进那份草稿，而不是把「已经定格过一份报告」
		// 甩到人脸上——他按的是记一笔，压根没打算建报告。
		return InsightReport{}, errDraftRaced
	}
	return value, err
}

// concludedExperimentsInWindow 取观察窗口和报告窗口有重叠的、已结论的实验。
//
// 用重叠而不是包含：实验的观察窗口是建实验时定的，报告的窗口是人在投后分析页上当时
// 选的，两者本来就不会正好对齐。要求包含的话，绝大多数实验都会因为差几天而被排除，
// 报告里那一块就永远写着「本轮没有实验」。
func (s Service) concludedExperimentsInWindow(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, window MetricWindow) ([]Experiment, error) {
	values, err := s.ListExperiments(ctx, actor, projectID,
		ExperimentFilter{Status: ExperimentConcluded, Limit: 100})
	if err != nil {
		return nil, err
	}
	matched := make([]Experiment, 0, len(values))
	for _, experiment := range values {
		if experiment.WindowStart.After(window.End) || experiment.WindowEnd.Before(window.Start) {
			continue
		}
		matched = append(matched, experiment)
	}
	return matched, nil
}

// DropReportFinding 把一条发现标记为已删除，或者把删掉的那条恢复回来。
//
// **不物理删除**——留着才知道系统带了什么、人不要什么，这是评估自动带入好不好用的
// 唯一依据。只有 draft 状态的报告能改：确认过的报告是要被引用、被追溯的，改一条
// 等于让引用它的人手上的那份变成假的。
func (s Service) DropReportFinding(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, reportID string, expectedVersion int64, index int, dropped bool) (InsightReport, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return InsightReport{}, err
	}
	report, err := s.Repository.GetReport(ctx, actor.OrganizationID, projectID, reportID)
	if err != nil {
		return InsightReport{}, err
	}
	if report.Status != ReportDraft {
		return InsightReport{}, ErrInvalidState
	}
	if report.Version != expectedVersion {
		return InsightReport{}, ErrVersionConflict
	}
	if index < 0 || index >= len(report.Digest) {
		return InsightReport{}, ErrInvalidRequest
	}
	digest := make([]ReportFinding, len(report.Digest))
	copy(digest, report.Digest)
	digest[index].Dropped = dropped
	return s.Repository.UpdateReportDigest(ctx, actor.OrganizationID, projectID, reportID, expectedVersion, digest, s.now())
}

func (s Service) ListReports(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, limit int) ([]InsightReport, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	values, err := s.Repository.ListReports(ctx, actor.OrganizationID, projectID, normalizeLimit(limit))
	if err != nil {
		return nil, err
	}
	// 空草稿不列出来。它是「记一笔」自动建了但人什么都没记的残留，
	// 列出来只会让真正有内容的那几份混在一堆空壳里。
	visible := make([]InsightReport, 0, len(values))
	for _, value := range values {
		if hasContent(value) {
			visible = append(visible, value)
		}
	}
	return visible, nil
}

// emptyDraftRetention 是空草稿保留多久。30 天：一个投放周期通常一个月内结束，
// 超过一个月还没记过任何东西的草稿，人已经不会回来了。
const emptyDraftRetention = 30 * 24 * time.Hour

// PurgeEmptyDrafts 由维护命令调用，不走 actor 鉴权——它删的是没有内容的残留，
// 不属于任何人的数据。
func (s Service) PurgeEmptyDrafts(ctx context.Context) (int64, error) {
	return s.Repository.PurgeEmptyDrafts(ctx, s.now().Add(-emptyDraftRetention))
}

func (s Service) ConfirmReport(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, reportID string, expectedVersion int64) (InsightReport, error) {
	if err := s.ready(actor, projectID, ScopeConfirm); err != nil {
		return InsightReport{}, err
	}
	value, err := s.Repository.GetReport(ctx, actor.OrganizationID, projectID, reportID)
	if err != nil {
		return InsightReport{}, err
	}
	if value.Status != ReportDraft {
		return InsightReport{}, ErrInvalidState
	}
	return s.Repository.ConfirmReport(ctx, actor.OrganizationID, projectID, reportID, expectedVersion, actor.Principal.ID, s.now())
}

// SubmitReview 提交复盘：补执行、定格系统发现、确认，一次写完。
func (s Service) SubmitReview(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, reportID string, request SubmitReviewRequest) (InsightReport, error) {
	if err := s.ready(actor, projectID, ScopeConfirm); err != nil {
		return InsightReport{}, err
	}
	if err := request.Validate(); err != nil {
		return InsightReport{}, err
	}
	report, err := s.Repository.GetReport(ctx, actor.OrganizationID, projectID, reportID)
	if err != nil {
		return InsightReport{}, err
	}
	if err := checkSubmittable(report, request.ExpectedVersion); err != nil {
		return InsightReport{}, err
	}

	window, ok := reportMetricWindow(report)
	if !ok {
		// 窗口解析不出来就没法算系统发现。宁可只带人记的那几笔提交，
		// 也不能拿一个猜出来的窗口去算——算出来的数字看起来一样可信。
		return s.Repository.SubmitReport(ctx, actor.OrganizationID, projectID, reportID,
			request.ExpectedVersion, request.ExecutionID, report.Digest, actor.Principal.ID, s.now())
	}

	analysis, err := s.GetPerformanceAnalysis(ctx, actor, projectID, window)
	if err != nil {
		return InsightReport{}, err
	}
	experiments, err := s.concludedExperimentsInWindow(ctx, actor, projectID, window)
	if err != nil {
		return InsightReport{}, err
	}
	experiences, err := s.ListExperiences(ctx, actor, projectID, ExperienceConfirmed, 50)
	if err != nil {
		return InsightReport{}, err
	}

	// 人记的那几笔在前，系统补的去重后跟在后面。
	pinned := make([]ReportFinding, 0, len(report.Digest))
	for _, finding := range report.Digest {
		if finding.Origin == OriginPinned {
			pinned = append(pinned, finding)
		}
	}
	digest := mergeFindings(pinned, buildReportDigest(analysis, experiments, experiences))

	return s.Repository.SubmitReport(ctx, actor.OrganizationID, projectID, reportID,
		request.ExpectedVersion, request.ExecutionID, digest, actor.Principal.ID, s.now())
}

// reportMetricWindow 把报告定格的日期串解析回窗口。
func reportMetricWindow(report InsightReport) (MetricWindow, bool) {
	start, end := reportWindow(report)
	if start == nil || end == nil {
		return MetricWindow{}, false
	}
	return MetricWindow{Start: *start, End: *end}, true
}

func (s Service) CreateExperience(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, reportID string, expectedReportVersion int64, request CreateExperienceRequest) (Experience, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return Experience{}, err
	}
	if err := request.Validate(); err != nil {
		return Experience{}, err
	}
	report, err := s.Repository.GetReport(ctx, actor.OrganizationID, projectID, reportID)
	if err != nil {
		return Experience{}, err
	}
	if report.Version != expectedReportVersion {
		return Experience{}, ErrVersionConflict
	}
	if report.Status != ReportConfirmed {
		return Experience{}, ErrInvalidState
	}
	id, err := s.idGenerator()("experience")
	if err != nil {
		return Experience{}, err
	}
	auditID, err := s.idGenerator()("experienceaudit")
	if err != nil {
		return Experience{}, err
	}
	now := s.now()
	card := request.withCardDefaults()
	// 统计窗口跟着报告走，不让人手抄。报告已经定格了「这个判断基于哪一段数据」，
	// 再让人在经验里填一遍，只会填错或者干脆不填——录入表单也确实没给这一格。
	// 结果就是每条经验的窗口永远是空的，看到经验的人无从判断它是哪一段时间的事。
	basis := card.DataBasis
	if basis.WindowStart == nil && basis.WindowEnd == nil {
		basis.WindowStart, basis.WindowEnd = reportWindow(report)
	}
	// A newly deposited conclusion starts as 待确认. Only an explicit confirm
	// makes it quotable downstream.
	value := Experience{
		ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID,
		LineageID: id, Revision: 1, ReportID: report.ID,
		SourceExecutionID: report.ExecutionID, SourceEvidenceID: report.EvidenceID,
		SourceMetricSnapshotID: report.MetricSnapshotID,
		Conclusion:             strings.TrimSpace(request.Conclusion), Conditions: append([]string{}, request.Conditions...),
		Counterexamples: append([]string{}, request.Counterexamples...), Status: ExperiencePending,
		CardType: card.CardType, Judgement: judge(card.Confidence, ""),
		RecommendedAction: strings.TrimSpace(card.RecommendedAction),
		Applicability:     card.Applicability, DataBasis: basis, ContentBasis: card.ContentBasis,
		StatusChangedBy: actor.Principal.ID, StatusChangedAt: &now,
		Version: 1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	}
	return s.Repository.CreateExperience(ctx, value, ExperienceAudit{
		ID: auditID, OrganizationID: actor.OrganizationID, ProjectID: projectID, ExperienceID: id,
		FromStatus: "", ToStatus: ExperiencePending, Reason: "从已确认复盘报告沉淀，等待人工确认。",
		ActorID: actor.Principal.ID, CreatedAt: now,
	})
}

// reportWindow 把报告定格的窗口解析回时间。报告里存的是日期串，早期报告根本没有
// 窗口；解析不出来就返回空，宁可这一格空着，也不能凭空填一个日期上去——经验被引用时
// 没人会回头核对它。
func reportWindow(report InsightReport) (*time.Time, *time.Time) {
	start, err := time.Parse("2006-01-02", report.WindowStart)
	if err != nil {
		return nil, nil
	}
	end, err := time.Parse("2006-01-02", report.WindowEnd)
	if err != nil {
		return nil, nil
	}
	return &start, &end
}

// ConfirmExperience 把待定的经验变成在用，也用来「重新看过了，还成立」。
// 确认一条修订版会在同一个事务里把它取代的那一版停用。
func (s Service) ConfirmExperience(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, experienceID string, expectedVersion int64) (Experience, error) {
	if err := s.ready(actor, projectID, ScopeConfirm); err != nil {
		return Experience{}, err
	}
	current, err := s.Repository.GetExperience(ctx, actor.OrganizationID, projectID, experienceID)
	if err != nil {
		return Experience{}, err
	}
	// 待定的确认，是「这条经验成立」；在用且标了复审的确认，是「重新看过了，还成立」。
	// 后者要顺手把标记摘掉，否则它会一直挂着，下次没人知道到底看过没有。
	if current.Status != ExperiencePending && !(current.Status == ExperienceConfirmed && current.NeedsReview) {
		return Experience{}, ErrInvalidState
	}
	auditID, err := s.idGenerator()("experienceaudit")
	if err != nil {
		return Experience{}, err
	}
	supersedeAuditID, err := s.idGenerator()("experienceaudit")
	if err != nil {
		return Experience{}, err
	}
	return s.Repository.ConfirmExperience(ctx, ConfirmExperienceInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, ID: experienceID,
		ExpectedVersion: expectedVersion, ActorID: actor.Principal.ID, Now: s.now(),
		AuditID: auditID, SupersedeAuditID: supersedeAuditID,
	})
}

// RejectExperience discards a 待确认 conclusion without deleting the row.
func (s Service) RejectExperience(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, experienceID string, request ExperienceTransitionRequest) (Experience, error) {
	return s.transition(ctx, actor, projectID, experienceID, ScopeConfirm,
		[]ExperienceStatus{ExperiencePending}, ExperienceRetired, request, true)
}

// RequestExperienceReview 给在用的经验打上「该看一眼了」。
//
// **它不改状态**——这条经验还在用、还能被引用，标记只是提醒有人该重新看它的依据。
// 这正是把「待复审」从状态改成标记的用处：新数据和老结论冲突时，提醒得发出去，
// 但不能顺手把一条正在被引用的经验从所有页面上撤掉。
func (s Service) RequestExperienceReview(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, experienceID string, request ExperienceTransitionRequest) (Experience, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return Experience{}, err
	}
	if err := request.validate(true); err != nil {
		return Experience{}, err
	}
	auditID, err := s.idGenerator()("experienceaudit")
	if err != nil {
		return Experience{}, err
	}
	return s.Repository.FlagExperienceForReview(ctx, FlagExperienceReviewInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, ID: experienceID,
		ExpectedVersion: request.ExpectedVersion, NeedsReview: true,
		Reason: strings.TrimSpace(request.Reason), ActorID: actor.Principal.ID,
		Now: s.now(), AuditID: auditID,
	})
}

// RetireExperience is the logical delete in PRD §11.2: the row stays readable
// and its reference history stays auditable.
//
// from 里只有 confirmed：标了复审的经验状态本来就是 confirmed，已经被覆盖。
func (s Service) RetireExperience(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, experienceID string, request ExperienceTransitionRequest) (Experience, error) {
	return s.transition(ctx, actor, projectID, experienceID, ScopeConfirm,
		[]ExperienceStatus{ExperienceConfirmed}, ExperienceRetired, request, true)
}

func (s Service) transition(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, experienceID string, scope contract.Scope, from []ExperienceStatus, to ExperienceStatus, request ExperienceTransitionRequest, reasonRequired bool) (Experience, error) {
	if err := s.ready(actor, projectID, scope); err != nil {
		return Experience{}, err
	}
	if err := request.validate(reasonRequired); err != nil {
		return Experience{}, err
	}
	auditID, err := s.idGenerator()("experienceaudit")
	if err != nil {
		return Experience{}, err
	}
	return s.Repository.TransitionExperience(ctx, TransitionExperienceInput{
		OrganizationID: actor.OrganizationID, ProjectID: projectID, ID: experienceID,
		ExpectedVersion: request.ExpectedVersion, From: from, To: to,
		Reason: strings.TrimSpace(request.Reason), ActorID: actor.Principal.ID,
		Now: s.now(), AuditID: auditID,
	})
}

// ReviseExperience appends a new revision to the lineage rather than editing
// the confirmed conclusion in place.
func (s Service) ReviseExperience(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, experienceID string, request ReviseExperienceRequest) (Experience, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return Experience{}, err
	}
	if err := request.validate(); err != nil {
		return Experience{}, err
	}
	source, err := s.Repository.GetExperience(ctx, actor.OrganizationID, projectID, experienceID)
	if err != nil {
		return Experience{}, err
	}
	if source.Version != request.ExpectedVersion {
		return Experience{}, ErrVersionConflict
	}
	if source.Status == ExperienceRetired || source.SupersededByID != "" {
		return Experience{}, ErrInvalidState
	}
	lineage, err := s.Repository.ListExperienceLineage(ctx, actor.OrganizationID, projectID, source.LineageID)
	if err != nil {
		return Experience{}, err
	}
	next := source.Revision
	for _, item := range lineage {
		if item.Revision > next {
			next = item.Revision
		}
		// A lineage may hold only one open revision at a time.
		if item.Status == ExperiencePending && item.ID != source.ID {
			return Experience{}, ErrInvalidState
		}
	}
	id, err := s.idGenerator()("experience")
	if err != nil {
		return Experience{}, err
	}
	auditID, err := s.idGenerator()("experienceaudit")
	if err != nil {
		return Experience{}, err
	}
	now := s.now()
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = fmt.Sprintf("修订自 %s 第 %d 版。", source.ID, source.Revision)
	}
	card := request.card()
	// 修订改的是结论的说法，不是它的数据来源——报告、执行、快照都照抄前一版，
	// 统计窗口也一样。这一格修订表单里没有，不接着传就会在修订这一步丢掉。
	basis := card.DataBasis
	if basis.WindowStart == nil && basis.WindowEnd == nil {
		basis.WindowStart, basis.WindowEnd = source.DataBasis.WindowStart, source.DataBasis.WindowEnd
	}
	value := Experience{
		ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID,
		LineageID: source.LineageID, Revision: next + 1, SupersedesID: source.ID,
		ReportID:          source.ReportID,
		SourceExecutionID: source.SourceExecutionID, SourceEvidenceID: source.SourceEvidenceID,
		SourceMetricSnapshotID: source.SourceMetricSnapshotID,
		Conclusion:             strings.TrimSpace(request.Conclusion), Conditions: append([]string{}, request.Conditions...),
		Counterexamples: append([]string{}, request.Counterexamples...), Status: ExperiencePending,
		CardType: card.CardType, Judgement: judge(card.Confidence, ""),
		RecommendedAction: strings.TrimSpace(card.RecommendedAction),
		Applicability:     card.Applicability, DataBasis: basis, ContentBasis: card.ContentBasis,
		StatusReason: reason, StatusChangedBy: actor.Principal.ID, StatusChangedAt: &now,
		Version: 1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	}
	input := ReviseExperienceInput{Value: value, Audit: ExperienceAudit{
		ID: auditID, OrganizationID: actor.OrganizationID, ProjectID: projectID, ExperienceID: id,
		FromStatus: "", ToStatus: ExperiencePending, Reason: reason,
		ActorID: actor.Principal.ID, CreatedAt: now,
	}}
	// 修订一条还没确认的经验时，前身必须跟着退休。留着它，待确认队列里就并排
	// 躺着同一条经验的两版，看不出该确认哪一个；确认了旧的那一版，刚补的内容
	// 就白补了。前身从没被确认过，也就没人引用过它，退休不会拿掉任何在用的东西。
	if source.Status == ExperiencePending {
		retireAuditID, idErr := s.idGenerator()("experienceaudit")
		if idErr != nil {
			return Experience{}, idErr
		}
		input.RetireSource = &TransitionExperienceInput{
			OrganizationID: actor.OrganizationID, ProjectID: projectID, ID: source.ID,
			ExpectedVersion: source.Version,
			From:            []ExperienceStatus{ExperiencePending},
			To:              ExperienceRetired,
			Reason:          fmt.Sprintf("已被第 %d 版取代。", value.Revision),
			ActorID:         actor.Principal.ID, Now: now, AuditID: retireAuditID,
		}
	}
	return s.Repository.ReviseExperience(ctx, input)
}

// RecordExperienceReference closes the AM-014 loop: a downstream consumer says
// whether it adopted, modified or rejected the quoted conclusion.
func (s Service) RecordExperienceReference(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, experienceID string, request RecordExperienceReferenceRequest) (ExperienceReference, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return ExperienceReference{}, err
	}
	if err := request.validate(); err != nil {
		return ExperienceReference{}, err
	}
	experience, err := s.Repository.GetExperience(ctx, actor.OrganizationID, projectID, experienceID)
	if err != nil {
		return ExperienceReference{}, err
	}
	// 这里是人点名引用后回填结果，闸只有一道：这条经验得在用。
	// 卡 Reusable 会让「有人用了一条 👁 的经验」这件事记不下来——事情发生了，
	// 拒绝记录不会让它没发生，只会让引用记录少一条。
	if !experience.Quotable() {
		return ExperienceReference{}, ErrInvalidState
	}
	id, err := s.idGenerator()("experienceref")
	if err != nil {
		return ExperienceReference{}, err
	}
	now := s.now()
	return s.Repository.CreateExperienceReference(ctx, ExperienceReference{
		ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID, ExperienceID: experience.ID,
		ConsumerKind: strings.TrimSpace(request.ConsumerKind), ConsumerID: strings.TrimSpace(request.ConsumerID),
		Outcome: request.Outcome, Note: strings.TrimSpace(request.Note),
		Version: 1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	})
}

func (s Service) ListExperienceReferences(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, experienceID string, limit int) ([]ExperienceReference, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	if _, err := s.Repository.GetExperience(ctx, actor.OrganizationID, projectID, experienceID); err != nil {
		return nil, err
	}
	return s.Repository.ListExperienceReferences(ctx, actor.OrganizationID, projectID, experienceID, normalizeLimit(limit))
}

// ListProjectExperienceReferences answers "which of our experiences were used
// downstream, and were they adopted" without walking every experience in turn.
func (s Service) ListProjectExperienceReferences(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, limit int) ([]ExperienceReference, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	return s.Repository.ListExperienceReferences(ctx, actor.OrganizationID, projectID, "", normalizeLimit(limit))
}

func (s Service) ListExperienceAudits(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, experienceID string, limit int) ([]ExperienceAudit, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	if _, err := s.Repository.GetExperience(ctx, actor.OrganizationID, projectID, experienceID); err != nil {
		return nil, err
	}
	return s.Repository.ListExperienceAudits(ctx, actor.OrganizationID, projectID, experienceID, normalizeLimit(limit))
}

func (s Service) ListExperienceLineage(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, experienceID string) ([]Experience, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	value, err := s.Repository.GetExperience(ctx, actor.OrganizationID, projectID, experienceID)
	if err != nil {
		return nil, err
	}
	return s.Repository.ListExperienceLineage(ctx, actor.OrganizationID, projectID, value.LineageID)
}

func summarizeSimulatedMetrics(snapshot DeliveryMetricSnapshot) (string, []string) {
	metrics := snapshot.RawMetrics
	ctr := ratioPercent(metrics.Clicks, metrics.Impressions)
	cvr := ratioPercent(metrics.Conversions, metrics.Clicks)
	summary := fmt.Sprintf(
		"基于本地模拟投放数据集 %s 生成复盘：曝光 %d、点击 %d、转化 %d、模拟花费 %.2f %s。该结果仅用于验证产品闭环，不代表真实广告平台效果。",
		snapshot.DatasetVersion, metrics.Impressions, metrics.Clicks, metrics.Conversions,
		float64(metrics.SpendCents)/100, snapshot.Currency,
	)
	return summary, []string{
		fmt.Sprintf("模拟点击率 CTR 为 %.2f%%（%d/%d）。", ctr, metrics.Clicks, metrics.Impressions),
		fmt.Sprintf("模拟点击转化率 CVR 为 %.2f%%（%d/%d）。", cvr, metrics.Conversions, metrics.Clicks),
		"所有指标来源均为 demo_fixture 模拟数据，不得用于判断真实广告效果或对外宣称。",
	}
}

func ratioPercent(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}

// ListExperiences returns every lifecycle state when status is empty, so the
// experience library can show 待定 / 在用 / 停用 side by side.
// 「该看一眼了」不是这里的一个取值——它是在用经验上的标记，按 NeedsReview 筛。
func (s Service) ListExperiences(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, status ExperienceStatus, limit int) ([]Experience, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	if status != "" && !status.valid() {
		return nil, ErrInvalidRequest
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	return s.Repository.ListExperiences(ctx, actor.OrganizationID, projectID, status, normalizeLimit(limit))
}

// GetPreLaunch 在 prelaunch.go。

func (s Service) GetPerformance(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID) (PerformanceOverview, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return PerformanceOverview{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return PerformanceOverview{}, err
	}
	values, err := s.Delivery.ListExecutions(ctx, actor, projectID, 100)
	if err != nil {
		return PerformanceOverview{}, err
	}
	return PerformanceOverview{
		ProjectID: projectID, Executions: values,
		Disclosure: "当前为本地模拟执行证据，不代表真实广告平台效果数据。",
	}, nil
}

func (s Service) ready(actor contract.ActorContext, projectID contract.ProjectID, scope contract.Scope) error {
	if s.Repository == nil || s.Projects == nil || s.Delivery == nil {
		return fmt.Errorf("insights dependencies are incomplete")
	}
	if actor.OrganizationID == "" || projectID == "" || !actor.HasScope(scope) {
		return fmt.Errorf("%s scope is required", scope)
	}
	return nil
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Service) idGenerator() ids.Generator {
	if s.NewID != nil {
		return s.NewID
	}
	return ids.New
}

func normalizeLimit(value int) int {
	if value < 1 || value > 100 {
		return 50
	}
	return value
}
