package insights

import (
	"context"
	"database/sql"
	"errors"
	"github.com/shikanon/cookies/internal/platform/contract"
)

func (r MySQLRepository) CreateMiyunCrawlJobIdempotent(ctx context.Context, value MiyunCrawlJob) (MiyunCrawlJob, bool, error) {
	created, err := r.CreateMiyunCrawlJob(ctx, value)
	if err == nil {
		return created, false, nil
	}
	if !errors.Is(err, ErrInvalidState) {
		return MiyunCrawlJob{}, false, err
	}
	existing, getErr := scanMiyunCrawlJob(r.DB.QueryRowContext(ctx, miyunCrawlJobSelect+`
		WHERE organization_id = ? AND project_id = ? AND idempotency_key = ?`, value.OrganizationID, value.ProjectID, value.IdempotencyKey))
	if getErr != nil {
		return MiyunCrawlJob{}, false, getErr
	}
	if existing.RequestHash != value.RequestHash {
		return MiyunCrawlJob{}, false, ErrIdempotencyConflict
	}
	return existing, true, nil
}

func (r MySQLRepository) ListMiyunCrawlJobs(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, limit int) ([]MiyunCrawlJob, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.DB.QueryContext(ctx, miyunCrawlJobSelect+`
		WHERE organization_id = ? AND project_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, organizationID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]MiyunCrawlJob, 0)
	for rows.Next() {
		value, scanErr := scanMiyunCrawlJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r MySQLRepository) UpdateMiyunCrawlJob(ctx context.Context, value MiyunCrawlJob, expectedVersion int64) (MiyunCrawlJob, error) {
	value.Version = expectedVersion
	if expectedVersion < 1 || value.Validate() != nil {
		return MiyunCrawlJob{}, ErrInvalidRequest
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE insight_miyun_crawl_jobs SET
		status = ?, completed_pages = ?, discovered_count = ?, deduplicated_count = ?, downloaded_count = ?, failed_count = ?,
		cooldown_until = ?, last_error_kind = ?, last_error_code = ?, version = version + 1, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ?`,
		value.Status, value.CompletedPages, value.DiscoveredCount, value.DeduplicatedCount, value.DownloadedCount, value.FailedCount,
		value.CooldownUntil, nullableString(value.LastErrorKind), nullableString(value.LastErrorCode), value.UpdatedAt,
		value.OrganizationID, value.ProjectID, value.ID, expectedVersion)
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	if affected != 1 {
		return MiyunCrawlJob{}, ErrVersionConflict
	}
	value.Version = expectedVersion + 1
	return value, nil
}

func (r MySQLRepository) UpdateMiyunCrawlJobAndConnection(ctx context.Context, job MiyunCrawlJob, jobExpected int64, connection MiyunConnection, connectionExpected int64) (MiyunCrawlJob, MiyunConnection, error) {
	job.Version, connection.Version = jobExpected, connectionExpected
	if jobExpected < 1 || connectionExpected < 1 || job.Validate() != nil || connection.Validate() != nil ||
		job.OrganizationID != connection.OrganizationID || job.ProjectID != connection.ProjectID || job.ConnectionID != connection.ID {
		return MiyunCrawlJob{}, MiyunConnection{}, ErrInvalidRequest
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return MiyunCrawlJob{}, MiyunConnection{}, err
	}
	defer tx.Rollback()
	jobResult, err := tx.ExecContext(ctx, `UPDATE insight_miyun_crawl_jobs SET
		status = ?, completed_pages = ?, discovered_count = ?, deduplicated_count = ?, downloaded_count = ?, failed_count = ?,
		cooldown_until = ?, last_error_kind = ?, last_error_code = ?, version = version + 1, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ?`,
		job.Status, job.CompletedPages, job.DiscoveredCount, job.DeduplicatedCount, job.DownloadedCount, job.FailedCount,
		job.CooldownUntil, nullableString(job.LastErrorKind), nullableString(job.LastErrorCode), job.UpdatedAt,
		job.OrganizationID, job.ProjectID, job.ID, jobExpected)
	if err != nil {
		return MiyunCrawlJob{}, MiyunConnection{}, err
	}
	connectionResult, err := tx.ExecContext(ctx, `UPDATE insight_miyun_connections SET
		status = ?, session_ciphertext = ?, session_key_version = ?, session_expires_at = ?,
		last_verified_at = ?, last_successful_request_at = ?, cooldown_until = ?, last_error_kind = ?, last_error_code = ?, last_error_at = ?,
		version = version + 1, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ?`,
		connection.Status, connection.SessionCiphertext, connection.SessionKeyVersion, connection.SessionExpiresAt,
		connection.LastVerifiedAt, connection.LastSuccessfulRequestAt, connection.CooldownUntil, nullableString(connection.LastErrorKind),
		nullableString(connection.LastErrorCode), connection.LastErrorAt, connection.UpdatedAt,
		connection.OrganizationID, connection.ProjectID, connection.ID, connectionExpected)
	if err != nil {
		return MiyunCrawlJob{}, MiyunConnection{}, err
	}
	jobAffected, jobRowsErr := jobResult.RowsAffected()
	connectionAffected, connectionRowsErr := connectionResult.RowsAffected()
	if jobRowsErr != nil || connectionRowsErr != nil {
		return MiyunCrawlJob{}, MiyunConnection{}, errors.Join(jobRowsErr, connectionRowsErr)
	}
	if jobAffected != 1 || connectionAffected != 1 {
		return MiyunCrawlJob{}, MiyunConnection{}, ErrVersionConflict
	}
	if err := tx.Commit(); err != nil {
		return MiyunCrawlJob{}, MiyunConnection{}, err
	}
	job.Version, connection.Version = jobExpected+1, connectionExpected+1
	return job, connection, nil
}

