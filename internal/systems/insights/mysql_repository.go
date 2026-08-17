package insights

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

type MySQLRepository struct {
	DB *sql.DB
}

func (r MySQLRepository) CreateReport(ctx context.Context, value InsightReport) (InsightReport, error) {
	findings, err := json.Marshal(value.Findings)
	if err != nil {
		return InsightReport{}, err
	}
	digest, err := marshalReportDigest(value.Digest)
	if err != nil {
		return InsightReport{}, err
	}
	_, err = r.DB.ExecContext(ctx, `INSERT INTO insight_reports (
		id, organization_id, project_id, execution_id, delivery_mode, evidence_id, evidence_summary,
		metric_snapshot_id, creative_package_id, is_simulated, dataset_version,
		status, summary, findings, digest, window_start, window_end,
		version, created_by, confirmed_by, confirmed_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?)`,
		value.ID, value.OrganizationID, value.ProjectID, value.ExecutionID, value.DeliveryMode,
		value.EvidenceID, value.EvidenceSummary, value.MetricSnapshotID, value.CreativePackageID,
		value.IsSimulated, value.DatasetVersion, value.Status, value.Summary, findings,
		digest, value.WindowStart, value.WindowEnd,
		value.Version, value.CreatedBy, value.CreatedAt, value.UpdatedAt)
	// 唯一键只约束还没提交的草稿（uq_insight_reports_open_draft 里的 draft_slot
	// 对已确认的行是 NULL）。所以撞上它只有一种意思：这个窗口已经开着一份草稿了，
	// 不是「这一轮复盘过了」——已经提交过的那份不挡新草稿，PRD §15.3 要的就是
	// 提交之后还能开下一份。
	if isDuplicateKey(err) {
		return InsightReport{}, fmt.Errorf("%w: 这个数据窗口已经有一份还没提交的复盘草稿了，去「复盘」看那一份", ErrInvalidState)
	}
	if err != nil {
		return InsightReport{}, err
	}
	return value, nil
}

