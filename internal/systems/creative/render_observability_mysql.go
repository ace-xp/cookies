package creative

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

type renderObservabilityExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func ensureInitialRenderObservability(ctx context.Context, executor renderObservabilityExecer, source ProductionRunSourceKind, organizationID contract.OrganizationID, projectID contract.ProjectID, jobID string, createdAt time.Time) error {
	usageQuery, eventQuery, err := initialRenderObservabilityQueries(source)
	if err != nil {
		return err
	}
	reason := renderOwnerLabel(source) + " actual cost is not metered."
	if _, err = executor.ExecContext(ctx, usageQuery, jobID, organizationID, projectID, reason, createdAt); err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, eventQuery, jobID, organizationID, projectID, renderOwnerLabel(source)+" queued.", createdAt)
	return err
}

func appendRenderLifecycleEvent(ctx context.Context, executor renderObservabilityExecer, source ProductionRunSourceKind, organizationID contract.OrganizationID, projectID contract.ProjectID, jobID, status, errorCode string, occurredAt time.Time) error {
	eventQuery, err := appendRenderEventQuery(source)
	if err != nil {
		return err
	}
	stage := safeRenderToken(status, 64)
	if stage == "" {
		return fmt.Errorf("creative render lifecycle stage is invalid")
	}
	code := safeRenderToken(errorCode, 128)
	_, err = executor.ExecContext(ctx, eventQuery,
		jobID, organizationID, projectID, stage, renderLifecycleMessage(source, stage), sql.NullString{String: code, Valid: code != ""}, occurredAt,
		jobID, organizationID, projectID)
	return err
}

