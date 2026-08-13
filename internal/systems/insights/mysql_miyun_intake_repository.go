package insights

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shikanon/cookies/internal/platform/contract"
)

func (r MySQLRepository) CreateMiyunProductProfileDraft(ctx context.Context, value MiyunProductProfile) (MiyunProductProfile, error) {
	if value.Status != MiyunProfileDraft {
		return MiyunProductProfile{}, fmt.Errorf("%w: analysis may only create a draft profile", ErrInvalidRequest)
	}
	if err := value.Validate(); err != nil {
		return MiyunProductProfile{}, err
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return MiyunProductProfile{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE insight_miyun_product_profiles
		SET status = 'superseded', version = version + 1, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND product_id = ? AND status = 'draft'`,
		value.UpdatedAt, value.OrganizationID, value.ProjectID, value.ProductID); err != nil {
		return MiyunProductProfile{}, err
	}
	if err := insertMiyunProductProfile(ctx, tx, value); err != nil {
		if isDuplicateKey(err) {
			return MiyunProductProfile{}, fmt.Errorf("%w: Miyun product profile identity already exists", ErrInvalidState)
		}
		return MiyunProductProfile{}, err
	}
	if err := tx.Commit(); err != nil {
		return MiyunProductProfile{}, err
	}
	return value, nil
}

func (r MySQLRepository) ListMiyunProductProfiles(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, limit int) ([]MiyunProductProfile, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.DB.QueryContext(ctx, miyunProductProfileSelect+`
		WHERE organization_id = ? AND project_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		organizationID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]MiyunProductProfile, 0)
	for rows.Next() {
		value, scanErr := scanMiyunProductProfile(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r MySQLRepository) ConfirmMiyunProductProfile(ctx context.Context, value MiyunProductProfile, expectedVersion int64) (MiyunProductProfile, error) {
	if value.Status != MiyunProfileConfirmed || expectedVersion < 1 {
		return MiyunProductProfile{}, ErrInvalidRequest
	}
	value.Version = expectedVersion
	if err := value.Validate(); err != nil {
		return MiyunProductProfile{}, err
	}
	keywords, materialTypes, contentTypes, _, _, fieldSources, _, err := encodeMiyunProfileJSON(value)
	if err != nil {
		return MiyunProductProfile{}, err
	}
	result, err := r.DB.ExecContext(ctx, `UPDATE insight_miyun_product_profiles SET
		product_name = ?, category_id = ?, category_name = ?, keywords = ?, material_types = ?, material_content_types = ?,
		window_start = ?, window_end = ?, field_sources = ?, status = 'confirmed', confirmed_by = ?,
		confirmed_at = ?, version = version + 1, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ? AND status = 'draft' AND version = ?`,
		value.ProductName, value.CategoryID, value.CategoryName, keywords, materialTypes, contentTypes,
		value.WindowStart.Format("2006-01-02"), value.WindowEnd.Format("2006-01-02"), fieldSources,
		value.ConfirmedBy, value.ConfirmedAt, value.UpdatedAt,
		value.OrganizationID, value.ProjectID, value.ID, expectedVersion)
	if err != nil {
		return MiyunProductProfile{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return MiyunProductProfile{}, err
	}
	if affected == 0 {
		var status MiyunProfileStatus
		var version int64
		lookupErr := r.DB.QueryRowContext(ctx, `SELECT status, version FROM insight_miyun_product_profiles
			WHERE organization_id = ? AND project_id = ? AND id = ?`, value.OrganizationID, value.ProjectID, value.ID).Scan(&status, &version)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return MiyunProductProfile{}, ErrNotFound
		}
		if lookupErr != nil {
			return MiyunProductProfile{}, lookupErr
		}
		if status != MiyunProfileDraft {
			return MiyunProductProfile{}, ErrInvalidState
		}
		return MiyunProductProfile{}, ErrVersionConflict
	}
	value.Version = expectedVersion + 1
	return value, nil
}

func (r MySQLRepository) CreateManualMiyunMaterial(ctx context.Context, record MiyunManualImportRecord) (MiyunManualImportResult, error) {
	if record.Material.ImportMethod != MiyunImportManual || record.Snapshot.ImportMethod != MiyunImportManual ||
		record.Snapshot.MaterialID != record.Material.ID || record.InsightAsset.ID != record.Material.InsightAssetID ||
		record.Snapshot.OrganizationID != record.Material.OrganizationID || record.Snapshot.ProjectID != record.Material.ProjectID ||
		record.InsightAsset.OrganizationID != record.Material.OrganizationID || record.InsightAsset.ProjectID != record.Material.ProjectID ||
		record.InsightAsset.SourceKind != AssetSourceExternal ||
		record.InsightAsset.PlatformAssetID != string(record.Material.PlatformAssetID) ||
		record.InsightAsset.PlatformAssetVersion != record.Material.PlatformAssetVersion {
		return MiyunManualImportResult{}, ErrInvalidRequest
	}
	if err := record.Material.Validate(); err != nil {
		return MiyunManualImportResult{}, err
	}
	if err := record.Snapshot.Validate(); err != nil {
		return MiyunManualImportResult{}, err
	}
	assetRequest := IndexAssetRequest{
		Title: record.InsightAsset.Title, SourceKind: record.InsightAsset.SourceKind,
		SourceRef: record.InsightAsset.SourceRef, PlatformAssetID: record.InsightAsset.PlatformAssetID,
		PlatformAssetVersion: record.InsightAsset.PlatformAssetVersion,
	}
	if err := assetRequest.validate(); err != nil {
		return MiyunManualImportResult{}, err
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return MiyunManualImportResult{}, err
	}
	defer tx.Rollback()

	existing, err := scanMiyunMaterial(tx.QueryRowContext(ctx, miyunMaterialSelect+`
		WHERE organization_id = ? AND project_id = ? AND manual_idempotency_key = ? FOR UPDATE`,
		record.Material.OrganizationID, record.Material.ProjectID, record.Material.ManualIdempotencyKey))
	if err == nil {
		if existing.ManualRequestHash != record.Material.ManualRequestHash {
			return MiyunManualImportResult{}, ErrIdempotencyConflict
		}
		snapshot, snapshotErr := scanMiyunMaterialSnapshot(tx.QueryRowContext(ctx, miyunMaterialSnapshotSelect+`
			WHERE organization_id = ? AND project_id = ? AND material_id = ? ORDER BY created_at, id LIMIT 1`,
			existing.OrganizationID, existing.ProjectID, existing.ID))
		if snapshotErr != nil {
			return MiyunManualImportResult{}, snapshotErr
		}
		asset, assetErr := scanAsset(tx.QueryRowContext(ctx, insightAssetSelect+`
			WHERE organization_id = ? AND project_id = ? AND id = ?`,
			existing.OrganizationID, existing.ProjectID, existing.InsightAssetID))
		if assetErr != nil {
			return MiyunManualImportResult{}, assetErr
		}
		return MiyunManualImportResult{Material: existing, Snapshot: snapshot, InsightAsset: asset, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return MiyunManualImportResult{}, err
	}

	if err := insertMiyunInsightAsset(ctx, tx, record.InsightAsset); err != nil {
		return MiyunManualImportResult{}, err
	}
	if err := insertManualMiyunMaterial(ctx, tx, record.Material); err != nil {
		if isDuplicateKey(err) {
			return MiyunManualImportResult{}, fmt.Errorf("%w: Miyun material or idempotency identity already exists", ErrInvalidState)
		}
		return MiyunManualImportResult{}, err
	}
	if err := insertManualMiyunSnapshot(ctx, tx, record.Snapshot); err != nil {
		return MiyunManualImportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return MiyunManualImportResult{}, err
	}
	return MiyunManualImportResult{Material: record.Material, Snapshot: record.Snapshot, InsightAsset: record.InsightAsset}, nil
}

type miyunSQLExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertMiyunProductProfile(ctx context.Context, execer miyunSQLExecer, value MiyunProductProfile) error {
	keywords, materialTypes, contentTypes, assetRefs, documentIDs, fieldSources, warnings, err := encodeMiyunProfileJSON(value)
	if err != nil {
		return err
	}
	_, err = execer.ExecContext(ctx, `INSERT INTO insight_miyun_product_profiles (
		id, organization_id, project_id, connection_id, status, product_id, product_name, brand_name, category_id, category_name,
		keywords, material_types, material_content_types, window_start, window_end, project_context_version,
		product_asset_refs, knowledge_document_ids, rule_version, model_version, analysis_method, input_hash,
		input_snapshot, field_sources, analysis_warnings, confirmed_by, confirmed_at,
		version, created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.OrganizationID, value.ProjectID, value.ConnectionID, value.Status, value.ProductID,
		value.ProductName, value.BrandName, value.CategoryID, value.CategoryName, keywords, materialTypes, contentTypes,
		value.WindowStart.Format("2006-01-02"), value.WindowEnd.Format("2006-01-02"), value.ProjectContextVersion,
		assetRefs, documentIDs, value.RuleVersion, nullableString(value.ModelVersion), value.AnalysisMethod,
		value.InputHash, value.InputSnapshot, fieldSources, warnings, nullableString(value.ConfirmedBy), value.ConfirmedAt,
		value.Version, value.CreatedBy, value.CreatedAt, value.UpdatedAt)
	return err
}

func encodeMiyunProfileJSON(value MiyunProductProfile) ([]byte, []byte, []byte, []byte, []byte, []byte, []byte, error) {
	values := []any{value.Keywords, value.MaterialTypes, value.MaterialContentTypes, value.ProductAssetRefs, value.KnowledgeDocumentIDs, value.FieldSources, value.AnalysisWarnings}
	encoded := make([][]byte, len(values))
	for index, item := range values {
		data, err := json.Marshal(item)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, nil, err
		}
		encoded[index] = data
	}
	return encoded[0], encoded[1], encoded[2], encoded[3], encoded[4], encoded[5], encoded[6], nil
}

func insertMiyunInsightAsset(ctx context.Context, execer miyunSQLExecer, value Asset) error {
	_, err := execer.ExecContext(ctx, `INSERT INTO insight_assets (
		id, organization_id, project_id, role, lineage_id, revision, title,
		source_kind, source_ref, source_job_id, platform_asset_id, platform_asset_version,
		asset_type, asset_type_source, asset_type_confidence,
		analysis_status, analysis_status_reason, analysis_status_changed_at,
		version, created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.OrganizationID, value.ProjectID, value.Role, value.LineageID, value.Revision, value.Title,
		value.SourceKind, value.SourceRef, nullableString(value.SourceJobID), nullableString(value.PlatformAssetID),
		nullableInt64(value.PlatformAssetVersion), value.AssetType, value.AssetTypeSource, value.AssetTypeConfidence,
		value.AnalysisStatus, value.AnalysisStatusReason, value.AnalysisStatusChangedAt,
		value.Version, value.CreatedBy, value.CreatedAt, value.UpdatedAt)
	return err
}

func insertManualMiyunMaterial(ctx context.Context, execer miyunSQLExecer, value MiyunMaterial) error {
	_, err := execer.ExecContext(ctx, `INSERT INTO insight_miyun_materials (
		id, organization_id, project_id, miyun_material_id, first_seen_crawl_job_id, import_method,
		manual_idempotency_key, manual_request_hash, resource_id, resource_url_ciphertext, resource_url_key_version, resource_expected_size,
		source_ref, source_ref_status, title, selection_status, import_status, decision_note,
		external_import_id, platform_asset_id, platform_asset_version,
		insight_asset_id, version, created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, 0, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.OrganizationID, value.ProjectID, value.MiyunMaterialID, nil, value.ImportMethod,
		value.ManualIdempotencyKey, value.ManualRequestHash, value.ResourceID, value.SourceRef, value.SourceRefStatus, value.Title,
		value.SelectionStatus, value.ImportStatus, nullableString(value.ExternalImportID), value.PlatformAssetID,
		value.PlatformAssetVersion, value.InsightAssetID, value.Version, value.CreatedBy, value.CreatedAt, value.UpdatedAt)
	return err
}

func insertManualMiyunSnapshot(ctx context.Context, execer miyunSQLExecer, value MiyunMaterialSnapshot) error {
	var raw any
	if len(value.SanitizedRaw) > 0 {
		raw = value.SanitizedRaw
	}
	_, err := execer.ExecContext(ctx, `INSERT INTO insight_miyun_material_snapshots (
		id, organization_id, project_id, material_id, crawl_job_id, source_page, import_method, schema_version,
		captured_at, first_published_at, last_published_at, delivery_days, cumulative_impressions,
		cumulative_impressions_raw, related_ads, related_creators, related_creators_raw, related_creators_known, material_score, views, likes,
		comments, shares, saves, sanitized_raw, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.OrganizationID, value.ProjectID, value.MaterialID, nil, value.SourcePage, value.ImportMethod,
		value.SchemaVersion, value.CapturedAt, value.FirstPublishedAt, value.LastPublishedAt,
		value.DeliveryDays, value.CumulativeImpressions, value.CumulativeImpressionsRaw,
		value.RelatedAds, value.RelatedCreators, value.RelatedCreatorsRaw, value.RelatedCreatorsKnown, value.MaterialScore, value.Views, value.Likes,
		value.Comments, value.Shares, value.Saves, raw, value.CreatedAt)
	return err
}