// UpdateReportDigest 覆盖整份汇总。人工删减改的是 dropped 标记，条目本身不删——
// 报告要能说清「系统给了什么、人拿掉了哪几条」，物理删掉就查不回来了。
func (r MySQLRepository) UpdateReportDigest(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string, expectedVersion int64, digest []ReportFinding, now time.Time) (InsightReport, error) {
	encoded, err := marshalReportDigest(digest)
	if err != nil {
		return InsightReport{}, err
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE insight_reports SET digest = ?, version = version + 1, updated_at = ? WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ? AND status = ?`,
		encoded, now, organizationID, projectID, id, expectedVersion, ReportDraft)
	if err != nil {
		return InsightReport{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return InsightReport{}, err
	}
	if affected == 0 {
		value, getErr := r.GetReport(ctx, organizationID, projectID, id)
		if getErr != nil {
			return InsightReport{}, getErr
		}
		if value.Version != expectedVersion {
			return InsightReport{}, ErrVersionConflict
		}
		return InsightReport{}, ErrInvalidState
	}
	return r.GetReport(ctx, organizationID, projectID, id)
}

func marshalReportDigest(digest []ReportFinding) ([]byte, error) {
	if digest == nil {
		digest = make([]ReportFinding, 0)
	}
	return json.Marshal(digest)
}

func (r MySQLRepository) ListReports(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, limit int) ([]InsightReport, error) {
	rows, err := r.DB.QueryContext(ctx, insightReportSelect+` WHERE organization_id = ? AND project_id = ? ORDER BY updated_at DESC, id DESC LIMIT ?`, organizationID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]InsightReport, 0)
	for rows.Next() {
		value, scanErr := scanInsightReport(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r MySQLRepository) GetReport(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (InsightReport, error) {
	value, err := scanInsightReport(r.DB.QueryRowContext(ctx, insightReportSelect+` WHERE organization_id = ? AND project_id = ? AND id = ?`, organizationID, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return InsightReport{}, ErrNotFound
	}
	return value, err
}

// FindDraftByWindow 只找 draft，不找已确认的。已确认的复盘是定格的，
// 往里加一条新发现等于事后改结论。提交之后还想记，开的是下一份草稿——
// 唯一键放得下（uq_insight_reports_open_draft 只约束草稿）。
//
// 同一个 (项目 + 窗口) 下可能有多份草稿——唯一键里还有 execution_id，从投放执行
// 建出来的草稿和记一笔建出来的草稿会并存。按创建时间取最早那份：人在这个窗口上
// 第一次记一笔建的那份，就是这轮复盘的正主。
func (r MySQLRepository) FindDraftByWindow(ctx context.Context, organizationID contract.OrganizationID,
	projectID contract.ProjectID, windowStart, windowEnd string) (InsightReport, error) {
	var id string
	err := r.DB.QueryRowContext(ctx, `
		SELECT id FROM insight_reports
		WHERE organization_id = ? AND project_id = ? AND window_start = ? AND window_end = ?
		  AND status = ?
		ORDER BY created_at ASC, id ASC LIMIT 1`,
		string(organizationID), string(projectID), windowStart, windowEnd, string(ReportDraft),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return InsightReport{}, ErrNotFound
	}
	if err != nil {
		return InsightReport{}, err
	}
	return r.GetReport(ctx, organizationID, projectID, id)
}

func (r MySQLRepository) ConfirmReport(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string, expectedVersion int64, actorID string, now time.Time) (InsightReport, error) {
	result, err := r.DB.ExecContext(ctx, `UPDATE insight_reports SET status = ?, confirmed_by = ?, confirmed_at = ?, version = version + 1, updated_at = ? WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ? AND status = ?`,
		ReportConfirmed, actorID, now, now, organizationID, projectID, id, expectedVersion, ReportDraft)
	if err != nil {
		return InsightReport{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return InsightReport{}, err
	}
	if affected == 0 {
		value, getErr := r.GetReport(ctx, organizationID, projectID, id)
		if getErr != nil {
			return InsightReport{}, getErr
		}
		if value.Version != expectedVersion {
			return InsightReport{}, ErrVersionConflict
		}
		return InsightReport{}, ErrInvalidState
	}
	return r.GetReport(ctx, organizationID, projectID, id)
}

// PurgeEmptyDrafts 清掉过了保留期还是一条发现都没有的草稿。
//
// 只按 created_at 删会连真的复盘草稿一起删掉，所以必须同时看内容：
// JSON_LENGTH(digest) = 0 才是「记一笔建了但人什么都没记」的残留。
func (r MySQLRepository) PurgeEmptyDrafts(ctx context.Context, before time.Time) (int64, error) {
	result, err := r.DB.ExecContext(ctx,
		`DELETE FROM insight_reports WHERE status = ? AND created_at < ? AND JSON_LENGTH(digest) = 0`,
		ReportDraft, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SubmitReport 补执行 ID 和摘要、写入定格后的 digest、置为已确认——一条 UPDATE 做完。
//
// 分成几次写会留下「已确认但没有系统发现」的报告，而它看起来和正常的一模一样，
// 没人会怀疑那份复盘漏了东西。
func (r MySQLRepository) SubmitReport(ctx context.Context, input SubmitReportInput) (InsightReport, error) {
	organizationID, projectID, reportID := input.OrganizationID, input.ProjectID, input.ReportID
	expectedVersion := input.ExpectedVersion
	encoded, err := marshalReportDigest(input.Digest)
	if err != nil {
		return InsightReport{}, err
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE insight_reports SET execution_id = ?, summary = ?, digest = ?, status = ?, confirmed_by = ?, confirmed_at = ?, version = version + 1, updated_at = ? WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ? AND status = ?`,
		input.ExecutionID, input.Summary, encoded, ReportConfirmed, input.ActorID, input.At, input.At,
		organizationID, projectID, reportID, expectedVersion, ReportDraft)
	if err != nil {
		// 这一下同时把 status 改成 confirmed，而唯一键里的 draft_slot 对已确认的行
		// 是 NULL，所以提交本身撞不上唯一键——同一个窗口允许躺着多份已确认的历史
		// 报告。留着这个分支是防唯一键哪天又改回去：那时候扔出去的会是一个裸的
		// 重复键错误，前端只能显示成 500。
		if isDuplicateKey(err) {
			return InsightReport{}, fmt.Errorf("%w: 这次投放在这个窗口上已经有一份复盘了，先看那一份", ErrInvalidState)
		}
		return InsightReport{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return InsightReport{}, err
	}
	if affected == 0 {
		value, getErr := r.GetReport(ctx, organizationID, projectID, reportID)
		if getErr != nil {
			return InsightReport{}, getErr
		}
		if value.Version != expectedVersion {
			return InsightReport{}, ErrVersionConflict
		}
		return InsightReport{}, ErrInvalidState
	}
	return r.GetReport(ctx, organizationID, projectID, reportID)
}

// CreateExperience writes the conclusion and its opening audit row together so
// a 待确认 experience can never exist without a trail (PRD §11.2).
func (r MySQLRepository) CreateExperience(ctx context.Context, value Experience, audit ExperienceAudit) (Experience, error) {
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		if txErr := insertExperienceTx(ctx, tx, value); txErr != nil {
			return txErr
		}
		return insertExperienceAudit(ctx, tx, audit)
	})
	if err != nil {
		return Experience{}, err
	}
	return value, nil
}