func (r MySQLRepository) ApplyMiyunCrawlPage(ctx context.Context, job MiyunCrawlJob, sourcePage int64, records []MiyunCrawlPageRecord, finished bool) (MiyunCrawlJob, error) {
	if sourcePage != job.CompletedPages+1 || sourcePage < 1 {
		return MiyunCrawlJob{}, ErrInvalidState
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	defer tx.Rollback()
	var storedVersion int64
	var storedCompleted int64
	if err := tx.QueryRowContext(ctx, `SELECT version, completed_pages FROM insight_miyun_crawl_jobs
		WHERE organization_id = ? AND project_id = ? AND id = ? FOR UPDATE`, job.OrganizationID, job.ProjectID, job.ID).Scan(&storedVersion, &storedCompleted); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MiyunCrawlJob{}, ErrNotFound
		}
		return MiyunCrawlJob{}, err
	}
	if storedCompleted >= sourcePage {
		existing, getErr := scanMiyunCrawlJob(tx.QueryRowContext(ctx, miyunCrawlJobSelect+`
			WHERE organization_id = ? AND project_id = ? AND id = ?`, job.OrganizationID, job.ProjectID, job.ID))
		return existing, getErr
	}
	if storedVersion != job.Version || storedCompleted != sourcePage-1 {
		return MiyunCrawlJob{}, ErrVersionConflict
	}
	var discovered, deduplicated int64
	for _, record := range records {
		if record.Material.OrganizationID != job.OrganizationID || record.Material.ProjectID != job.ProjectID ||
			record.Material.FirstSeenCrawlJobID != job.ID || record.Snapshot.CrawlJobID != job.ID ||
			record.Snapshot.SourcePage != sourcePage || record.Snapshot.MaterialID != record.Material.ID {
			return MiyunCrawlJob{}, ErrInvalidRequest
		}
		if err := record.Material.Validate(); err != nil {
			return MiyunCrawlJob{}, err
		}
		if err := record.Snapshot.Validate(); err != nil {
			return MiyunCrawlJob{}, err
		}
		result, insertErr := tx.ExecContext(ctx, `INSERT IGNORE INTO insight_miyun_materials (
			id, organization_id, project_id, miyun_material_id, first_seen_crawl_job_id, import_method,
			manual_idempotency_key, manual_request_hash, resource_id, resource_url_ciphertext, resource_url_key_version, resource_expected_size,
			source_ref, source_ref_status, title, selection_status, import_status, decision_note,
			version, created_by, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?)`,
			record.Material.ID, record.Material.OrganizationID, record.Material.ProjectID, record.Material.MiyunMaterialID,
			record.Material.FirstSeenCrawlJobID, record.Material.ImportMethod, record.Material.ResourceID,
			record.Material.ResourceURLCiphertext, record.Material.ResourceURLKeyVersion, record.Material.ResourceExpectedSize, record.Material.SourceRef,
			record.Material.SourceRefStatus, record.Material.Title, record.Material.SelectionStatus, record.Material.ImportStatus,
			record.Material.Version, record.Material.CreatedBy, record.Material.CreatedAt, record.Material.UpdatedAt)
		if insertErr != nil {
			return MiyunCrawlJob{}, insertErr
		}
		inserted, _ := result.RowsAffected()
		if inserted == 1 {
			discovered++
		} else {
			deduplicated++
			if _, err := tx.ExecContext(ctx, `UPDATE insight_miyun_materials SET
				resource_id = ?, resource_url_ciphertext = ?, resource_url_key_version = ?, resource_expected_size = ?, title = ?, updated_at = ?
				WHERE organization_id = ? AND project_id = ? AND miyun_material_id = ?`,
				record.Material.ResourceID, record.Material.ResourceURLCiphertext, record.Material.ResourceURLKeyVersion, record.Material.ResourceExpectedSize,
				record.Material.Title, record.Material.UpdatedAt, record.Material.OrganizationID,
				record.Material.ProjectID, record.Material.MiyunMaterialID); err != nil {
				return MiyunCrawlJob{}, err
			}
		}
		var materialID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM insight_miyun_materials
			WHERE organization_id = ? AND project_id = ? AND miyun_material_id = ?`, record.Material.OrganizationID,
			record.Material.ProjectID, record.Material.MiyunMaterialID).Scan(&materialID); err != nil {
			return MiyunCrawlJob{}, err
		}
		snapshot := record.Snapshot
		snapshot.MaterialID = materialID
		var raw any
		if len(snapshot.SanitizedRaw) > 0 {
			raw = snapshot.SanitizedRaw
		}
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO insight_miyun_material_snapshots (
			id, organization_id, project_id, material_id, crawl_job_id, source_page, import_method, schema_version,
			captured_at, first_published_at, last_published_at, delivery_days, cumulative_impressions,
			cumulative_impressions_raw, related_ads, related_creators, related_creators_raw, related_creators_known,
			material_score, views, likes, comments, shares, saves, sanitized_raw, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			snapshot.ID, snapshot.OrganizationID, snapshot.ProjectID, snapshot.MaterialID, snapshot.CrawlJobID,
			snapshot.SourcePage, snapshot.ImportMethod, snapshot.SchemaVersion, snapshot.CapturedAt, snapshot.FirstPublishedAt,
			snapshot.LastPublishedAt, snapshot.DeliveryDays, snapshot.CumulativeImpressions, snapshot.CumulativeImpressionsRaw,
			snapshot.RelatedAds, snapshot.RelatedCreators, snapshot.RelatedCreatorsRaw, snapshot.RelatedCreatorsKnown,
			snapshot.MaterialScore, snapshot.Views, snapshot.Likes, snapshot.Comments, snapshot.Shares, snapshot.Saves, raw, snapshot.CreatedAt); err != nil {
			return MiyunCrawlJob{}, err
		}
	}
	status := MiyunCrawlJobRunning
	if finished {
		status = MiyunCrawlJobSucceeded
	}
	result, err := tx.ExecContext(ctx, `UPDATE insight_miyun_crawl_jobs SET status = CASE
		WHEN ? = 'succeeded' AND EXISTS (
			SELECT 1 FROM insight_miyun_materials m
			WHERE m.organization_id = insight_miyun_crawl_jobs.organization_id
			  AND m.project_id = insight_miyun_crawl_jobs.project_id
			  AND m.first_seen_crawl_job_id = insight_miyun_crawl_jobs.id
			  AND m.import_status = 'failed'
		) THEN 'partial' ELSE ? END, completed_pages = ?,
		discovered_count = discovered_count + ?, deduplicated_count = deduplicated_count + ?,
		version = version + 1, updated_at = ? WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ?`,
		status, status, sourcePage, discovered, deduplicated, job.UpdatedAt, job.OrganizationID, job.ProjectID, job.ID, storedVersion)
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return MiyunCrawlJob{}, ErrVersionConflict
	}
	if err := tx.Commit(); err != nil {
		return MiyunCrawlJob{}, err
	}
	return r.GetMiyunCrawlJob(ctx, job.OrganizationID, job.ProjectID, job.ID)
}

func (r MySQLRepository) ListMiyunMaterials(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, options MiyunMaterialListOptions) (MiyunMaterialListPage, error) {
	where := ` WHERE organization_id = ? AND project_id = ?`
	args := []any{organizationID, projectID}
	if options.CrawlJobID != "" {
		where += ` AND EXISTS (
			SELECT 1 FROM insight_miyun_material_snapshots snapshot
			WHERE snapshot.organization_id = insight_miyun_materials.organization_id
			  AND snapshot.project_id = insight_miyun_materials.project_id
			  AND snapshot.material_id = insight_miyun_materials.id
			  AND snapshot.crawl_job_id = ?
		)`
		args = append(args, options.CrawlJobID)
	}
	if options.Search != "" {
		where += ` AND (title LIKE ? OR miyun_material_id LIKE ?)`
		pattern := "%" + options.Search + "%"
		args = append(args, pattern, pattern)
	}
	if options.HandoffEligible {
		where += ` AND selection_status = 'confirmed' AND import_status IN ('imported', 'deduplicated')`
	}
	var total int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM insight_miyun_materials`+where, args...).Scan(&total); err != nil {
		return MiyunMaterialListPage{}, err
	}
	query := miyunMaterialSelect + where
	if field := miyunMaterialSortField(options.Sort); field != "" {
		query += ` ORDER BY (SELECT ` + field + ` FROM insight_miyun_material_snapshots sort_snapshot
			WHERE sort_snapshot.organization_id = insight_miyun_materials.organization_id
			  AND sort_snapshot.project_id = insight_miyun_materials.project_id
			  AND sort_snapshot.material_id = insight_miyun_materials.id`
		if options.CrawlJobID != "" {
			query += ` AND sort_snapshot.crawl_job_id = ?`
			args = append(args, options.CrawlJobID)
		}
		query += ` ORDER BY sort_snapshot.captured_at DESC, sort_snapshot.id DESC LIMIT 1) DESC, updated_at DESC, id DESC`
	} else {
		query += ` ORDER BY updated_at DESC, id DESC`
	}
	query += ` LIMIT ? OFFSET ?`
	args = append(args, options.Limit, options.Offset)
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return MiyunMaterialListPage{}, err
	}
	defer rows.Close()
	values := make([]MiyunMaterial, 0)
	for rows.Next() {
		value, scanErr := scanMiyunMaterial(rows)
		if scanErr != nil {
			return MiyunMaterialListPage{}, scanErr
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return MiyunMaterialListPage{}, err
	}
	return MiyunMaterialListPage{Items: values, Total: total, Limit: options.Limit, Offset: options.Offset}, nil
}

