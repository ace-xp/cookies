package insights

import (
	"testing"
	"time"
)

// 这些测试直接打 buildQualityReport。它是纯函数（数据源 + 事实 + 批次 + 处置 → 报告），
// 检测逻辑的每一条判断都能在这里钉死，不必绕服务和仓储。

var qualityNow = time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)

func qualityWindow() MetricWindow {
	return MetricWindow{
		Start: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC),
	}
}

func qualityDay(day int) time.Time {
	return time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC)
}

func qualitySource(id string, through *time.Time) DataSource {
	return DataSource{
		ID: id, OrganizationID: "org_1", ProjectID: "prj_1",
		Platform: PlatformDouyin, AccountLabel: id,
		Status: DataSourceActive, QualityStatus: QualityHealthy,
		Caliber:     MetricCaliber{TimeZone: "Asia/Shanghai", Currency: "CNY", AttributionWindow: "7d_click", MetricSchemaVersion: "v1"},
		DataThrough: through,
		UpdatedAt:   qualityNow,
	}
}

func qualityFact(sourceID, objectID string, day int, counts MetricCounts) MetricFactWithMapping {
	return MetricFactWithMapping{
		MetricFact: MetricFact{
			OrganizationID: "org_1", ProjectID: "prj_1", DataSourceID: sourceID,
			Platform: PlatformDouyin, PlatformObjectKind: "creative", PlatformObjectID: objectID,
			StatDate: qualityDay(day),
			Caliber:  MetricCaliber{TimeZone: "Asia/Shanghai", Currency: "CNY", AttributionWindow: "7d_click", MetricSchemaVersion: "v1"},
			Counts:   counts,
		},
		AssetID:       "ast_" + objectID,
		MappingStatus: MappingMatched,
	}
}

func findIssue(t *testing.T, report QualityReport, fingerprint string) QualityIssue {
	t.Helper()
	for _, issue := range report.Issues {
		if issue.Fingerprint == fingerprint {
			return issue
		}
	}
	t.Fatalf("报告里没有指纹为 %s 的问题；现有：%v", fingerprint, fingerprintsOf(report))
	return QualityIssue{}
}

func hasIssue(report QualityReport, fingerprint string) bool {
	for _, issue := range report.Issues {
		if issue.Fingerprint == fingerprint {
			return true
		}
	}
	return false
}

func fingerprintsOf(report QualityReport) []string {
	values := make([]string, 0, len(report.Issues))
	for _, issue := range report.Issues {
		values = append(values, issue.Fingerprint)
	}
	return values
}

func TestQualityFreshnessSeverity(t *testing.T) {
	// freshnessDelayedAfterDays = 2，翻倍 = 4。
	cases := []struct {
		name     string
		through  time.Time
		want     QualitySeverity
		reported bool
	}{
		{"滞后 1 天不报", qualityDay(28), "", false},
		{"滞后 2 天仍在容忍内", qualityDay(27), "", false},
		{"滞后 3 天报警告", qualityDay(26), SeverityWarning, true},
		{"滞后 6 天报阻断", qualityDay(23), SeverityBlocking, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			through := testCase.through
			report := buildQualityReport(qualityWindow(),
				[]DataSource{qualitySource("src_1", &through)}, nil, nil, nil, qualityNow)

			target := fingerprint(QualityFreshness, "lag", "src_1")
			if !testCase.reported {
				if hasIssue(report, target) {
					t.Fatalf("不该报滞后，却报了：%v", fingerprintsOf(report))
				}
				return
			}
			issue := findIssue(t, report, target)
			if issue.Severity != testCase.want {
				t.Fatalf("严重度 = %s，想要 %s", issue.Severity, testCase.want)
			}
			if issue.State != QualityOpen {
				t.Fatalf("没人处置过，状态应为 open，实际 %s", issue.State)
			}
		})
	}
}

