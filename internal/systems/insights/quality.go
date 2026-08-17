package insights

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 数据质量（PRD §一级导航：阻止错误数据产生强结论；doc10 §10 `GET /api/insights/v1/data-quality`）。
//
// 设计上最要紧的一条：**问题不入库**。
//
// 一个质量问题就是「按现有数据算出来的一个事实」——某个数据源三天没数据了、
// 某天的点击数超过了曝光数、有六个平台对象的花费没归属。这些每次都能重算，
// 而且必须重算：如果把问题也存一份，就会出现「库里说有问题、数据里已经好了」，
// 而这恰恰是数据质量模块最不该犯的错。
//
// 库里只存**人对某条问题做过什么处置**（见 migrations 里的 insight_quality_dispositions），
// 靠 fingerprint 与实时算出的问题对上。fingerprint 不含数据窗口——含了的话
// 换一次窗口指纹就变，处置永远跟不住同一个问题。
//
// 三种处置的语义差别很大，不要混：
//
//	acknowledged 我知道了，有人在跟。问题继续留在队列里，只是标了责任人。
//	ignored      判定这条不用管（比如测试账户的数据缺口）。从队列里下去，再次检出也不弹回来。
//	resolved     已经修好了。**下次扫描如果还检出，自动弹回 reopened**——
//	             报了修但问题还在，这件事必须被看见，否则点一次「已修复」就能让问题永久消失。

type QualityIssueKind string

const (
	// QualityFreshness 数据够不够新：doc10 §11「任何洞察必须展示数据截止时间」。
	QualityFreshness QualityIssueKind = "freshness"
	// QualityMissing 该有的数据没有：日期不连续、导入被拒行（doc10 §7 日期连续性）。
	QualityMissing QualityIssueKind = "missing"
	// QualityAnomaly 数据本身不对劲：质量状态非 healthy，或出现物理上不可能的值。
	QualityAnomaly QualityIssueKind = "anomaly"
	// QualityCaliber 口径不一致：两个数字并排之前必须一致的东西不一致了（doc10 §6）。
	QualityCaliber QualityIssueKind = "caliber"
	// QualityReconciliation 对不上账：花费归不到素材，映射率不足（doc10 §7、§12.6）。
	QualityReconciliation QualityIssueKind = "reconciliation"
)

func (k QualityIssueKind) valid() bool {
	switch k {
	case QualityFreshness, QualityMissing, QualityAnomaly, QualityCaliber, QualityReconciliation:
		return true
	}
	return false
}

func (k QualityIssueKind) Label() string {
	switch k {
	case QualityFreshness:
		return "新鲜度"
	case QualityMissing:
		return "缺失"
	case QualityAnomaly:
		return "异常"
	case QualityCaliber:
		return "口径"
	case QualityReconciliation:
		return "对账"
	}
	return string(k)
}

type QualitySeverity string

const (
	// SeverityBlocking 会让整个 Project 暂停强结论（PRD §10.3、doc10 §12.4）。
	SeverityBlocking QualitySeverity = "blocking"
	// SeverityWarning 结论仍可给出，但必须带着这条一起看。
	SeverityWarning QualitySeverity = "warning"
	// SeverityInfo 只是记一笔，不影响怎么读数字。
	SeverityInfo QualitySeverity = "info"
)

func (s QualitySeverity) Label() string {
	switch s {
	case SeverityBlocking:
		return "阻断"
	case SeverityWarning:
		return "警告"
	case SeverityInfo:
		return "提示"
	}
	return string(s)
}

func (s QualitySeverity) rank() int {
	switch s {
	case SeverityBlocking:
		return 0
	case SeverityWarning:
		return 1
	}
	return 2
}

// QualityIssueState 是问题**当前**的处置状态。open 与 reopened 不入库：
// 前者是「还没人碰过」，后者是「有人报了修但问题还在」，两者都由比对算出。
type QualityIssueState string

const (
	QualityOpen         QualityIssueState = "open"
	QualityAcknowledged QualityIssueState = "acknowledged"
	QualityResolved     QualityIssueState = "resolved"
	QualityIgnored      QualityIssueState = "ignored"
	QualityReopened     QualityIssueState = "reopened"
)

func (s QualityIssueState) Label() string {
	switch s {
	case QualityOpen:
		return "待处理"
	case QualityAcknowledged:
		return "跟进中"
	case QualityResolved:
		return "已修复"
	case QualityIgnored:
		return "已忽略"
	case QualityReopened:
		return "报修后又出现"
	}
	return string(s)
}