func (r MySQLRepository) loadRenderObservability(ctx context.Context, source ProductionRunSourceKind, organizationID contract.OrganizationID, projectID contract.ProjectID, jobID string) (*RenderUsage, []RenderEvent, error) {
	usageQuery, eventQuery, err := loadRenderObservabilityQueries(source)
	if err != nil {
		return nil, nil, err
	}
	var usage RenderUsage
	var amount sql.NullInt64
	var reason sql.NullString
	err = r.DB.QueryRowContext(ctx, usageQuery, jobID, organizationID, projectID).
		Scan(&usage.Currency, &amount, &reason, &usage.MeasuredAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	var usageResult *RenderUsage
	if err == nil {
		if amount.Valid {
			usage.ActualCostMinor = &amount.Int64
		}
		if reason.Valid {
			usage.UnavailableReason = &reason.String
		}
		usageResult = &usage
	}
	rows, err := r.DB.QueryContext(ctx, eventQuery, jobID, organizationID, projectID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	events := []RenderEvent{}
	for rows.Next() {
		var event RenderEvent
		if err := rows.Scan(&event.Ordinal, &event.Stage, &event.SafeMessage, &event.ErrorCode, &event.OccurredAt); err != nil {
			return nil, nil, err
		}
		events = append(events, event)
	}
	return usageResult, events, rows.Err()
}

// RecordRenderUsage stores an owner-reported actual cost or an explicit
// unavailable fact. Production Center only reads this record and never writes it.
func (r MySQLRepository) RecordRenderUsage(ctx context.Context, source ProductionRunSourceKind, organizationID contract.OrganizationID, projectID contract.ProjectID, jobID string, usage RenderUsage) error {
	if err := usage.Validate(); err != nil {
		return err
	}
	usageQuery, err := updateRenderUsageQuery(source)
	if err != nil {
		return err
	}
	result, err := r.DB.ExecContext(ctx, usageQuery, usage.Currency,
		sql.NullInt64{Int64: valueOrZero(usage.ActualCostMinor), Valid: usage.ActualCostMinor != nil},
		sql.NullString{String: stringOrEmpty(usage.UnavailableReason), Valid: usage.UnavailableReason != nil}, usage.MeasuredAt,
		jobID, organizationID, projectID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return ErrNotFound
	}
	return nil
}

func initialRenderObservabilityQueries(source ProductionRunSourceKind) (string, string, error) {
	const usageSuffix = ` (render_job_id,organization_id,project_id,currency,actual_cost_minor,unavailable_reason,measured_at) VALUES (?,?,?,'CNY',NULL,?,?)`
	const eventSuffix = ` (render_job_id,organization_id,project_id,ordinal,stage,safe_message,error_code,occurred_at) VALUES (?,?,?,1,'queued',?,NULL,?)`
	switch source {
	case ProductionSourceCreativeRender:
		return `INSERT IGNORE INTO creative_render_job_usage` + usageSuffix, `INSERT IGNORE INTO creative_render_job_events` + eventSuffix, nil
	case ProductionSourceEditingRender:
		return `INSERT IGNORE INTO creative_edit_render_job_usage` + usageSuffix, `INSERT IGNORE INTO creative_edit_render_job_events` + eventSuffix, nil
	default:
		return "", "", fmt.Errorf("unsupported creative render owner %q", source)
	}
}

func appendRenderEventQuery(source ProductionRunSourceKind) (string, error) {
	const creativeQuery = `INSERT IGNORE INTO creative_render_job_events (render_job_id,organization_id,project_id,ordinal,stage,safe_message,error_code,occurred_at) SELECT ?,?,?,COALESCE(MAX(ordinal),0)+1,?,?,?,? FROM creative_render_job_events WHERE render_job_id=? AND organization_id=? AND project_id=?`
	const editingQuery = `INSERT IGNORE INTO creative_edit_render_job_events (render_job_id,organization_id,project_id,ordinal,stage,safe_message,error_code,occurred_at) SELECT ?,?,?,COALESCE(MAX(ordinal),0)+1,?,?,?,? FROM creative_edit_render_job_events WHERE render_job_id=? AND organization_id=? AND project_id=?`
	if source == ProductionSourceCreativeRender {
		return creativeQuery, nil
	}
	if source == ProductionSourceEditingRender {
		return editingQuery, nil
	}
	return "", fmt.Errorf("unsupported creative render owner %q", source)
}

func loadRenderObservabilityQueries(source ProductionRunSourceKind) (string, string, error) {
	const usageSuffix = ` WHERE render_job_id=? AND organization_id=? AND project_id=?`
	const eventSuffix = ` WHERE render_job_id=? AND organization_id=? AND project_id=? ORDER BY ordinal`
	if source == ProductionSourceCreativeRender {
		return `SELECT currency,actual_cost_minor,unavailable_reason,measured_at FROM creative_render_job_usage` + usageSuffix, `SELECT ordinal,stage,safe_message,COALESCE(error_code,''),occurred_at FROM creative_render_job_events` + eventSuffix, nil
	}
	if source == ProductionSourceEditingRender {
		return `SELECT currency,actual_cost_minor,unavailable_reason,measured_at FROM creative_edit_render_job_usage` + usageSuffix, `SELECT ordinal,stage,safe_message,COALESCE(error_code,''),occurred_at FROM creative_edit_render_job_events` + eventSuffix, nil
	}
	return "", "", fmt.Errorf("unsupported creative render owner %q", source)
}

func updateRenderUsageQuery(source ProductionRunSourceKind) (string, error) {
	const suffix = ` SET currency=?,actual_cost_minor=?,unavailable_reason=?,measured_at=? WHERE render_job_id=? AND organization_id=? AND project_id=?`
	if source == ProductionSourceCreativeRender {
		return `UPDATE creative_render_job_usage` + suffix, nil
	}
	if source == ProductionSourceEditingRender {
		return `UPDATE creative_edit_render_job_usage` + suffix, nil
	}
	return "", fmt.Errorf("unsupported creative render owner %q", source)
}

func renderOwnerLabel(source ProductionRunSourceKind) string {
	if source == ProductionSourceEditingRender {
		return "Editing render"
	}
	return "Creative render"
}

func renderLifecycleMessage(source ProductionRunSourceKind, stage string) string {
	owner := renderOwnerLabel(source)
	switch stage {
	case "running":
		return owner + " started."
	case "succeeded":
		return owner + " completed."
	case "failed":
		return owner + " failed."
	case "cancelled":
		return owner + " was cancelled."
	default:
		return owner + " state changed."
	}
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