// 停用的源不判滞后，但**在窗口中途停掉**要报覆盖不全。
//
// 盯的是一件很容易被漏掉的事：一个源在窗口第 5 天被暂停，后面 4 天它一条数据都没有，
// 而整个窗口的花费和转化仍然被当成完整的一轮去比。旧实现对非 active 的源直接 continue，
// 于是这种情况在界面上一点痕迹都没有——看上去像是那个源后半段表现掉了。
func TestQualityStoppedSourceMidWindowIsReported(t *testing.T) {
	t.Parallel()

	target := fingerprint(QualityFreshness, "stopped", "src_1")

	// 窗口是 7-20 ~ 7-29。数据止于 7-24，源已暂停：窗口后 5 天没有它的数据。
	midway := qualityDay(24)
	paused := qualitySource("src_1", &midway)
	paused.Status = DataSourcePaused
	report := buildQualityReport(qualityWindow(), []DataSource{paused}, nil, nil, nil, qualityNow)
	issue := findIssue(t, report, target)
	if issue.Severity != SeverityWarning {
		t.Fatalf("覆盖不全是警告不是阻断（停一个旧账号不该锁死整轮结论），实际 %s", issue.Severity)
	}
	if issue.AffectedDays != 5 {
		t.Fatalf("少的天数应为 5，实际 %d", issue.AffectedDays)
	}
	if !report.StrongConclusionsAllowed {
		t.Fatal("只有覆盖不全这一条警告时，不该禁止强结论")
	}

	// 数据一直到窗口结束：暂停发生在这一轮之后，和这一轮无关。
	full := qualityDay(29)
	after := qualitySource("src_1", &full)
	after.Status = DataSourcePaused
	if hasIssue(buildQualityReport(qualityWindow(), []DataSource{after}, nil, nil, nil, qualityNow), target) {
		t.Error("数据覆盖了整个窗口，不该报覆盖不全")
	}

	// 数据止于窗口开始之前：这个源在这一轮压根没投过。
	before := qualityDay(19)
	old := qualitySource("src_1", &before)
	old.Status = DataSourceRevoked
	if hasIssue(buildQualityReport(qualityWindow(), []DataSource{old}, nil, nil, nil, qualityNow), target) {
		t.Error("这一轮没投过的源不该报覆盖不全")
	}

	// 草稿源从来没产出过数据。
	draft := qualitySource("src_1", &midway)
	draft.Status = DataSourceDraft
	if hasIssue(buildQualityReport(qualityWindow(), []DataSource{draft}, nil, nil, nil, qualityNow), target) {
		t.Error("草稿源不该报覆盖不全")
	}
}

// 底表上那个天数得能解释自己算不算问题。前端据此在每一行后面写「为什么不算」，
// 而不是自己复制一份阈值——复制的那份迟早和这里对不上，底表就会说一件和队列相反的话。
func TestSourceHealthSaysWhetherFreshnessWasJudged(t *testing.T) {
	t.Parallel()

	tolerated := qualityDay(27) // 落后 2 天，在容忍范围内
	if health := sourceHealthOf(qualitySource("src_1", &tolerated), qualityNow); health.FreshnessJudged {
		t.Error("落后 2 天不进队列，freshness_judged 应为 false")
	}
	lagging := qualityDay(26)
	if health := sourceHealthOf(qualitySource("src_1", &lagging), qualityNow); !health.FreshnessJudged {
		t.Error("落后 3 天进了队列，freshness_judged 应为 true")
	}
	paused := qualitySource("src_1", &lagging)
	paused.Status = DataSourcePaused
	health := sourceHealthOf(paused, qualityNow)
	if health.FreshnessJudged {
		t.Error("停用的源不判滞后")
	}
	if health.Status != DataSourcePaused {
		t.Errorf("生命周期状态没带上：%q", health.Status)
	}
}

func TestQualityBlockingIssueStopsStrongConclusions(t *testing.T) {
	through := qualityDay(23)
	report := buildQualityReport(qualityWindow(),
		[]DataSource{qualitySource("src_1", &through)}, nil, nil, nil, qualityNow)

	// PRD §10.3 / doc10 §12.4：有阻断级问题时不允许给出强结论。
	if report.StrongConclusionsAllowed {
		t.Fatal("存在阻断级问题，却仍允许强结论")
	}
	if report.BlockedReason == "" {
		t.Fatal("禁止强结论时必须说明原因")
	}
}