// ReviseExperience appends a revision and, when the predecessor was never
// confirmed, retires it in the same transaction. Two 待确认 rows of one lineage
// must never sit in the candidate queue together: they read as two independent
// conclusions, and confirming the older one silently discards the revision.
func (r MySQLRepository) ReviseExperience(ctx context.Context, input ReviseExperienceInput) (Experience, error) {
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		if txErr := insertExperienceTx(ctx, tx, input.Value); txErr != nil {
			return txErr
		}
		if txErr := insertExperienceAudit(ctx, tx, input.Audit); txErr != nil {
			return txErr
		}
		if input.RetireSource == nil {
			return nil
		}
		if _, txErr := transitionExperienceTx(ctx, tx, *input.RetireSource); txErr != nil {
			return txErr
		}
		_, txErr := tx.ExecContext(ctx, `UPDATE insight_experiences SET superseded_by_id = ? WHERE organization_id = ? AND project_id = ? AND id = ?`,
			input.Value.ID, input.RetireSource.OrganizationID, input.RetireSource.ProjectID, input.RetireSource.ID)
		return txErr
	})
	if err != nil {
		return Experience{}, err
	}
	return input.Value, nil
}

func insertExperienceTx(ctx context.Context, tx *sql.Tx, value Experience) error {
	conditions, err := json.Marshal(value.Conditions)
	if err != nil {
		return err
	}
	counterexamples, err := json.Marshal(value.Counterexamples)
	if err != nil {
		return err
	}
	applicability, err := json.Marshal(value.Applicability)
	if err != nil {
		return err
	}
	dataBasis, err := json.Marshal(value.DataBasis)
	if err != nil {
		return err
	}
	contentBasis, err := json.Marshal(value.ContentBasis)
	if err != nil {
		return err
	}
	// 阈值版本用 NULL 表示「不知道」，不是 0。0 是「按出厂设定判的」，也是一个
	// 确定的答案；把不知道写成 0，界面上就会给一条人手填档位的经验盖上出厂印。
	var thresholdVersion any
	if value.ThresholdVersion != nil {
		thresholdVersion = *value.ThresholdVersion
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO insight_experiences (
		id, organization_id, project_id, lineage_id, revision, supersedes_id, superseded_by_id,
		report_id, source_execution_id, source_evidence_id, source_metric_snapshot_id,
		conclusion, card_type, confidence, threshold_version, recommended_action,
		conditions, counterexamples, applicability, data_basis, content_basis,
		status, needs_review, status_reason, status_changed_by, status_changed_at,
		confirmed_by, confirmed_at, version, created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?)`,
		value.ID, value.OrganizationID, value.ProjectID, value.LineageID, value.Revision,
		nullableString(value.SupersedesID),
		value.ReportID, value.SourceExecutionID, value.SourceEvidenceID, value.SourceMetricSnapshotID,
		value.Conclusion, value.CardType, value.Confidence, thresholdVersion, value.RecommendedAction,
		conditions, counterexamples, applicability, dataBasis, contentBasis,
		value.Status, value.NeedsReview, value.StatusReason,
		value.StatusChangedBy, value.StatusChangedAt,
		value.Version, value.CreatedBy, value.CreatedAt, value.UpdatedAt)
	return err
}

// ListExperiences filters by lifecycle status when one is given; an empty
// status returns every revision, including retired ones, so the library stays
// auditable.
func (r MySQLRepository) ListExperiences(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, status ExperienceStatus, limit int) ([]Experience, error) {
	query := experienceSelect + ` WHERE organization_id = ? AND project_id = ?`
	args := []any{organizationID, projectID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	return r.queryExperiences(ctx, query, args...)
}

func (r MySQLRepository) GetExperience(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (Experience, error) {
	value, err := scanExperience(r.DB.QueryRowContext(ctx, experienceSelect+` WHERE organization_id = ? AND project_id = ? AND id = ?`, organizationID, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Experience{}, ErrNotFound
	}
	return value, err
}

// ListExperienceLineage returns every revision of one conclusion oldest first,
// so the UI can show how a conclusion evolved instead of only its latest form.
func (r MySQLRepository) ListExperienceLineage(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, lineageID string) ([]Experience, error) {
	return r.queryExperiences(ctx, experienceSelect+` WHERE organization_id = ? AND project_id = ? AND lineage_id = ? ORDER BY revision ASC`,
		organizationID, projectID, lineageID)
}

func (r MySQLRepository) TransitionExperience(ctx context.Context, input TransitionExperienceInput) (Experience, error) {
	var value Experience
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		var txErr error
		value, txErr = transitionExperienceTx(ctx, tx, input)
		return txErr
	})
	if err != nil {
		return Experience{}, err
	}
	return value, nil
}

// FlagExperienceForReview 只翻 needs_review 那一格，状态一动不动。
//
// 状态没变，审计照写一条，from_status 和 to_status 都是 confirmed——审计如实记
// 发生过什么，要看清楚发生的是哪件事，看 reason。少写这条审计的代价是：一条经验
// 上突然多了个「该看一眼了」，没人查得出是谁在什么时候基于什么加的。
func (r MySQLRepository) FlagExperienceForReview(ctx context.Context, input FlagExperienceReviewInput) (Experience, error) {
	var value Experience
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		current, txErr := getExperienceForUpdate(ctx, tx, input.OrganizationID, input.ProjectID, input.ID)
		if txErr != nil {
			return txErr
		}
		if current.Version != input.ExpectedVersion {
			return ErrVersionConflict
		}
		// 只有在用的经验才谈得上复审。待定的还没人认可，停用的已经不在引用集里。
		if current.Status != ExperienceConfirmed {
			return ErrInvalidState
		}
		if _, txErr = tx.ExecContext(ctx, `UPDATE insight_experiences SET needs_review = ?, status_reason = ?, status_changed_by = ?, status_changed_at = ?, version = version + 1, updated_at = ? WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ? AND status = 'confirmed'`,
			input.NeedsReview, input.Reason, input.ActorID, input.Now, input.Now,
			input.OrganizationID, input.ProjectID, input.ID, input.ExpectedVersion); txErr != nil {
			return txErr
		}
		if txErr = insertExperienceAudit(ctx, tx, ExperienceAudit{
			ID: input.AuditID, OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
			ExperienceID: input.ID, FromStatus: ExperienceConfirmed, ToStatus: ExperienceConfirmed,
			Reason: input.Reason, ActorID: input.ActorID, CreatedAt: input.Now,
		}); txErr != nil {
			return txErr
		}
		current.NeedsReview = input.NeedsReview
		current.StatusReason = input.Reason
		current.StatusChangedBy = input.ActorID
		current.StatusChangedAt = &input.Now
		current.Version++
		current.UpdatedAt = input.Now
		value = current
		return nil
	})
	if err != nil {
		return Experience{}, err
	}
	return value, nil
}

// ConfirmExperience makes a revision quotable and retires the one it supersedes
// in the same transaction, so a lineage never has two reusable conclusions.
func (r MySQLRepository) ConfirmExperience(ctx context.Context, input ConfirmExperienceInput) (Experience, error) {
	var value Experience
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		confirmed, txErr := transitionExperienceTx(ctx, tx, TransitionExperienceInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, ID: input.ID,
			ExpectedVersion: input.ExpectedVersion,
			// 在用也能确认：那是「标了复审、重新看过、还成立」这条路径。
			From: []ExperienceStatus{ExperiencePending, ExperienceConfirmed},
			To:   ExperienceConfirmed, ActorID: input.ActorID, Now: input.Now, AuditID: input.AuditID,
		})
		if txErr != nil {
			return txErr
		}
		// 确认就等于看过了，顺手摘掉复审标记；否则它会一直挂着，下次没人知道到底看过没有。
		if _, txErr = tx.ExecContext(ctx, `UPDATE insight_experiences SET confirmed_by = ?, confirmed_at = ?, needs_review = 0 WHERE organization_id = ? AND project_id = ? AND id = ?`,
			input.ActorID, input.Now, input.OrganizationID, input.ProjectID, input.ID); txErr != nil {
			return txErr
		}
		confirmed.ConfirmedBy = input.ActorID
		confirmed.ConfirmedAt = &input.Now
		confirmed.NeedsReview = false
		value = confirmed
		if confirmed.SupersedesID == "" {
			return nil
		}
		previous, txErr := getExperienceForUpdate(ctx, tx, input.OrganizationID, input.ProjectID, confirmed.SupersedesID)
		if txErr != nil {
			return txErr
		}
		if previous.Status == ExperienceRetired {
			return nil
		}
		if _, txErr = transitionExperienceTx(ctx, tx, TransitionExperienceInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, ID: previous.ID,
			ExpectedVersion: previous.Version,
			From:            []ExperienceStatus{ExperiencePending, ExperienceConfirmed},
			To:              ExperienceRetired,
			Reason:          fmt.Sprintf("已被第 %d 版取代。", confirmed.Revision),
			ActorID:         input.ActorID, Now: input.Now, AuditID: input.SupersedeAuditID,
		}); txErr != nil {
			return txErr
		}
		_, txErr = tx.ExecContext(ctx, `UPDATE insight_experiences SET superseded_by_id = ? WHERE organization_id = ? AND project_id = ? AND id = ?`,
			confirmed.ID, input.OrganizationID, input.ProjectID, previous.ID)
		return txErr
	})
	if err != nil {
		return Experience{}, err
	}
	return value, nil
}

func (r MySQLRepository) CreateExperienceReference(ctx context.Context, value ExperienceReference) (ExperienceReference, error) {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO insight_experience_references (
		id, organization_id, project_id, experience_id, consumer_kind, consumer_id, outcome, note,
		version, created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE outcome = VALUES(outcome), note = VALUES(note), version = version + 1, updated_at = VALUES(updated_at)`,
		value.ID, value.OrganizationID, value.ProjectID, value.ExperienceID, value.ConsumerKind,
		value.ConsumerID, value.Outcome, value.Note, value.Version, value.CreatedBy, value.CreatedAt, value.UpdatedAt)
	if err != nil {
		return ExperienceReference{}, err
	}
	stored, err := scanExperienceReference(r.DB.QueryRowContext(ctx, experienceReferenceSelect+` WHERE organization_id = ? AND experience_id = ? AND consumer_kind = ? AND consumer_id = ?`,
		value.OrganizationID, value.ExperienceID, value.ConsumerKind, value.ConsumerID))
	if errors.Is(err, sql.ErrNoRows) {
		return ExperienceReference{}, ErrNotFound
	}
	return stored, err
}

