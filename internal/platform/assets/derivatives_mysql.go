package assets

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

func (r MySQLRepository) EnsureDerivative(ctx context.Context, value AssetDerivative, job ProcessingJob) (AssetDerivative, ProcessingJob, bool, error) {
	db, err := r.db()
	if err != nil {
		return AssetDerivative{}, ProcessingJob{}, false, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return AssetDerivative{}, ProcessingJob{}, false, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO asset_derivatives (id,organization_id,project_id,source_asset_id,source_asset_version,profile,status,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`, value.ID, value.OrganizationID, value.ProjectID, value.Source.AssetID, value.Source.Version, value.Profile, value.Status, value.CreatedAt, value.UpdatedAt)
	if err != nil {
		if !isDuplicate(err) {
			return AssetDerivative{}, ProcessingJob{}, false, err
		}
		stored, storedJob, getErr := getDerivativeByKey(ctx, tx, value.OrganizationID, value.ProjectID, value.Source, value.Profile)
		return stored, storedJob, true, getErr
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO asset_processing_jobs (id,organization_id,project_id,derivative_id,status,attempt,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)`, job.ID, job.OrganizationID, job.ProjectID, job.DerivativeID, job.Status, job.Attempt, job.CreatedAt, job.UpdatedAt); err != nil {
		return AssetDerivative{}, ProcessingJob{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return AssetDerivative{}, ProcessingJob{}, false, err
	}
	return value, job, false, nil
}

func (r MySQLRepository) FailDerivativeScheduling(ctx context.Context, id, code string, now time.Time) (AssetDerivative, ProcessingJob, error) {
	db, err := r.db()
	if err != nil {
		return AssetDerivative{}, ProcessingJob{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return AssetDerivative{}, ProcessingJob{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE asset_derivatives SET status='failed',error_code=?,updated_at=? WHERE id=? AND status='queued'`, code, now, id); err != nil {
		return AssetDerivative{}, ProcessingJob{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE asset_processing_jobs SET status='failed',error_code=?,updated_at=? WHERE derivative_id=? AND status='queued'`, code, now, id); err != nil {
		return AssetDerivative{}, ProcessingJob{}, err
	}
	value, job, err := getDerivativeByID(ctx, tx, id)
	if err != nil {
		return value, job, err
	}
	if err = tx.Commit(); err != nil {
		return AssetDerivative{}, ProcessingJob{}, err
	}
	return value, job, nil
}

func (r MySQLRepository) RetryDerivative(ctx context.Context, org contract.OrganizationID, project contract.ProjectID, id, newJobID string, now time.Time) (AssetDerivative, ProcessingJob, error) {
	db, err := r.db()
	if err != nil {
		return AssetDerivative{}, ProcessingJob{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return AssetDerivative{}, ProcessingJob{}, err
	}
	defer tx.Rollback()
	value, prior, err := getDerivativeByID(ctx, tx, id)
	if err != nil {
		return value, prior, err
	}
	if value.OrganizationID != org || value.ProjectID != project || value.Status != DerivativeFailed {
		return AssetDerivative{}, ProcessingJob{}, ErrInvalidState
	}
	if _, err = tx.ExecContext(ctx, `UPDATE asset_derivatives SET status='queued',error_code=NULL,updated_at=? WHERE id=? AND status='failed'`, now, id); err != nil {
		return AssetDerivative{}, ProcessingJob{}, err
	}
	job := ProcessingJob{ID: newJobID, OrganizationID: org, ProjectID: project, DerivativeID: id, Status: ProcessingQueued, Attempt: prior.Attempt + 1, CreatedAt: now, UpdatedAt: now}
	if _, err = tx.ExecContext(ctx, `INSERT INTO asset_processing_jobs (id,organization_id,project_id,derivative_id,status,attempt,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)`, job.ID, job.OrganizationID, job.ProjectID, job.DerivativeID, job.Status, job.Attempt, job.CreatedAt, job.UpdatedAt); err != nil {
		return AssetDerivative{}, ProcessingJob{}, err
	}
	if err = tx.Commit(); err != nil {
		return AssetDerivative{}, ProcessingJob{}, err
	}
	value.Status, value.ErrorCode, value.UpdatedAt = DerivativeQueued, "", now
	return value, job, nil
}

// GetDerivative / GetDerivativeByID 是把已有的两个内部取数函数露出去。
// 之前它们只在事务里被自己人用，外面既查不到派生物状态也拿不到产物——
// 这套脚手架建好之后一直没跑起来，缺的就是这个口。
func (r MySQLRepository) GetDerivative(ctx context.Context, org contract.OrganizationID, project contract.ProjectID, source contract.AssetVersionRef, profile DerivativeProfile) (AssetDerivative, error) {
	db, err := r.db()
	if err != nil {
		return AssetDerivative{}, err
	}
	value, _, err := getDerivativeByKey(ctx, db, org, project, source, profile)
	return value, err
}

func (r MySQLRepository) GetDerivativeByID(ctx context.Context, id string) (AssetDerivative, error) {
	db, err := r.db()
	if err != nil {
		return AssetDerivative{}, err
	}
	value, _, err := getDerivativeByID(ctx, db, id)
	return value, err
}

// CompleteDerivative 把产物写回并置为 ready。
//
// 只从 queued/running 转过来：worker 重复投递时，第二次的 UPDATE 影响 0 行，
// 而不是把一条已经 ready 的记录指向另一张图。任务行跟着一起结，否则队列里
// 那条永远停在 running，重试通道会以为它还在跑。
func (r MySQLRepository) CompleteDerivative(ctx context.Context, id string, output contract.AssetVersionRef, now time.Time) (AssetDerivative, error) {
	db, err := r.db()
	if err != nil {
		return AssetDerivative{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return AssetDerivative{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx,
		`UPDATE asset_derivatives SET status='ready',output_asset_id=?,output_asset_version=?,error_code=NULL,updated_at=?
		 WHERE id=? AND status IN ('queued','running')`,
		string(output.AssetID), output.Version, now, id); err != nil {
		return AssetDerivative{}, err
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE asset_processing_jobs SET status='succeeded',error_code=NULL,updated_at=?
		 WHERE derivative_id=? AND status IN ('queued','running')`, now, id); err != nil {
		return AssetDerivative{}, err
	}
	value, _, err := getDerivativeByID(ctx, tx, id)
	if err != nil {
		return value, err
	}
	if err = tx.Commit(); err != nil {
		return AssetDerivative{}, err
	}
	return value, nil
}

type derivativeQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getDerivativeByKey(ctx context.Context, q derivativeQuerier, org contract.OrganizationID, project contract.ProjectID, ref contract.AssetVersionRef, profile DerivativeProfile) (AssetDerivative, ProcessingJob, error) {
	return scanDerivative(q.QueryRowContext(ctx, derivativeSelect+` WHERE d.organization_id=? AND d.project_id=? AND d.source_asset_id=? AND d.source_asset_version=? AND d.profile=? ORDER BY j.attempt DESC LIMIT 1`, org, project, ref.AssetID, ref.Version, profile))
}
func getDerivativeByID(ctx context.Context, q derivativeQuerier, id string) (AssetDerivative, ProcessingJob, error) {
	return scanDerivative(q.QueryRowContext(ctx, derivativeSelect+` WHERE d.id=? ORDER BY j.attempt DESC LIMIT 1`, id))
}

const derivativeSelect = `SELECT d.id,d.organization_id,d.project_id,d.source_asset_id,d.source_asset_version,d.profile,d.status,d.output_asset_id,d.output_asset_version,d.error_code,d.created_at,d.updated_at,j.id,j.status,j.attempt,j.error_code,j.created_at,j.updated_at FROM asset_derivatives d JOIN asset_processing_jobs j ON j.derivative_id=d.id`

func scanDerivative(row *sql.Row) (AssetDerivative, ProcessingJob, error) {
	var d AssetDerivative
	var j ProcessingJob
	var outID sql.NullString
	var outVersion sql.NullInt64
	var dCode, jCode sql.NullString
	err := row.Scan(&d.ID, &d.OrganizationID, &d.ProjectID, &d.Source.AssetID, &d.Source.Version, &d.Profile, &d.Status, &outID, &outVersion, &dCode, &d.CreatedAt, &d.UpdatedAt, &j.ID, &j.Status, &j.Attempt, &jCode, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return d, j, ErrNotFound
	}
	if err != nil {
		return d, j, err
	}
	j.OrganizationID, j.ProjectID, j.DerivativeID = d.OrganizationID, d.ProjectID, d.ID
	d.ErrorCode = dCode.String
	j.ErrorCode = jCode.String
	if outID.Valid && outVersion.Valid {
		ref := contract.AssetVersionRef{AssetID: contract.AssetID(outID.String), Version: outVersion.Int64}
		d.Output = &ref
	}
	return d, j, nil
}