func TestQualityResolvedFlipsToReopenedWhenObservedAgain(t *testing.T) {
	through := qualityDay(26)
	sources := []DataSource{qualitySource("src_1", &through)}
	target := fingerprint(QualityFreshness, "lag", "src_1")

	// 报修时问题的最新观测时间停在昨天，而这次检测又看到了它。
	stale := QualityDisposition{
		OrganizationID: "org_1", ProjectID: "prj_1",
		Fingerprint: target, IssueKind: QualityFreshness, State: DispositionResolved,
		Note: "已让平台侧重跑同步", ObservedThrough: qualityNow.Add(-24 * time.Hour),
	}
	report := buildQualityReport(qualityWindow(), sources, nil, nil, []QualityDisposition{stale}, qualityNow)
	issue := findIssue(t, report, target)
	if issue.State != QualityReopened {
		t.Fatalf("报修后又被观测到，应为 reopened，实际 %s", issue.State)
	}
	if !issue.State.InQueue() {
		t.Fatal("reopened 必须留在修复队列里，否则点一次「已修复」就能让问题永久消失")
	}
	if issue.Disposition == nil {
		t.Fatal("应带上处置记录，前端要显示是谁在什么时候报的修")
	}

	// 报修时的观测时间不早于这次观测，就认已修复。
	fresh := stale
	fresh.ObservedThrough = qualityNow
	report = buildQualityReport(qualityWindow(), sources, nil, nil, []QualityDisposition{fresh}, qualityNow)
	issue = findIssue(t, report, target)
	if issue.State != QualityResolved {
		t.Fatalf("处置覆盖了最新观测，应为 resolved，实际 %s", issue.State)
	}
	if issue.State.InQueue() {
		t.Fatal("已修复且没再出现，不该继续占着队列")
	}
}

func TestQualityIgnoredNeverComesBack(t *testing.T) {
	through := qualityDay(23)
	sources := []DataSource{qualitySource("src_1", &through)}
	target := fingerprint(QualityFreshness, "lag", "src_1")

	ignored := QualityDisposition{
		OrganizationID: "org_1", ProjectID: "prj_1",
		Fingerprint: target, IssueKind: QualityFreshness, State: DispositionIgnored,
		Note: "这是测试账户，本来就不再同步", ObservedThrough: qualityNow.Add(-72 * time.Hour),
	}
	report := buildQualityReport(qualityWindow(), sources, nil, nil, []QualityDisposition{ignored}, qualityNow)
	issue := findIssue(t, report, target)
	// 「这条不用管」是对问题本身的判断，问题再出现也不改变它——和 resolved 的区别就在这。
	if issue.State != QualityIgnored {
		t.Fatalf("已忽略的问题不该弹回，实际 %s", issue.State)
	}
	if issue.State.InQueue() {
		t.Fatal("已忽略的问题不该占着修复队列")
	}
	// 已忽略的阻断级问题不再拦住强结论——有人明确判过它不影响结论。
	if !report.StrongConclusionsAllowed {
		t.Fatal("唯一的阻断级问题已被忽略，应恢复允许强结论")
	}
}

func TestQualityMappingRateSeverity(t *testing.T) {
	// 映射率 = 有归属花费 / 总花费。阈值：< 80% 阻断，< 95% 警告，其余仅提示。
	cases := []struct {
		name               string
		matched, unmatched int64
		want               QualitySeverity
	}{
		{"映射率 70% 阻断", 70_000, 30_000, SeverityBlocking},
		{"映射率 90% 警告", 90_000, 10_000, SeverityWarning},
		{"映射率 99% 仅提示", 99_000, 1_000, SeverityInfo},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			matched := qualityFact("src_1", "obj_ok", 25, MetricCounts{Impressions: 1000, Clicks: 30, SpendCents: testCase.matched})
			orphan := qualityFact("src_1", "obj_lost", 25, MetricCounts{Impressions: 900, Clicks: 20, SpendCents: testCase.unmatched})
			orphan.AssetID = ""
			orphan.MappingStatus = MappingUnmatched

			through := qualityDay(29)
			report := buildQualityReport(qualityWindow(),
				[]DataSource{qualitySource("src_1", &through)},
				[]MetricFactWithMapping{matched, orphan}, nil, nil, qualityNow)

			issue := findIssue(t, report, fingerprint(QualityReconciliation, "mapping-rate", ""))
			if issue.Severity != testCase.want {
				t.Fatalf("严重度 = %s，想要 %s", issue.Severity, testCase.want)
			}
			if issue.AffectedSpendCents != testCase.unmatched {
				t.Fatalf("影响花费 = %d，想要 %d", issue.AffectedSpendCents, testCase.unmatched)
			}
			// 花得多的未匹配对象要单独成条，映射率数字本身看不出是哪个对象。
			if !hasIssue(report, fingerprint(QualityReconciliation, "object", "douyin\x00creative\x00obj_lost")) {
				t.Fatalf("未匹配对象没有单独成条：%v", fingerprintsOf(report))
			}
		})
	}
}