func miyunMaterialSortField(value string) string {
	switch value {
	case "delivery_days", "cumulative_impressions", "related_ads", "material_score":
		return value
	case "related_creators":
		return `CASE WHEN related_creators_known THEN related_creators ELSE NULL END`
	}
	return ""
}

func (r MySQLRepository) DecideMiyunMaterial(ctx context.Context, value MiyunMaterial, expectedVersion int64) (MiyunMaterial, error) {
	value.Version = expectedVersion
	if expectedVersion < 1 || value.Validate() != nil {
		return MiyunMaterial{}, ErrInvalidRequest
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE insight_miyun_materials SET selection_status = ?, import_status = ?,
		decision_by = ?, decision_at = ?, decision_note = ?, version = version + 1, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ? AND selection_status = 'discovered' AND version = ?`,
		value.SelectionStatus, value.ImportStatus, value.DecisionBy, value.DecisionAt, value.DecisionNote, value.UpdatedAt,
		value.OrganizationID, value.ProjectID, value.ID, expectedVersion)
	if err != nil {
		return MiyunMaterial{}, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return MiyunMaterial{}, ErrVersionConflict
	}
	value.Version = expectedVersion + 1
	return value, nil
}

func (r MySQLRepository) MarkMiyunMaterialImporting(ctx context.Context, value MiyunMaterial, expectedVersion int64, _ string) (MiyunMaterial, error) {
	if value.SelectionStatus != MiyunMaterialConfirmed {
		return MiyunMaterial{}, ErrInvalidState
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE insight_miyun_materials SET import_status = 'downloading',
		last_import_error_kind = NULL, last_import_error_code = NULL, version = version + 1, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ? AND selection_status = 'confirmed'
		AND import_status IN ('pending', 'failed') AND version = ?`, value.UpdatedAt, value.OrganizationID, value.ProjectID, value.ID, expectedVersion)
	if err != nil {
		return MiyunMaterial{}, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return MiyunMaterial{}, ErrVersionConflict
	}
	value.ImportStatus, value.Version = MiyunMaterialImportDownloading, expectedVersion+1
	return value, nil
}