// InQueue 决定这条问题还要不要占着修复队列。已忽略的下队列，已修复且没再出现的也下队列。
func (s QualityIssueState) InQueue() bool {
	return s == QualityOpen || s == QualityAcknowledged || s == QualityReopened
}

// QualityDispositionState 是能被写进库的三种处置，是 QualityIssueState 的子集。
type QualityDispositionState string

const (
	DispositionAcknowledged QualityDispositionState = "acknowledged"
	DispositionResolved     QualityDispositionState = "resolved"
	DispositionIgnored      QualityDispositionState = "ignored"
)

func (s QualityDispositionState) valid() bool {
	switch s {
	case DispositionAcknowledged, DispositionResolved, DispositionIgnored:
		return true
	}
	return false
}

// QualityDisposition 是库里那一行：谁、什么时候、把哪条问题标成了什么、依据是什么。
type QualityDisposition struct {
	ID             string                  `json:"id"`
	OrganizationID contract.OrganizationID `json:"organization_id"`
	ProjectID      contract.ProjectID      `json:"project_id"`

	Fingerprint string                  `json:"fingerprint"`
	IssueKind   QualityIssueKind        `json:"issue_kind"`
	State       QualityDispositionState `json:"state"`
	Note        string                  `json:"note"`

	// ObservedThrough 是处置发生时该问题的最新观测时间。检测器算出更晚的观测时间
	// 就说明处置之后问题又发生了。
	ObservedThrough time.Time `json:"observed_through"`

	Version   int64     `json:"version"`
	DecidedBy string    `json:"decided_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// QualityIssue 是检测器算出来的一条问题，叠加了处置状态之后返回给前端。
type QualityIssue struct {
	// Fingerprint 在同一个问题反复出现时保持不变，处置记录才跟得住。
	Fingerprint string           `json:"fingerprint"`
	Kind        QualityIssueKind `json:"kind"`
	Severity    QualitySeverity  `json:"severity"`

	// Title 一句话说清出了什么事；Detail 补充可核对的具体数字。
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Suggestion 落 PRD §10.3「先建议检查追踪、匹配和同步」——不要只报警不给下一步。
	Suggestion string `json:"suggestion"`

	// 影响范围（22 §6.2）。
	ScopeLabel         string    `json:"scope_label"`
	DataSourceID       string    `json:"data_source_id,omitempty"`
	Platform           Platform  `json:"platform,omitempty"`
	AffectedSpendCents int64     `json:"affected_spend_cents"`
	AffectedDays       int       `json:"affected_days,omitempty"`
	StatDate           string    `json:"stat_date,omitempty"`
	LastObservedAt     time.Time `json:"last_observed_at"`

	State       QualityIssueState   `json:"state"`
	Disposition *QualityDisposition `json:"disposition,omitempty"`
}

// QualityReport 是 GET data-quality 的返回。20 §4.1：问题队列为主，
// 影响范围和修复状态为辅，趋势只给影响判断所需的量。
type QualityReport struct {
	Window      MetricWindow `json:"window"`
	GeneratedAt time.Time    `json:"generated_at"`

	Issues []QualityIssue `json:"issues"`

	// ByKind 是六个二级视图的角标数字，只数还在队列里的。
	ByKind     map[QualityIssueKind]int `json:"by_kind"`
	OpenCount  int                      `json:"open_count"`
	QueueCount int                      `json:"queue_count"`

	// StrongConclusionsAllowed 为 false 时，投前/投后都不应给出强结论或自动优化动作
	// （PRD §10.3、doc10 §12.4）。
	StrongConclusionsAllowed bool   `json:"strong_conclusions_allowed"`
	BlockedReason            string `json:"blocked_reason,omitempty"`

	// Sources 是新鲜度视图的底表：每个数据源截止到哪天、滞后多少。
	Sources []SourceHealth `json:"sources"`
}

type ResolveQualityIssueRequest struct {
	Fingerprint string                  `json:"fingerprint"`
	IssueKind   QualityIssueKind        `json:"issue_kind"`
	State       QualityDispositionState `json:"state"`
	Note        string                  `json:"note"`
	// ObservedThrough 由前端回传它当时看到的那条问题的 last_observed_at。
	// 由客户端回传而不是服务端取 now()，是为了让「你处置的是你看到的那个版本」
	// 这件事成立——中间如果问题又恶化了，处置不会连带把新情况也盖掉。
	ObservedThrough time.Time `json:"observed_through"`
}

func (r ResolveQualityIssueRequest) validate() error {
	if strings.TrimSpace(r.Fingerprint) == "" {
		return fmt.Errorf("%w: 缺少问题标识", ErrInvalidRequest)
	}
	if !r.IssueKind.valid() {
		return fmt.Errorf("%w: 问题类型无效", ErrInvalidRequest)
	}
	if !r.State.valid() {
		return fmt.Errorf("%w: 处置状态只能是跟进中、已修复或已忽略", ErrInvalidRequest)
	}
	if strings.TrimSpace(r.Note) == "" {
		return fmt.Errorf("%w: 处置必须写明依据", ErrInvalidRequest)
	}
	if len(r.Note) > 1000 {
		return fmt.Errorf("%w: 处置说明过长", ErrInvalidRequest)
	}
	if r.ObservedThrough.IsZero() {
		return fmt.Errorf("%w: 缺少问题的观测时间", ErrInvalidRequest)
	}
	return nil
}

// --- 检测阈值 ---

const (
	// 滞后超过这么多天就算警告，再翻一倍算阻断。doc10 没有给具体天数，
	// 这里取的是 freshnessDelayedAfterDays 的同一套口径，改动会同时影响投后分析的提示。
	freshnessBlockingAfterDays = freshnessDelayedAfterDays * 2
	// 映射率低于此值时，素材级结论已经不成立了（doc10 §5）。
	mappingRateBlockingBelow = 0.80
	mappingRateWarningBelow  = 0.95
)

// --- 服务 ---

// GetDataQuality 落 doc10 §10 的 `GET /api/insights/v1/data-quality`。
func (s Service) GetDataQuality(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, window MetricWindow) (QualityReport, error) {
	if err := s.connectorsReady(actor, projectID, ScopeRead); err != nil {
		return QualityReport{}, err
	}
	if window.End.Before(window.Start) {
		return QualityReport{}, fmt.Errorf("%w: 数据窗口的结束日期早于开始日期", ErrInvalidRequest)
	}
	if window.Days() > maxWindowDays {
		return QualityReport{}, fmt.Errorf("%w: 数据窗口最长 %d 天", ErrInvalidRequest, maxWindowDays)
	}
	sources, err := s.Connectors.ListDataSources(ctx, actor.OrganizationID, projectID, DataSourceFilter{Limit: 200})
	if err != nil {
		return QualityReport{}, err
	}
	facts, err := s.Connectors.ListMetricFacts(ctx, actor.OrganizationID, projectID, window)
	if err != nil {
		return QualityReport{}, err
	}
	batches, err := s.Connectors.ListImportBatches(ctx, actor.OrganizationID, projectID, ImportBatchFilter{Limit: 200})
	if err != nil {
		return QualityReport{}, err
	}
	dispositions, err := s.Connectors.ListQualityDispositions(ctx, actor.OrganizationID, projectID)
	if err != nil {
		return QualityReport{}, err
	}
	return buildQualityReport(window, sources, facts, batches, dispositions, s.now()), nil
}

// ResolveQualityIssue 记下一次处置。问题本身不入库，这里写的只是「人做了什么」。
func (s Service) ResolveQualityIssue(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, request ResolveQualityIssueRequest) (QualityDisposition, error) {
	if err := s.connectorsReady(actor, projectID, ScopeWrite); err != nil {
		return QualityDisposition{}, err
	}
	if err := request.validate(); err != nil {
		return QualityDisposition{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return QualityDisposition{}, err
	}
	id, err := s.idGenerator()("qualitydisposition")
	if err != nil {
		return QualityDisposition{}, err
	}
	now := s.now()
	record := QualityDisposition{
		ID:              id,
		OrganizationID:  actor.OrganizationID,
		ProjectID:       projectID,
		Fingerprint:     strings.TrimSpace(request.Fingerprint),
		IssueKind:       request.IssueKind,
		State:           request.State,
		Note:            strings.TrimSpace(request.Note),
		ObservedThrough: request.ObservedThrough.UTC(),
		Version:         1,
		DecidedBy:       actor.Principal.ID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	return s.Connectors.UpsertQualityDisposition(ctx, record)
}

// --- 检测器 ---

func buildQualityReport(window MetricWindow, sources []DataSource, facts []MetricFactWithMapping,
	batches []ImportBatch, dispositions []QualityDisposition, now time.Time) QualityReport {

	report := QualityReport{Window: window, GeneratedAt: now, ByKind: map[QualityIssueKind]int{}}

	byFingerprint := map[string]QualityDisposition{}
	for _, item := range dispositions {
		byFingerprint[item.Fingerprint] = item
	}

	issues := append(detectFreshness(window, sources, now), detectMissing(window, sources, facts, batches, now)...)
	issues = append(issues, detectAnomaly(sources, facts, now)...)
	issues = append(issues, detectCaliber(facts, now)...)
	issues = append(issues, detectReconciliation(facts, now)...)

	for index := range issues {
		issue := &issues[index]
		disposition, ok := byFingerprint[issue.Fingerprint]
		if !ok {
			issue.State = QualityOpen
			continue
		}
		copied := disposition
		issue.Disposition = &copied
		switch disposition.State {
		case DispositionIgnored:
			// 「这条不用管」是对问题本身的判断，不会因为它又出现一次而改变。
			issue.State = QualityIgnored
		case DispositionResolved:
			if issue.LastObservedAt.After(disposition.ObservedThrough) {
				// 报了修，但问题在那之后又被观测到。这件事必须显式冒出来。
				issue.State = QualityReopened
			} else {
				issue.State = QualityResolved
			}
		default:
			issue.State = QualityAcknowledged
		}
	}

	sort.SliceStable(issues, func(i, j int) bool {
		left, right := issues[i], issues[j]
		// 还在队列里的排前面，然后按严重度，再按影响的花费。
		if left.State.InQueue() != right.State.InQueue() {
			return left.State.InQueue()
		}
		if left.Severity != right.Severity {
			return left.Severity.rank() < right.Severity.rank()
		}
		if left.AffectedSpendCents != right.AffectedSpendCents {
			return left.AffectedSpendCents > right.AffectedSpendCents
		}
		return left.Fingerprint < right.Fingerprint
	})
	report.Issues = issues

	var blocking []string
	for _, issue := range issues {
		if !issue.State.InQueue() {
			continue
		}
		report.QueueCount++
		report.ByKind[issue.Kind]++
		if issue.State == QualityOpen || issue.State == QualityReopened {
			report.OpenCount++
		}
		if issue.Severity == SeverityBlocking {
			blocking = append(blocking, issue.Title)
		}
	}

	// PRD §10.3 / doc10 §12.4：未匹配、延迟或异常数据不产生强洞察或自动优化动作。
	// 已忽略的问题不算——那是有人明确判过「这条不影响结论」。
	report.StrongConclusionsAllowed = len(blocking) == 0
	if len(blocking) > 0 {
		report.BlockedReason = strings.Join(blocking, "；")
	}

	for _, source := range sources {
		report.Sources = append(report.Sources, sourceHealthOf(source, now))
	}
	sort.Slice(report.Sources, func(i, j int) bool {
		return report.Sources[i].FreshnessDays > report.Sources[j].FreshnessDays
	})
	return report
}

func sourceHealthOf(source DataSource, now time.Time) SourceHealth {
	health := SourceHealth{
		DataSourceID: source.ID, Platform: source.Platform, Label: sourceLabel(source),
		Status:        source.Status,
		QualityStatus: source.QualityStatus, QualityNote: source.QualityNote, DataThrough: source.DataThrough,
	}
	if source.DataThrough != nil {
		health.FreshnessDays = int(now.Sub(*source.DataThrough).Hours() / 24)
	}
	// 和 detectFreshness 用同一组条件。写成一个字段而不是让前端自己按天数猜，是因为
	// 「几天算滞后」是这边的口径：前端复制一份阈值，改了 Go 忘了改那边，底表就会开始
	// 说一件和队列相反的事。
	health.FreshnessJudged = source.Status == DataSourceActive &&
		(source.DataThrough == nil || health.FreshnessDays > freshnessDelayedAfterDays)
	return health
}

// detectFreshness：数据够不够新。
//
// 滞后判定只对**已启用**的源做——停用的源本来就不该有新数据，天天报它滞后是噪音。
// 但「不判滞后」不等于「不用管」：一个在窗口中途被暂停或吊销的源，它那部分投放的
// 数据只到暂停那天为止，这个窗口的花费和转化因此是不全的，而据此算出来的对比结论
// 会把「后半段没有数据」读成「后半段表现差」。所以停用的源另走一条检查：只要它的
// 数据止于窗口内部，就报一条覆盖不全（PRD §10.3 说的「延迟数据不产生强洞察」，
// 说的正是这种情况）。
func detectFreshness(window MetricWindow, sources []DataSource, now time.Time) []QualityIssue {
	var issues []QualityIssue
	for _, source := range sources {
		if source.Status != DataSourceActive {
			issues = append(issues, detectStoppedSourceCoverage(window, source, now)...)
			continue
		}
		label := sourceLabel(source)
		if source.DataThrough == nil {
			issues = append(issues, QualityIssue{
				Fingerprint:    fingerprint(QualityFreshness, "never", source.ID),
				Kind:           QualityFreshness,
				Severity:       SeverityWarning,
				Title:          fmt.Sprintf("%s 启用了但一条数据都没有", label),
				Detail:         "数据源处于同步中状态，但至今没有任何指标落库。",
				Suggestion:     "先确认字段映射配好了，再看导入批次里有没有全量被拒的记录。",
				ScopeLabel:     label,
				DataSourceID:   source.ID,
				Platform:       source.Platform,
				LastObservedAt: now,
			})
			continue
		}
		lag := int(now.Sub(*source.DataThrough).Hours() / 24)
		if lag <= freshnessDelayedAfterDays {
			continue
		}
		severity := SeverityWarning
		if lag > freshnessBlockingAfterDays {
			severity = SeverityBlocking
		}
		issues = append(issues, QualityIssue{
			Fingerprint:  fingerprint(QualityFreshness, "lag", source.ID),
			Kind:         QualityFreshness,
			Severity:     severity,
			Title:        fmt.Sprintf("%s 的数据滞后 %d 天", label, lag),
			Detail:       fmt.Sprintf("最新数据只到 %s，今天是 %s。", source.DataThrough.Format("2006-01-02"), now.Format("2006-01-02")),
			Suggestion:   "先查同步记录有没有失败批次；平台侧报表本身有延迟的话，把结论窗口往前挪。",
			ScopeLabel:   label,
			DataSourceID: source.ID,
			Platform:     source.Platform,
			AffectedDays: lag,
			// 滞后是持续状态，此刻仍然成立，所以观测时间就是现在。
			// 这也意味着标记「已修复」后只要还滞后就会立刻弹回 reopened——这是有意的。
			LastObservedAt: now,
		})
	}
	return issues
}

// detectStoppedSourceCoverage：一个停用的源把这个窗口切了一半。
//
// 只在数据止于窗口**内部**时报——止于窗口开始之前的，说明这个源在这一轮里压根没投，
// 它不该出现在这一轮的账上，也就谈不上覆盖不全；止于窗口结束之后的，说明整个窗口都
// 有数据，暂停发生在窗口之后，不影响这一轮。
//
// 严重度是警告不是阻断：数据本身没错，只是只覆盖了一段。是不是接受这个口径要人来判
// ——把它做成阻断，等于每次停一个旧账号就把整轮结论锁死。
func detectStoppedSourceCoverage(window MetricWindow, source DataSource, now time.Time) []QualityIssue {
	if source.Status == DataSourceDraft || source.DataThrough == nil {
		// 草稿源从来没有产出过数据，谈不上覆盖不全。
		return nil
	}
	through := *source.DataThrough
	if !through.After(window.Start) || !through.Before(window.End) {
		return nil
	}
	label := sourceLabel(source)
	missing := int(window.End.Sub(through).Hours() / 24)
	return []QualityIssue{{
		Fingerprint: fingerprint(QualityFreshness, "stopped", source.ID),
		Kind:        QualityFreshness,
		Severity:    SeverityWarning,
		Title:       fmt.Sprintf("%s 已%s，这个窗口只覆盖到 %s", label, source.Status.Label(), through.Format("2006-01-02")),
		Detail: fmt.Sprintf("窗口是 %s 到 %s，这个源的数据只到 %s，后面 %d 天它没有任何数据。",
			window.Start.Format("2006-01-02"), window.End.Format("2006-01-02"), through.Format("2006-01-02"), missing),
		Suggestion: "这一轮的花费和转化少了这个源的后半段。要么把窗口收到 " +
			through.Format("2006-01-02") + " 之前，要么在结论里写明这个源只投了前半段。",
		ScopeLabel:     label,
		DataSourceID:   source.ID,
		Platform:       source.Platform,
		AffectedDays:   missing,
		LastObservedAt: now,
	}}
}

// detectMissing：该有的数据没有。两个来源——窗口内的日期空洞，和导入时被拒的行。
func detectMissing(window MetricWindow, sources []DataSource, facts []MetricFactWithMapping,
	batches []ImportBatch, now time.Time) []QualityIssue {

	var issues []QualityIssue

	// doc10 §7「对账覆盖日期连续性」。只在数据源自己的有数区间内找空洞：
	// 区间之外没有数据是「还没投」或「已经停了」，不是缺失。
	type span struct {
		first, last time.Time
		days        map[string]struct{}
		spend       int64
	}
	spans := map[string]*span{}
	for _, fact := range facts {
		item, ok := spans[fact.DataSourceID]
		if !ok {
			item = &span{first: fact.StatDate, last: fact.StatDate, days: map[string]struct{}{}}
			spans[fact.DataSourceID] = item
		}
		if fact.StatDate.Before(item.first) {
			item.first = fact.StatDate
		}
		if fact.StatDate.After(item.last) {
			item.last = fact.StatDate
		}
		item.days[fact.StatDate.Format("2006-01-02")] = struct{}{}
		item.spend += fact.Counts.SpendCents
	}
	labels := map[string]string{}
	platforms := map[string]Platform{}
	for _, source := range sources {
		labels[source.ID] = sourceLabel(source)
		platforms[source.ID] = source.Platform
	}
	for sourceID, item := range spans {
		var gaps []string
		for day := item.first; !day.After(item.last); day = day.AddDate(0, 0, 1) {
			key := day.Format("2006-01-02")
			if _, ok := item.days[key]; !ok {
				gaps = append(gaps, key)
			}
		}
		if len(gaps) == 0 {
			continue
		}
		label := labels[sourceID]
		if label == "" {
			label = sourceID
		}
		shown := gaps
		if len(shown) > 6 {
			shown = shown[:6]
		}
		issues = append(issues, QualityIssue{
			Fingerprint: fingerprint(QualityMissing, "gap", sourceID),
			Kind:        QualityMissing,
			Severity:    SeverityWarning,
			Title:       fmt.Sprintf("%s 在有数区间里缺了 %d 天", label, len(gaps)),
			Detail: fmt.Sprintf("%s 至 %s 之间这几天没有任何数据：%s%s。",
				item.first.Format("2006-01-02"), item.last.Format("2006-01-02"),
				strings.Join(shown, "、"), more(len(gaps), len(shown))),
			Suggestion:     "缺的这几天要么补导报表，要么在结论里说明窗口不连续——直接按窗口求和会低估花费。",
			ScopeLabel:     label,
			DataSourceID:   sourceID,
			Platform:       platforms[sourceID],
			AffectedDays:   len(gaps),
			LastObservedAt: now,
		})
	}

	// 导入被拒的行：数据没进来，而且这件事只在批次详情里能看到，很容易被忽略。
	for _, batch := range batches {
		if batch.RejectedRows <= 0 {
			continue
		}
		label := labels[batch.DataSourceID]
		if label == "" {
			label = batch.SourceLabel
		}
		observed := batch.UpdatedAt
		if batch.FinishedAt != nil {
			observed = *batch.FinishedAt
		}
		issues = append(issues, QualityIssue{
			Fingerprint: fingerprint(QualityMissing, "rejected", batch.ID),
			Kind:        QualityMissing,
			Severity:    SeverityWarning,
			Title:       fmt.Sprintf("%s 有 %d 行没能入库", label, batch.RejectedRows),
			Detail: fmt.Sprintf("批次「%s」提交 %d 行，入库 %d 行，被拒 %d 行。%s",
				batch.SourceLabel, batch.RequestedRows, batch.AcceptedRows, batch.RejectedRows, batch.ErrorSummary),
			Suggestion:     "打开这个批次看被拒原因，多数是字段映射没覆盖到某一列，或者日期/金额格式不对。",
			ScopeLabel:     label,
			DataSourceID:   batch.DataSourceID,
			LastObservedAt: observed,
		})
	}
	return issues
}

// detectAnomaly：数据本身不对劲。质量状态是人或同步流程标的，不可能值是算出来的。
func detectAnomaly(sources []DataSource, facts []MetricFactWithMapping, now time.Time) []QualityIssue {
	var issues []QualityIssue
	for _, source := range sources {
		if source.QualityStatus == QualityHealthy {
			continue
		}
		label := sourceLabel(source)
		severity := SeverityWarning
		if source.QualityStatus.BlocksStrongConclusion() {
			severity = SeverityBlocking
		}
		issues = append(issues, QualityIssue{
			Fingerprint:    fingerprint(QualityAnomaly, "status", source.ID),
			Kind:           QualityAnomaly,
			Severity:       severity,
			Title:          fmt.Sprintf("%s 的数据质量被标为「%s」", label, source.QualityStatus.Label()),
			Detail:         defaultText(source.QualityNote, "没有留下说明。"),
			Suggestion:     "按 PRD §10.3 的顺序查：先看追踪有没有断，再看素材映射，最后看同步。",
			ScopeLabel:     label,
			DataSourceID:   source.ID,
			Platform:       source.Platform,
			LastObservedAt: source.UpdatedAt,
		})
	}

	// 物理上不可能的值。这类错几乎都是字段映射把列接错了，
	// 一旦进了库就会让点击率之类的派生指标变成一个荒谬但不为零的数。
	type impossible struct {
		spend int64
		days  map[string]struct{}
		what  string
	}
	found := map[string]*impossible{}
	note := func(key, what string, fact MetricFactWithMapping) {
		item, ok := found[key]
		if !ok {
			item = &impossible{days: map[string]struct{}{}, what: what}
			found[key] = item
		}
		item.spend += fact.Counts.SpendCents
		item.days[fact.StatDate.Format("2006-01-02")] = struct{}{}
	}
	latest := map[string]time.Time{}
	for _, fact := range facts {
		key := fact.DataSourceID
		if fact.Counts.Clicks > fact.Counts.Impressions {
			note(key+"\x00clicks", "点击数超过曝光数", fact)
			if fact.StatDate.After(latest[key+"\x00clicks"]) {
				latest[key+"\x00clicks"] = fact.StatDate
			}
		}
		if fact.Counts.Conversions > fact.Counts.Clicks && fact.Counts.Clicks > 0 {
			note(key+"\x00conversions", "转化数超过点击数", fact)
			if fact.StatDate.After(latest[key+"\x00conversions"]) {
				latest[key+"\x00conversions"] = fact.StatDate
			}
		}
		if fact.Counts.VideoCompletions > fact.Counts.VideoViews {
			note(key+"\x00completions", "完播数超过播放数", fact)
			if fact.StatDate.After(latest[key+"\x00completions"]) {
				latest[key+"\x00completions"] = fact.StatDate
			}
		}
	}
	labels := map[string]string{}
	for _, source := range sources {
		labels[source.ID] = sourceLabel(source)
	}
	for key, item := range found {
		sourceID := strings.Split(key, "\x00")[0]
		label := labels[sourceID]
		if label == "" {
			label = sourceID
		}
		observed := latest[key]
		if observed.IsZero() {
			observed = now
		}
		issues = append(issues, QualityIssue{
			Fingerprint: fingerprint(QualityAnomaly, "impossible", key),
			Kind:        QualityAnomaly,
			Severity:    SeverityBlocking,
			Title:       fmt.Sprintf("%s 出现不可能的数值：%s", label, item.what),
			Detail: fmt.Sprintf("涉及 %d 天、%s 花费。这种数在物理上不成立，几乎可以确定是字段映射接错了列。",
				len(item.days), money(item.spend)),
			Suggestion:         "去字段映射核对这两个指标各自对应平台报表的哪一列，改完作为更正批次重导。",
			ScopeLabel:         label,
			DataSourceID:       sourceID,
			AffectedSpendCents: item.spend,
			AffectedDays:       len(item.days),
			LastObservedAt:     observed,
		})
	}
	return issues
}

// detectCaliber：两个数字并排之前必须一致的东西（doc10 §6）。
// 这里只报告窗口内**实际混在一起**的口径，不报数据源配置上的差异——
// 配置不同但从不同时出现在一个窗口里，就不构成问题。
func detectCaliber(facts []MetricFactWithMapping, now time.Time) []QualityIssue {
	currencies := map[string]int64{}
	attributions := map[string]int64{}
	schemas := map[string]int64{}
	var total int64
	for _, fact := range facts {
		currencies[fact.Caliber.Currency] += fact.Counts.SpendCents
		attributions[fact.Caliber.AttributionWindow] += fact.Counts.SpendCents
		schemas[fact.Caliber.MetricSchemaVersion] += fact.Counts.SpendCents
		total += fact.Counts.SpendCents
	}

	var issues []QualityIssue
	add := func(slot string, values map[string]int64, severity QualitySeverity, title, detail, suggestion string) {
		if len(values) <= 1 {
			return
		}
		issues = append(issues, QualityIssue{
			Fingerprint:        fingerprint(QualityCaliber, slot, ""),
			Kind:               QualityCaliber,
			Severity:           severity,
			Title:              title,
			Detail:             fmt.Sprintf("%s窗口内出现：%s。", detail, describeShare(values)),
			Suggestion:         suggestion,
			ScopeLabel:         "整个窗口",
			AffectedSpendCents: total,
			LastObservedAt:     now,
		})
	}
	// 币种混了就直接不能加，这是阻断级——加出来的总额没有意义。
	add("currency", currencies, SeverityBlocking,
		"窗口里混了多种币种", "金额不可直接相加。",
		"先决定用哪个币种作为报告口径，再给其余数据源配汇率换算，换算前不要合并展示。")
	add("attribution", attributions, SeverityBlocking,
		"窗口里混了多种归因窗口", "转化数不可直接比较。",
		"把参与比较的数据源统一到同一个归因窗口，或者分开出两份结论并各自标注。")
	add("schema", schemas, SeverityWarning,
		"窗口里混了多个指标口径版本", "同名指标的定义可能已经变了。",
		"确认新旧版本对涉及的指标定义是否一致；不一致的话按版本分段看。")
	return issues
}

// detectReconciliation：对账（doc10 §7、§12.6）。
// 核心是映射率——有多少花费能归到具体素材上。归不上的钱照样是花掉的钱，
// 但它撑不起「这条创意更好」这类结论（doc10 §5）。
func detectReconciliation(facts []MetricFactWithMapping, now time.Time) []QualityIssue {
	var total, unmatched int64
	objects := map[string]struct{}{}
	perObject := map[string]int64{}
	names := map[string]string{}
	platforms := map[string]Platform{}
	for _, fact := range facts {
		total += fact.Counts.SpendCents
		if fact.AssetID != "" || fact.MappingStatus == MappingIgnored {
			continue
		}
		key := string(fact.Platform) + "\x00" + fact.PlatformObjectKind + "\x00" + fact.PlatformObjectID
		unmatched += fact.Counts.SpendCents
		perObject[key] += fact.Counts.SpendCents
		objects[key] = struct{}{}
		names[key] = fact.PlatformObjectID
		platforms[key] = fact.Platform
	}
	if len(objects) == 0 || total == 0 {
		return nil
	}

	rate := float64(total-unmatched) / float64(total)
	severity := SeverityInfo
	switch {
	case rate < mappingRateBlockingBelow:
		severity = SeverityBlocking
	case rate < mappingRateWarningBelow:
		severity = SeverityWarning
	}
	issues := []QualityIssue{{
		Fingerprint: fingerprint(QualityReconciliation, "mapping-rate", ""),
		Kind:        QualityReconciliation,
		Severity:    severity,
		Title:       fmt.Sprintf("有 %s 花费归不到素材上，映射率 %.1f%%", money(unmatched), rate*100),
		Detail: fmt.Sprintf("窗口内共 %s 花费，其中 %d 个平台对象还没认领。这些钱计入总盘，但不参与素材级比较。",
			money(total), len(objects)),
		Suggestion:         "去「分析素材库 · 待匹配」逐个认领；确实不属于本项目的，标为忽略后它就不再算进未匹配。",
		ScopeLabel:         "整个窗口",
		AffectedSpendCents: unmatched,
		LastObservedAt:     now,
	}}

	// 花得多的未匹配对象单独成条：一个花了几千块的对象没人认，比映射率数字本身更该被看见。
	type entry struct {
		key   string
		spend int64
	}
	var ranked []entry
	for key, spend := range perObject {
		ranked = append(ranked, entry{key, spend})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].spend > ranked[j].spend })
	if len(ranked) > 10 {
		ranked = ranked[:10]
	}
	for _, item := range ranked {
		issues = append(issues, QualityIssue{
			Fingerprint:        fingerprint(QualityReconciliation, "object", item.key),
			Kind:               QualityReconciliation,
			Severity:           SeverityWarning,
			Title:              fmt.Sprintf("平台对象 %s 花了 %s 但没有归属", names[item.key], money(item.spend)),
			Detail:             "它的花费计入总盘，但不会出现在任何素材版本的账上。",
			Suggestion:         "认领到对应的素材版本；如果是测试或非本项目投放，标为忽略。",
			ScopeLabel:         names[item.key],
			Platform:           platforms[item.key],
			AffectedSpendCents: item.spend,
			LastObservedAt:     now,
		})
	}
	return issues
}

// fingerprint 拼出问题的稳定标识。**不含数据窗口**——含了的话换一次窗口
// 指纹就变，处置记录永远跟不住同一个问题。
func fingerprint(kind QualityIssueKind, slot, scope string) string {
	if scope == "" {
		return string(kind) + "|" + slot
	}
	return string(kind) + "|" + slot + "|" + scope
}

func describeShare(values map[string]int64) string {
	type entry struct {
		name  string
		spend int64
	}
	var ranked []entry
	for name, spend := range values {
		if name == "" {
			name = "（未声明）"
		}
		ranked = append(ranked, entry{name, spend})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].spend != ranked[j].spend {
			return ranked[i].spend > ranked[j].spend
		}
		return ranked[i].name < ranked[j].name
	})
	parts := make([]string, 0, len(ranked))
	for _, item := range ranked {
		parts = append(parts, fmt.Sprintf("%s（%s）", item.name, money(item.spend)))
	}
	return strings.Join(parts, "、")
}

func money(cents int64) string {
	return fmt.Sprintf("¥%.2f", float64(cents)/100)
}

func more(total, shown int) string {
	if total <= shown {
		return ""
	}
	return fmt.Sprintf(" 等 %d 天", total)
}

func defaultText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