func TestQualityIgnoredMappingIsNotUnmatched(t *testing.T) {
	// 标为「忽略」的平台对象是人明确判过不属于本项目的，不该继续算进未匹配花费。
	matched := qualityFact("src_1", "obj_ok", 25, MetricCounts{Impressions: 1000, Clicks: 30, SpendCents: 90_000})
	skipped := qualityFact("src_1", "obj_test", 25, MetricCounts{Impressions: 100, Clicks: 2, SpendCents: 10_000})
	skipped.AssetID = ""
	skipped.MappingStatus = MappingIgnored

	through := qualityDay(29)
	report := buildQualityReport(qualityWindow(),
		[]DataSource{qualitySource("src_1", &through)},
		[]MetricFactWithMapping{matched, skipped}, nil, nil, qualityNow)

	if hasIssue(report, fingerprint(QualityReconciliation, "mapping-rate", "")) {
		t.Fatalf("忽略的对象不该算未匹配：%v", fingerprintsOf(report))
	}
}

func TestQualityMissingOnlyInsideDataSpan(t *testing.T) {
	// 22 号到 24 号有数据、23 号断了；窗口从 20 号开始，但 20、21 号只是「还没投」，不算缺。
	facts := []MetricFactWithMapping{
		qualityFact("src_1", "obj_1", 22, MetricCounts{Impressions: 1000, Clicks: 30, SpendCents: 10_000}),
		qualityFact("src_1", "obj_1", 24, MetricCounts{Impressions: 1000, Clicks: 30, SpendCents: 10_000}),
	}
	through := qualityDay(29)
	report := buildQualityReport(qualityWindow(),
		[]DataSource{qualitySource("src_1", &through)}, facts, nil, nil, qualityNow)

	issue := findIssue(t, report, fingerprint(QualityMissing, "gap", "src_1"))
	if issue.AffectedDays != 1 {
		t.Fatalf("缺失天数 = %d，想要 1（只有 23 号，窗口开头的空档不算）", issue.AffectedDays)
	}
}

func TestQualityImpossibleValuesAreBlocking(t *testing.T) {
	// 点击数超过曝光数在物理上不成立，几乎一定是字段映射接错了列，
	// 而它会让点击率变成一个荒谬但不为零的数——必须阻断。
	broken := qualityFact("src_1", "obj_1", 25, MetricCounts{Impressions: 100, Clicks: 500, SpendCents: 10_000})
	through := qualityDay(29)
	report := buildQualityReport(qualityWindow(),
		[]DataSource{qualitySource("src_1", &through)},
		[]MetricFactWithMapping{broken}, nil, nil, qualityNow)

	issue := findIssue(t, report, fingerprint(QualityAnomaly, "impossible", "src_1\x00clicks"))
	if issue.Severity != SeverityBlocking {
		t.Fatalf("严重度 = %s，想要 %s", issue.Severity, SeverityBlocking)
	}
	if report.StrongConclusionsAllowed {
		t.Fatal("存在不可能数值时不该允许强结论")
	}
}