// An empty experienceID lists every reference in the project, which is what the
// 引用记录 view needs to answer "who used our experiences and how did it go".
func (r MySQLRepository) ListExperienceReferences(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, experienceID string, limit int) ([]ExperienceReference, error) {
	query := experienceReferenceSelect + ` WHERE organization_id = ? AND project_id = ?`
	arguments := []any{organizationID, projectID}
	if experienceID != "" {
		query += ` AND experience_id = ?`
		arguments = append(arguments, experienceID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	rows, err := r.DB.QueryContext(ctx, query, append(arguments, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]ExperienceReference, 0)
	for rows.Next() {
		value, scanErr := scanExperienceReference(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r MySQLRepository) ListExperienceAudits(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, experienceID string, limit int) ([]ExperienceAudit, error) {
	rows, err := r.DB.QueryContext(ctx, experienceAuditSelect+` WHERE organization_id = ? AND project_id = ? AND experience_id = ? ORDER BY sequence ASC LIMIT ?`,
		organizationID, projectID, experienceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]ExperienceAudit, 0)
	for rows.Next() {
		var value ExperienceAudit
		if scanErr := rows.Scan(&value.ID, &value.OrganizationID, &value.ProjectID, &value.ExperienceID,
			&value.FromStatus, &value.ToStatus, &value.Reason, &value.ActorID, &value.CreatedAt); scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r MySQLRepository) queryExperiences(ctx context.Context, query string, args ...any) ([]Experience, error) {
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]Experience, 0)
	for rows.Next() {
		value, scanErr := scanExperience(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r MySQLRepository) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// transitionExperienceTx locks the row, enforces the optimistic version and the
// allowed source states, then records the move.
func transitionExperienceTx(ctx context.Context, tx *sql.Tx, input TransitionExperienceInput) (Experience, error) {
	current, err := getExperienceForUpdate(ctx, tx, input.OrganizationID, input.ProjectID, input.ID)
	if err != nil {
		return Experience{}, err
	}
	if current.Version != input.ExpectedVersion {
		return Experience{}, ErrVersionConflict
	}
	if !allowsStatus(input.From, current.Status) {
		return Experience{}, ErrInvalidState
	}
	if _, err := tx.ExecContext(ctx, `UPDATE insight_experiences SET status = ?, status_reason = ?, status_changed_by = ?, status_changed_at = ?, version = version + 1, updated_at = ? WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ?`,
		input.To, input.Reason, input.ActorID, input.Now, input.Now,
		input.OrganizationID, input.ProjectID, input.ID, input.ExpectedVersion); err != nil {
		return Experience{}, err
	}
	if err := insertExperienceAudit(ctx, tx, ExperienceAudit{
		ID: input.AuditID, OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		ExperienceID: input.ID, FromStatus: current.Status, ToStatus: input.To,
		Reason: input.Reason, ActorID: input.ActorID, CreatedAt: input.Now,
	}); err != nil {
		return Experience{}, err
	}
	current.Status = input.To
	current.StatusReason = input.Reason
	current.StatusChangedBy = input.ActorID
	current.StatusChangedAt = &input.Now
	current.Version++
	current.UpdatedAt = input.Now
	return current, nil
}

func getExperienceForUpdate(ctx context.Context, tx *sql.Tx, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (Experience, error) {
	value, err := scanExperience(tx.QueryRowContext(ctx, experienceSelect+` WHERE organization_id = ? AND project_id = ? AND id = ? FOR UPDATE`, organizationID, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Experience{}, ErrNotFound
	}
	return value, err
}

func insertExperienceAudit(ctx context.Context, tx *sql.Tx, value ExperienceAudit) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO insight_experience_audits (
		id, organization_id, project_id, experience_id, from_status, to_status, reason, actor_id, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.OrganizationID, value.ProjectID, value.ExperienceID,
		value.FromStatus, value.ToStatus, value.Reason, value.ActorID, value.CreatedAt)
	return err
}

func allowsStatus(values []ExperienceStatus, value ExperienceStatus) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// unmarshalNullableJSON 把 NULL 或空列当成零值。用在后加的 JSON 列上——
// 历史行没有这些字段不是数据损坏，报错会让整张经验库读不出来。
func unmarshalNullableJSON(raw []byte, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, target)
}

const insightReportSelect = `SELECT id, organization_id, project_id, execution_id, delivery_mode, evidence_id, evidence_summary, metric_snapshot_id, creative_package_id, is_simulated, dataset_version, status, summary, findings, digest, window_start, window_end, version, created_by, confirmed_by, confirmed_at, created_at, updated_at FROM insight_reports`
const experienceSelect = `SELECT id, organization_id, project_id, lineage_id, revision, supersedes_id, superseded_by_id, report_id, source_execution_id, source_evidence_id, source_metric_snapshot_id, conclusion, card_type, confidence, threshold_version, recommended_action, conditions, counterexamples, applicability, data_basis, content_basis, status, needs_review, status_reason, status_changed_by, status_changed_at, confirmed_by, confirmed_at, version, created_by, created_at, updated_at FROM insight_experiences`
const experienceAuditSelect = `SELECT id, organization_id, project_id, experience_id, from_status, to_status, reason, actor_id, created_at FROM insight_experience_audits`
const experienceReferenceSelect = `SELECT id, organization_id, project_id, experience_id, consumer_kind, consumer_id, outcome, note, version, created_by, created_at, updated_at FROM insight_experience_references`

type rowScanner interface {
	Scan(...any) error
}

func scanInsightReport(row rowScanner) (InsightReport, error) {
	var value InsightReport
	var findings, digest []byte
	var confirmedBy, windowStart, windowEnd sql.NullString
	var confirmedAt sql.NullTime
	err := row.Scan(&value.ID, &value.OrganizationID, &value.ProjectID, &value.ExecutionID, &value.DeliveryMode,
		&value.EvidenceID, &value.EvidenceSummary, &value.MetricSnapshotID, &value.CreativePackageID,
		&value.IsSimulated, &value.DatasetVersion, &value.Status, &value.Summary, &findings,
		&digest, &windowStart, &windowEnd, &value.Version,
		&value.CreatedBy, &confirmedBy, &confirmedAt, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return InsightReport{}, err
	}
	if err := json.Unmarshal(findings, &value.Findings); err != nil {
		return InsightReport{}, fmt.Errorf("decode insight findings: %w", err)
	}
	// 老报告这一列是空的。空切片而不是 nil：nil 序列化成 null，前端拿到会白屏。
	value.Digest = make([]ReportFinding, 0)
	if err := unmarshalNullableJSON(digest, &value.Digest); err != nil {
		return InsightReport{}, fmt.Errorf("decode insight digest: %w", err)
	}
	if value.Digest == nil {
		value.Digest = make([]ReportFinding, 0)
	}
	// 补齐旧行：digest 是 JSON 列，早先存进去的发现只有 confidence，没有 verdict
	// 也没有 origin。补在这里而不是各个查询方法里，是因为报告只有这一条读取路径，
	// 漏一个入口就会有一半的发现在复盘页上没有档位。
	for index := range value.Digest {
		value.Digest[index].normalize()
	}
	value.WindowStart = windowStart.String
	value.WindowEnd = windowEnd.String
	if confirmedBy.Valid {
		value.ConfirmedBy = confirmedBy.String
	}
	if confirmedAt.Valid {
		value.ConfirmedAt = &confirmedAt.Time
	}
	return value, nil
}

func scanExperience(row rowScanner) (Experience, error) {
	var value Experience
	var conditions, counterexamples []byte
	var applicability, dataBasis, contentBasis []byte
	var supersedesID, supersededByID, confirmedBy sql.NullString
	var statusChangedAt, confirmedAt sql.NullTime
	var thresholdVersion sql.NullInt64
	err := row.Scan(&value.ID, &value.OrganizationID, &value.ProjectID,
		&value.LineageID, &value.Revision, &supersedesID, &supersededByID, &value.ReportID,
		&value.SourceExecutionID, &value.SourceEvidenceID, &value.SourceMetricSnapshotID, &value.Conclusion,
		&value.CardType, &value.Confidence, &thresholdVersion, &value.RecommendedAction, &conditions,
		&counterexamples, &applicability, &dataBasis, &contentBasis,
		&value.Status, &value.NeedsReview, &value.StatusReason, &value.StatusChangedBy, &statusChangedAt,
		&confirmedBy, &confirmedAt, &value.Version, &value.CreatedBy, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return Experience{}, err
	}
	// 库里只存 confidence。三档判定不落库，每次读出来由唯一收敛点重新算——
	// 存下来的话，收敛规则改了以后老行还带着旧档位，同一条经验在两个页面上会不一样。
	value.Judgement = judge(value.Confidence, "")
	// 阈值版本例外：它不是算出来的，是当初判这一档时生效的那一版的号码，
	// 存的是历史事实，重算不出来。NULL 保持为 nil（不知道），不落成 0（出厂设定）。
	if thresholdVersion.Valid {
		version := thresholdVersion.Int64
		value.ThresholdVersion = &version
	}
	if err := json.Unmarshal(conditions, &value.Conditions); err != nil {
		return Experience{}, fmt.Errorf("decode experience conditions: %w", err)
	}
	if err := json.Unmarshal(counterexamples, &value.Counterexamples); err != nil {
		return Experience{}, fmt.Errorf("decode experience counterexamples: %w", err)
	}
	// 三个卡片字段是这次扩展加的，历史行是 NULL。NULL 解成零值而不是报错——
	// 老经验没写适用范围是事实，投影时会把它标进 MissingFields。
	if err := unmarshalNullableJSON(applicability, &value.Applicability); err != nil {
		return Experience{}, fmt.Errorf("decode experience applicability: %w", err)
	}
	if err := unmarshalNullableJSON(dataBasis, &value.DataBasis); err != nil {
		return Experience{}, fmt.Errorf("decode experience data basis: %w", err)
	}
	if err := unmarshalNullableJSON(contentBasis, &value.ContentBasis); err != nil {
		return Experience{}, fmt.Errorf("decode experience content basis: %w", err)
	}
	value.SupersedesID = supersedesID.String
	value.SupersededByID = supersededByID.String
	value.ConfirmedBy = confirmedBy.String
	if statusChangedAt.Valid {
		value.StatusChangedAt = &statusChangedAt.Time
	}
	if confirmedAt.Valid {
		value.ConfirmedAt = &confirmedAt.Time
	}
	return value, nil
}

func scanExperienceReference(row rowScanner) (ExperienceReference, error) {
	var value ExperienceReference
	err := row.Scan(&value.ID, &value.OrganizationID, &value.ProjectID, &value.ExperienceID,
		&value.ConsumerKind, &value.ConsumerID, &value.Outcome, &value.Note,
		&value.Version, &value.CreatedBy, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return ExperienceReference{}, err
	}
	return value, nil
}
