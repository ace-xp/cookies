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

func (r MySQLRepository) CreateDecision(ctx context.Context, value DeliveryDecision) (DeliveryDecision, error) {
	if err := value.Validate(); err != nil {
		return DeliveryDecision{}, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return DeliveryDecision{}, err
	}
	_, err = r.DB.ExecContext(ctx, `INSERT INTO delivery_decisions (
		organization_id,project_id,decision_id,plan_id,plan_version,schema_version,policy_version,canonical_hash,decision_json,created_by,created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, value.OrganizationID, value.ProjectID, value.ID, value.Inputs.PlanID, value.Inputs.PlanVersion, value.SchemaVersion, value.PolicyVersion, value.CanonicalHash, payload, value.CreatedBy, value.CreatedAt)
	if err == nil {
		return value, nil
	}
	var mysqlError *mysqlDriver.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		existing, getErr := scanDecision(r.DB.QueryRowContext(ctx, decisionByCanonicalHashQuery, value.OrganizationID, value.ProjectID, value.CanonicalHash))
		if getErr == nil {
			computed, hashErr := existing.ComputeCanonicalHash()
			if hashErr == nil && computed == value.CanonicalHash {
				return existing, nil
			}
			return DeliveryDecision{}, ErrIdempotencyConflict
		}
	}
	return DeliveryDecision{}, err
}

func (r MySQLRepository) ListDecisions(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, limit int) ([]DeliveryDecision, error) {
	rows, err := r.DB.QueryContext(ctx, decisionsByProjectQuery, organizationID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []DeliveryDecision{}
	for rows.Next() {
		value, scanErr := scanDecision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r MySQLRepository) GetDecision(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (DeliveryDecision, error) {
	value, err := scanDecision(r.DB.QueryRowContext(ctx, decisionByIDQuery, organizationID, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return DeliveryDecision{}, ErrNotFound
	}
	return value, err
}

const decisionSelect = `SELECT decision_json FROM delivery_decisions`
const decisionByCanonicalHashQuery = `SELECT decision_json FROM delivery_decisions WHERE organization_id=? AND project_id=? AND canonical_hash=?`
const decisionsByProjectQuery = `SELECT decision_json FROM delivery_decisions WHERE organization_id=? AND project_id=? ORDER BY created_at DESC,decision_id DESC LIMIT ?`
const decisionByIDQuery = `SELECT decision_json FROM delivery_decisions WHERE organization_id=? AND project_id=? AND decision_id=?`

func scanDecision(row rowScanner) (DeliveryDecision, error) {
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		return DeliveryDecision{}, err
	}
	var value DeliveryDecision
	if err := json.Unmarshal(payload, &value); err != nil {
		return DeliveryDecision{}, fmt.Errorf("decode delivery decision: %w", err)
	}
	return value, nil
}

func (r MySQLRepository) CreateDecisionSelection(ctx context.Context, value DecisionSelection, idempotencyKey, requestHash string) (DecisionSelection, bool, error) {
	if err := value.Validate(); err != nil {
		return DecisionSelection{}, false, err
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return DecisionSelection{}, false, err
	}
	defer tx.Rollback()
	var storedDecisionHash string
	if err = tx.QueryRowContext(ctx, `SELECT canonical_hash FROM delivery_decisions WHERE organization_id=? AND project_id=? AND decision_id=? FOR UPDATE`, value.OrganizationID, value.ProjectID, value.DecisionID).Scan(&storedDecisionHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DecisionSelection{}, false, ErrNotFound
		}
		return DecisionSelection{}, false, err
	}
	if storedDecisionHash != value.DecisionCanonicalHash {
		return DecisionSelection{}, false, ErrApprovalContentMismatch
	}
	var existingRequestHash string
	err = tx.QueryRowContext(ctx, `SELECT request_hash FROM delivery_decision_selections WHERE organization_id=? AND project_id=? AND idempotency_key=? FOR UPDATE`, value.OrganizationID, value.ProjectID, idempotencyKey).Scan(&existingRequestHash)
	if err == nil {
		if existingRequestHash != requestHash {
			return DecisionSelection{}, false, ErrIdempotencyConflict
		}
		existing, getErr := scanDecisionSelection(tx.QueryRowContext(ctx, decisionSelectionByIdempotencyKeyQuery, value.OrganizationID, value.ProjectID, idempotencyKey))
		return existing, true, getErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return DecisionSelection{}, false, err
	}
	var existingSelectionID string
	err = tx.QueryRowContext(ctx, `SELECT selection_id FROM delivery_decision_selections WHERE organization_id=? AND project_id=? AND decision_id=?`, value.OrganizationID, value.ProjectID, value.DecisionID).Scan(&existingSelectionID)
	if err == nil {
		return DecisionSelection{}, false, ErrIdempotencyConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return DecisionSelection{}, false, err
	}
	configurationJSON, err := json.Marshal(value.Configuration)
	if err != nil {
		return DecisionSelection{}, false, err
	}
	configuration := value.Configuration
	_, err = tx.ExecContext(ctx, `INSERT INTO delivery_platform_configurations (
		organization_id,project_id,configuration_id,version_number,schema_version,platform,profile_version,intent_id,intent_version,intent_canonical_hash,canonical_hash,hash_algorithm,configuration_json,created_by,created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE configuration_id=VALUES(configuration_id)`, value.OrganizationID, value.ProjectID, configuration.ConfigurationID, configuration.VersionNumber, configuration.SchemaVersion, configuration.Platform, configuration.ProfileVersion, configuration.Intent.IntentID, configuration.Intent.VersionNumber, configuration.Intent.CanonicalHash, configuration.CanonicalHash, configuration.HashAlgorithm, configurationJSON, value.CreatedBy, value.CreatedAt)
	if err != nil {
		return DecisionSelection{}, false, err
	}
	var storedHash string
	var storedJSON []byte
	if err = tx.QueryRowContext(ctx, `SELECT canonical_hash,configuration_json FROM delivery_platform_configurations WHERE organization_id=? AND project_id=? AND configuration_id=? AND version_number=?`, value.OrganizationID, value.ProjectID, configuration.ConfigurationID, configuration.VersionNumber).Scan(&storedHash, &storedJSON); err != nil {
		return DecisionSelection{}, false, err
	}
	if storedHash != configuration.CanonicalHash || !equalJSONDocuments(storedJSON, configurationJSON) {
		return DecisionSelection{}, false, ErrIdempotencyConflict
	}
	workflowJSON, err := json.Marshal(value.Workflow)
	if err != nil {
		return DecisionSelection{}, false, err
	}
	workflow := value.Workflow
	if workflow.RemoteWriteEnabled || workflow.Status != "ready_for_final_approval" {
		return DecisionSelection{}, false, ErrInvalidState
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO delivery_compiled_workflows (
		organization_id,project_id,workflow_id,decision_id,decision_canonical_hash,configuration_canonical_hash,schema_version,compiler_version,status,remote_write_enabled,canonical_hash,workflow_json,created_by,created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, value.OrganizationID, value.ProjectID, workflow.ID, workflow.DecisionID, workflow.DecisionCanonicalHash, workflow.ConfigurationCanonicalHash, workflow.SchemaVersion, workflow.CompilerVersion, workflow.Status, false, workflow.CanonicalHash, workflowJSON, value.CreatedBy, value.CreatedAt)
	if err != nil {
		return DecisionSelection{}, false, err
	}
	selectionJSON, err := json.Marshal(value)
	if err != nil {
		return DecisionSelection{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO delivery_decision_selections (
		organization_id,project_id,selection_id,decision_id,decision_canonical_hash,candidate_id,configuration_id,configuration_version,configuration_canonical_hash,workflow_id,workflow_canonical_hash,selection_json,idempotency_key,request_hash,created_by,created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, value.OrganizationID, value.ProjectID, value.ID, value.DecisionID, value.DecisionCanonicalHash, value.CandidateID, configuration.ConfigurationID, configuration.VersionNumber, configuration.CanonicalHash, workflow.ID, workflow.CanonicalHash, selectionJSON, idempotencyKey, requestHash, value.CreatedBy, value.CreatedAt)
	if err != nil {
		var mysqlError *mysqlDriver.MySQLError
		if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
			return DecisionSelection{}, false, ErrIdempotencyConflict
		}
		return DecisionSelection{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return DecisionSelection{}, false, err
	}
	return value, false, nil
}

func (r MySQLRepository) GetDecisionSelection(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (DecisionSelection, error) {
	value, err := scanDecisionSelection(r.DB.QueryRowContext(ctx, decisionSelectionByIDQuery, organizationID, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return DecisionSelection{}, ErrNotFound
	}
	return value, err
}

const decisionSelectionSelect = `SELECT selection_json FROM delivery_decision_selections`
const decisionSelectionByIdempotencyKeyQuery = `SELECT selection_json FROM delivery_decision_selections WHERE organization_id=? AND project_id=? AND idempotency_key=?`
const decisionSelectionByIDQuery = `SELECT selection_json FROM delivery_decision_selections WHERE organization_id=? AND project_id=? AND selection_id=?`

func scanDecisionSelection(row rowScanner) (DecisionSelection, error) {
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		return DecisionSelection{}, err
	}
	var value DecisionSelection
	if err := json.Unmarshal(payload, &value); err != nil {
		return DecisionSelection{}, fmt.Errorf("decode delivery decision selection: %w", err)
	}
	return value, nil
}
