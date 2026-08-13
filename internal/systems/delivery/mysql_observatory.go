package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/shikanon/cookies/internal/platform/contract"
)

func (r MySQLRepository) CreateObservatoryRun(ctx context.Context, value DeliveryObservatoryRun) (DeliveryObservatoryRun, bool, error) {
	if err := value.Validate(); err != nil {
		return DeliveryObservatoryRun{}, false, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return DeliveryObservatoryRun{}, false, err
	}
	_, err = r.DB.ExecContext(ctx, `INSERT INTO delivery_observatory_runs (
		organization_id,project_id,run_id,selection_id,decision_id,decision_canonical_hash,configuration_canonical_hash,workflow_id,workflow_canonical_hash,schema_version,runner_version,source,mode,data_state,status,outcome,remote_write_enabled,input_hash,canonical_hash,run_json,created_by,created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,FALSE,?,?,?,?,?)`, value.OrganizationID, value.ProjectID, value.ID, value.Binding.SelectionID, value.Binding.DecisionID, value.Binding.DecisionCanonicalHash, value.Binding.ConfigurationCanonicalHash, value.Binding.WorkflowID, value.Binding.WorkflowCanonicalHash, value.SchemaVersion, value.RunnerVersion, value.Source, value.Mode, value.DataState, value.Status, value.Outcome, value.InputHash, value.CanonicalHash, payload, value.CreatedBy, value.CreatedAt)
	if err == nil {
		return value, false, nil
	}
	var mysqlError *mysqlDriver.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		existing, getErr := scanObservatoryRun(r.DB.QueryRowContext(ctx, observatoryRunByInputHashQuery, value.OrganizationID, value.ProjectID, value.InputHash))
		if getErr != nil {
			return DeliveryObservatoryRun{}, false, getErr
		}
		if existing.CanonicalHash != value.CanonicalHash {
			return DeliveryObservatoryRun{}, false, ErrIdempotencyConflict
		}
		return existing, true, nil
	}
	return DeliveryObservatoryRun{}, false, err
}

func (r MySQLRepository) ListObservatoryRuns(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, limit int) ([]DeliveryObservatoryRun, error) {
	rows, err := r.DB.QueryContext(ctx, observatoryRunsByProjectQuery, organizationID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []DeliveryObservatoryRun{}
	for rows.Next() {
		value, scanErr := scanObservatoryRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r MySQLRepository) GetObservatoryRun(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (DeliveryObservatoryRun, error) {
	value, err := scanObservatoryRun(r.DB.QueryRowContext(ctx, observatoryRunByIDQuery, organizationID, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryObservatoryRun{}, ErrNotFound
	}
	return value, err
}

const observatoryRunSelect = `SELECT run_json FROM delivery_observatory_runs`
const observatoryRunByInputHashQuery = `SELECT run_json FROM delivery_observatory_runs WHERE organization_id=? AND project_id=? AND input_hash=?`
const observatoryRunsByProjectQuery = `SELECT run_json FROM delivery_observatory_runs WHERE organization_id=? AND project_id=? ORDER BY created_at DESC,run_id DESC LIMIT ?`
const observatoryRunByIDQuery = `SELECT run_json FROM delivery_observatory_runs WHERE organization_id=? AND project_id=? AND run_id=?`

func scanObservatoryRun(row rowScanner) (DeliveryObservatoryRun, error) {
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		return DeliveryObservatoryRun{}, err
	}
	var value DeliveryObservatoryRun
	if err := json.Unmarshal(payload, &value); err != nil {
		return DeliveryObservatoryRun{}, fmt.Errorf("decode delivery observatory run: %w", err)
	}
	return value, nil
}

func (r MySQLRepository) CreateObservatoryFeedback(ctx context.Context, value DeliveryObservatoryFeedback, idempotencyKey, requestHash string) (DeliveryObservatoryFeedback, bool, error) {
	if err := value.Validate(); err != nil {
		return DeliveryObservatoryFeedback{}, false, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return DeliveryObservatoryFeedback{}, false, err
	}
	_, err = r.DB.ExecContext(ctx, `INSERT INTO delivery_observatory_feedback (
		organization_id,project_id,feedback_id,run_id,run_canonical_hash,run_outcome,schema_version,disposition,final_configuration_canonical_hash,canonical_hash,feedback_json,idempotency_key,request_hash,created_by,created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, value.OrganizationID, value.ProjectID, value.ID, value.RunID, value.RunCanonicalHash, value.RunOutcome, value.SchemaVersion, value.Disposition, nullableString(value.FinalConfigurationCanonicalHash), value.CanonicalHash, payload, idempotencyKey, requestHash, value.CreatedBy, value.CreatedAt)
	if err == nil {
		return value, false, nil
	}
	var mysqlError *mysqlDriver.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		var existingRequestHash string
		existing, getErr := scanObservatoryFeedback(r.DB.QueryRowContext(ctx, observatoryFeedbackByIdempotencyKeyQuery, value.OrganizationID, value.ProjectID, idempotencyKey))
		if getErr != nil {
			return DeliveryObservatoryFeedback{}, false, getErr
		}
		getErr = r.DB.QueryRowContext(ctx, `SELECT request_hash FROM delivery_observatory_feedback WHERE organization_id=? AND project_id=? AND idempotency_key=?`, value.OrganizationID, value.ProjectID, idempotencyKey).Scan(&existingRequestHash)
		if getErr != nil {
			return DeliveryObservatoryFeedback{}, false, getErr
		}
		if existingRequestHash != requestHash {
			return DeliveryObservatoryFeedback{}, false, ErrIdempotencyConflict
		}
		return existing, true, nil
	}
	return DeliveryObservatoryFeedback{}, false, err
}

func (r MySQLRepository) ListObservatoryFeedback(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, runID string, limit int) ([]DeliveryObservatoryFeedback, error) {
	rows, err := r.DB.QueryContext(ctx, observatoryFeedbackByRunQuery, organizationID, projectID, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []DeliveryObservatoryFeedback{}
	for rows.Next() {
		value, scanErr := scanObservatoryFeedback(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

const observatoryFeedbackSelect = `SELECT feedback_json,run_outcome FROM delivery_observatory_feedback`
const observatoryFeedbackByIdempotencyKeyQuery = `SELECT feedback_json,run_outcome FROM delivery_observatory_feedback WHERE organization_id=? AND project_id=? AND idempotency_key=?`
const observatoryFeedbackByRunQuery = `SELECT feedback_json,run_outcome FROM delivery_observatory_feedback WHERE organization_id=? AND project_id=? AND run_id=? ORDER BY created_at DESC,feedback_id DESC LIMIT ?`

func scanObservatoryFeedback(row rowScanner) (DeliveryObservatoryFeedback, error) {
	var payload []byte
	var runOutcome string
	if err := row.Scan(&payload, &runOutcome); err != nil {
		return DeliveryObservatoryFeedback{}, err
	}
	var value DeliveryObservatoryFeedback
	if err := json.Unmarshal(payload, &value); err != nil {
		return DeliveryObservatoryFeedback{}, fmt.Errorf("decode delivery observatory feedback: %w", err)
	}
	if value.RunOutcome == "" {
		value.RunOutcome = runOutcome
	}
	return value, nil
}