func (r MySQLRepository) CompleteMiyunMaterialImport(ctx context.Context, completion MiyunMaterialImportCompletion) (MiyunMaterial, error) {
	value := completion.Material
	if value.ImportStatus != MiyunMaterialImportDownloading || completion.ExpectedVersion != value.Version || completion.Result.AssetRef.Validate() != nil ||
		completion.InsightAsset.SourceKind != AssetSourceMiyun || completion.InsightAsset.PlatformAssetID != string(completion.Result.AssetRef.AssetID) ||
		completion.InsightAsset.PlatformAssetVersion != completion.Result.AssetRef.Version {
		return MiyunMaterial{}, ErrInvalidRequest
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return MiyunMaterial{}, err
	}
	defer tx.Rollback()
	if err := insertMiyunInsightAsset(ctx, tx, completion.InsightAsset); err != nil {
		if !isDuplicateKey(err) {
			return MiyunMaterial{}, err
		}
	}
	status := MiyunMaterialImportImported
	if completion.Result.Deduplicated {
		status = MiyunMaterialImportDeduplicated
	}
	result, err := tx.ExecContext(ctx, `UPDATE insight_miyun_materials SET import_status = ?, external_import_id = ?,
		platform_asset_id = ?, platform_asset_version = ?, insight_asset_id = ?, last_import_error_kind = NULL,
		last_import_error_code = NULL, version = version + 1, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ? AND selection_status = 'confirmed'
		AND import_status = 'downloading' AND version = ?`, status, completion.Result.ExternalImportID,
		completion.Result.AssetRef.AssetID, completion.Result.AssetRef.Version, completion.InsightAsset.ID, value.UpdatedAt,
		value.OrganizationID, value.ProjectID, value.ID, completion.ExpectedVersion)
	if err != nil {
		return MiyunMaterial{}, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return MiyunMaterial{}, ErrVersionConflict
	}
	if value.FirstSeenCrawlJobID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE insight_miyun_crawl_jobs SET
			downloaded_count = downloaded_count + 1,
			status = CASE WHEN status IN ('succeeded', 'partial') THEN CASE WHEN EXISTS (
				SELECT 1 FROM insight_miyun_materials m
				WHERE m.organization_id = insight_miyun_crawl_jobs.organization_id
				  AND m.project_id = insight_miyun_crawl_jobs.project_id
				  AND m.first_seen_crawl_job_id = insight_miyun_crawl_jobs.id
				  AND m.import_status = 'failed'
			) THEN 'partial' ELSE 'succeeded' END ELSE status END,
			version = version + 1, updated_at = ?
			WHERE organization_id = ? AND project_id = ? AND id = ?`, value.UpdatedAt,
			value.OrganizationID, value.ProjectID, value.FirstSeenCrawlJobID); err != nil {
			return MiyunMaterial{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return MiyunMaterial{}, err
	}
	value.ImportStatus, value.ExternalImportID = status, completion.Result.ExternalImportID
	value.PlatformAssetID, value.PlatformAssetVersion = completion.Result.AssetRef.AssetID, completion.Result.AssetRef.Version
	value.InsightAssetID, value.Version = completion.InsightAsset.ID, completion.ExpectedVersion+1
	return value, nil
}

func (r MySQLRepository) FailMiyunMaterialImport(ctx context.Context, value MiyunMaterial, expectedVersion int64, kind, code string) (MiyunMaterial, error) {
	if kind == "" || code == "" {
		return MiyunMaterial{}, ErrInvalidRequest
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return MiyunMaterial{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE insight_miyun_materials SET import_status = 'failed',
		last_import_error_kind = ?, last_import_error_code = ?, version = version + 1, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ? AND import_status = 'downloading' AND version = ?`,
		kind, code, value.UpdatedAt, value.OrganizationID, value.ProjectID, value.ID, expectedVersion)
	if err != nil {
		return MiyunMaterial{}, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return MiyunMaterial{}, ErrVersionConflict
	}
	if value.FirstSeenCrawlJobID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE insight_miyun_crawl_jobs SET
			status = CASE WHEN status IN ('succeeded', 'partial') THEN 'partial' ELSE status END,
			failed_count = failed_count + 1, version = version + 1, updated_at = ?
			WHERE organization_id = ? AND project_id = ? AND id = ?`,
			value.UpdatedAt, value.OrganizationID, value.ProjectID, value.FirstSeenCrawlJobID); err != nil {
			return MiyunMaterial{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return MiyunMaterial{}, err
	}
	value.ImportStatus, value.LastImportErrorKind, value.LastImportErrorCode, value.Version = MiyunMaterialImportFailed, kind, code, expectedVersion+1
	return value, nil
}