func TestQualityCaliberOnlyWhenActuallyMixed(t *testing.T) {
	through := qualityDay(29)
	sources := []DataSource{qualitySource("src_1", &through), qualitySource("src_2", &through)}

	// 口径一致：不报。
	same := []MetricFactWithMapping{
		qualityFact("src_1", "obj_1", 25, MetricCounts{Impressions: 1000, Clicks: 30, SpendCents: 10_000}),
		qualityFact("src_2", "obj_2", 25, MetricCounts{Impressions: 1000, Clicks: 30, SpendCents: 10_000}),
	}
	report := buildQualityReport(qualityWindow(), sources, same, nil, nil, qualityNow)
	if hasIssue(report, fingerprint(QualityCaliber, "currency", "")) {
		t.Fatalf("口径一致却报了币种混用：%v", fingerprintsOf(report))
	}

	// 币种真的混进同一个窗口：阻断，因为加出来的总额没有意义。
	mixed := append([]MetricFactWithMapping{}, same...)
	mixed[1].Caliber.Currency = "USD"
	report = buildQualityReport(qualityWindow(), sources, mixed, nil, nil, qualityNow)
	issue := findIssue(t, report, fingerprint(QualityCaliber, "currency", ""))
	if issue.Severity != SeverityBlocking {
		t.Fatalf("币种混用应为阻断，实际 %s", issue.Severity)
	}
}

func TestQualityRejectedRowsSurfaceFromBatches(t *testing.T) {
	// 被拒的行只在批次详情里看得到，很容易被忽略，所以要提到质量队列里。
	finished := qualityNow.Add(-2 * time.Hour)
	batch := ImportBatch{
		ID: "bat_1", OrganizationID: "org_1", ProjectID: "prj_1", DataSourceID: "src_1",
		SourceLabel: "7月报表.csv", RequestedRows: 100, AcceptedRows: 88, RejectedRows: 12,
		ErrorSummary: "日期列格式不对", FinishedAt: &finished, UpdatedAt: qualityNow,
	}
	through := qualityDay(29)
	report := buildQualityReport(qualityWindow(),
		[]DataSource{qualitySource("src_1", &through)}, nil, []ImportBatch{batch}, nil, qualityNow)

	issue := findIssue(t, report, fingerprint(QualityMissing, "rejected", "bat_1"))
	if !issue.LastObservedAt.Equal(finished) {
		t.Fatalf("观测时间应取批次完成时间 %s，实际 %s", finished, issue.LastObservedAt)
	}
	if report.ByKind[QualityMissing] != 1 {
		t.Fatalf("缺失类角标 = %d，想要 1", report.ByKind[QualityMissing])
	}
}

func TestQualityFingerprintIgnoresWindow(t *testing.T) {
	// 指纹不含窗口：换个窗口看同一个问题，处置记录必须还跟得住。
	through := qualityDay(26)
	sources := []DataSource{qualitySource("src_1", &through)}

	wide := buildQualityReport(qualityWindow(), sources, nil, nil, nil, qualityNow)
	narrow := buildQualityReport(MetricWindow{Start: qualityDay(27), End: qualityDay(29)},
		sources, nil, nil, nil, qualityNow)

	if len(wide.Issues) == 0 || len(narrow.Issues) == 0 {
		t.Fatal("两个窗口都应检出滞后问题")
	}
	if wide.Issues[0].Fingerprint != narrow.Issues[0].Fingerprint {
		t.Fatalf("换窗口后指纹变了：%s vs %s", wide.Issues[0].Fingerprint, narrow.Issues[0].Fingerprint)
	}
}

func TestQualityQueueSortsBlockingFirst(t *testing.T) {
	through := qualityDay(23) // 阻断级滞后
	source := qualitySource("src_1", &through)
	orphan := qualityFact("src_1", "obj_lost", 25, MetricCounts{Impressions: 900, Clicks: 20, SpendCents: 500})
	orphan.AssetID = ""
	orphan.MappingStatus = MappingUnmatched
	matched := qualityFact("src_1", "obj_ok", 25, MetricCounts{Impressions: 1000, Clicks: 30, SpendCents: 99_500})

	report := buildQualityReport(qualityWindow(), []DataSource{source},
		[]MetricFactWithMapping{matched, orphan}, nil, nil, qualityNow)

	// 20 §4.1：错误与延迟置顶。
	if report.Issues[0].Severity != SeverityBlocking {
		t.Fatalf("队首严重度 = %s，想要 %s；顺序：%v",
			report.Issues[0].Severity, SeverityBlocking, fingerprintsOf(report))
	}
	if report.QueueCount != len(report.Issues) {
		t.Fatalf("没人处置过，队列数 %d 应等于问题数 %d", report.QueueCount, len(report.Issues))
	}
	if report.OpenCount != report.QueueCount {
		t.Fatalf("未处理数 %d 应等于队列数 %d", report.OpenCount, report.QueueCount)
	}
}
