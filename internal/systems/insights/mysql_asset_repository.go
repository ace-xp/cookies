package insights

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// MySQL implementation of AssetRepository. Tables are created by
// migrations/insights/20260729103000_insight_asset_index.up.sql.

func (r MySQLRepository) CreateAsset(ctx context.Context, value Asset) (Asset, error) {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO insight_assets (
		id, organization_id, project_id, role, lineage_id, revision, title,
		source_kind, source_ref, source_job_id, platform_asset_id, platform_asset_version,
		asset_type, asset_type_source, asset_type_confidence,
		analysis_status, analysis_status_reason, analysis_status_changed_at,
		version, created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.OrganizationID, value.ProjectID, value.Role, value.LineageID, value.Revision, value.Title,
		value.SourceKind, value.SourceRef, nullableString(value.SourceJobID),
		nullableString(value.PlatformAssetID), nullableInt64(value.PlatformAssetVersion),
		value.AssetType, value.AssetTypeSource, value.AssetTypeConfidence,
		value.AnalysisStatus, value.AnalysisStatusReason, value.AnalysisStatusChangedAt,
		value.Version, value.CreatedBy, value.CreatedAt, value.UpdatedAt)
	if err != nil {
		return Asset{}, err
	}
	return value, nil
}

func (r MySQLRepository) ListAssets(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, filter AssetFilter) ([]Asset, error) {
	query := insightAssetSelect + ` WHERE organization_id = ? AND project_id = ?`
	args := []any{organizationID, projectID}
	if len(filter.Statuses) > 0 {
		query += ` AND analysis_status IN (` + placeholders(len(filter.Statuses)) + `)`
		for _, status := range filter.Statuses {
			args = append(args, status)
		}
	}
	if len(filter.AssetTypes) > 0 {
		query += ` AND asset_type IN (` + placeholders(len(filter.AssetTypes)) + `)`
		for _, assetType := range filter.AssetTypes {
			args = append(args, assetType)
		}
	}
	if len(filter.SourceKinds) > 0 {
		query += ` AND source_kind IN (` + placeholders(len(filter.SourceKinds)) + `)`
		for _, kind := range filter.SourceKinds {
			args = append(args, kind)
		}
	}
	if len(filter.Roles) > 0 {
		query += ` AND role IN (` + placeholders(len(filter.Roles)) + `)`
		for _, role := range filter.Roles {
			args = append(args, role)
		}
	}
	if filter.LineageID != "" {
		query += ` AND lineage_id = ?`
		args = append(args, filter.LineageID)
	}
	// An unset limit means「不限」rather than「零条」: a caller that forgets to set
	// one should get the list, not a silently empty page.
	query += ` ORDER BY updated_at DESC, id DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}
	return r.queryAssets(ctx, query, args...)
}

func (r MySQLRepository) GetAsset(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (Asset, error) {
	value, err := scanAsset(r.DB.QueryRowContext(ctx, insightAssetSelect+` WHERE organization_id = ? AND project_id = ? AND id = ?`, organizationID, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return value, err
}

// ListAssetLineage returns every revision of one creative oldest first (AM-001).
func (r MySQLRepository) ListAssetLineage(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, lineageID string) ([]Asset, error) {
	return r.queryAssets(ctx, insightAssetSelect+` WHERE organization_id = ? AND project_id = ? AND lineage_id = ? ORDER BY revision ASC`,
		organizationID, projectID, lineageID)
}

// UpdateAssetType records the AM-004 answer and the status move together, so an
// asset can never be 可分析 without a type.
func (r MySQLRepository) UpdateAssetType(ctx context.Context, input UpdateAssetTypeInput) (Asset, error) {
	var value Asset
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		current, txErr := getAssetForUpdate(ctx, tx, input.OrganizationID, input.ProjectID, input.ID)
		if txErr != nil {
			return txErr
		}
		if current.Version != input.ExpectedVersion {
			return ErrVersionConflict
		}
		if !allowsAnalysisStatus(input.From, current.AnalysisStatus) {
			return fmt.Errorf("%w: 素材当前是%s，不能在此状态下改判类型", ErrInvalidState, current.AnalysisStatus.Label())
		}
		if _, txErr = tx.ExecContext(ctx, `UPDATE insight_assets
			SET asset_type = ?, asset_type_source = ?, asset_type_confidence = ?,
			    analysis_status = ?, analysis_status_reason = ?, analysis_status_changed_at = ?,
			    version = version + 1, updated_at = ?
			WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ?`,
			input.AssetType, input.Source, input.Confidence,
			input.To, input.Reason, input.Now, input.Now,
			input.OrganizationID, input.ProjectID, input.ID, input.ExpectedVersion); txErr != nil {
			return txErr
		}
		current.AssetType = input.AssetType
		current.AssetTypeSource = input.Source
		current.AssetTypeConfidence = input.Confidence
		current.AnalysisStatus = input.To
		current.AnalysisStatusReason = input.Reason
		current.AnalysisStatusChangedAt = &input.Now
		current.Version++
		current.UpdatedAt = input.Now
		value = current
		return nil
	})
	if err != nil {
		return Asset{}, err
	}
	return value, nil
}

func (r MySQLRepository) TransitionAsset(ctx context.Context, input TransitionAssetInput) (Asset, error) {
	var value Asset
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		current, txErr := transitionAssetTx(ctx, tx, input)
		if txErr != nil {
			return txErr
		}
		value = current
		return nil
	})
	if err != nil {
		return Asset{}, err
	}
	return value, nil
}

func (r MySQLRepository) CreateAssetMapping(ctx context.Context, value AssetMapping) (AssetMapping, error) {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO insight_asset_mappings (
		id, organization_id, project_id, platform, platform_object_kind, platform_object_id, platform_object_name,
		insight_asset_id, status, match_source, matched_by, matched_at, note, version, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.OrganizationID, value.ProjectID, value.Platform, value.PlatformObjectKind,
		value.PlatformObjectID, value.PlatformObjectName, nullableString(value.AssetID), value.Status,
		value.MatchSource, nullableString(value.MatchedBy), value.MatchedAt, value.Note,
		value.Version, value.CreatedAt, value.UpdatedAt)
	if err != nil {
		return AssetMapping{}, err
	}
	return value, nil
}

func (r MySQLRepository) ListAssetMappings(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, filter AssetMappingFilter) ([]AssetMapping, error) {
	query := assetMappingSelect + ` WHERE organization_id = ? AND project_id = ?`
	args := []any{organizationID, projectID}
	if len(filter.Statuses) > 0 {
		query += ` AND status IN (` + placeholders(len(filter.Statuses)) + `)`
		for _, status := range filter.Statuses {
			args = append(args, status)
		}
	}
	if filter.Platform != "" {
		query += ` AND platform = ?`
		args = append(args, filter.Platform)
	}
	if filter.AssetID != "" {
		query += ` AND insight_asset_id = ?`
		args = append(args, filter.AssetID)
	}
	query += ` ORDER BY updated_at DESC, id DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]AssetMapping, 0)
	for rows.Next() {
		value, scanErr := scanAssetMapping(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (r MySQLRepository) GetAssetMapping(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (AssetMapping, error) {
	value, err := scanAssetMapping(r.DB.QueryRowContext(ctx, assetMappingSelect+` WHERE organization_id = ? AND project_id = ? AND id = ?`, organizationID, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AssetMapping{}, ErrNotFound
	}
	return value, err
}

func (r MySQLRepository) ResolveAssetMapping(ctx context.Context, value AssetMapping, expectedVersion int64) (AssetMapping, error) {
	result, err := r.DB.ExecContext(ctx, `UPDATE insight_asset_mappings
		SET insight_asset_id = ?, status = ?, match_source = ?, matched_by = ?, matched_at = ?, note = ?,
		    version = version + 1, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ?`,
		nullableString(value.AssetID), value.Status, value.MatchSource, nullableString(value.MatchedBy),
		value.MatchedAt, value.Note, value.UpdatedAt,
		value.OrganizationID, value.ProjectID, value.ID, expectedVersion)
	if err != nil {
		return AssetMapping{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return AssetMapping{}, err
	}
	if affected == 0 {
		return AssetMapping{}, ErrVersionConflict
	}
	return r.GetAssetMapping(ctx, value.OrganizationID, value.ProjectID, value.ID)
}

// UpsertAssetFeatures writes one layer and advances the asset in one
// transaction. The ON DUPLICATE KEY target is uq_insight_asset_features_layer
// (organization, asset, key, source), which is why re-extracting can only ever
// overwrite the AI row and never the human conclusion beside it (AM-006).
func (r MySQLRepository) UpsertAssetFeatures(ctx context.Context, input UpsertAssetFeaturesInput) ([]AssetFeature, error) {
	err := r.inTx(ctx, func(tx *sql.Tx) error {
		if _, txErr := transitionAssetTx(ctx, tx, TransitionAssetInput{
			OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, ID: input.AssetID,
			ExpectedVersion: input.ExpectedVersion, From: input.From, To: input.To,
			Reason: input.Reason, Now: input.Now,
		}); txErr != nil {
			return txErr
		}
		for _, feature := range input.Features {
			terms, marshalErr := marshalTerms(feature.Value)
			if marshalErr != nil {
				return marshalErr
			}
			if _, txErr := tx.ExecContext(ctx, `INSERT INTO insight_asset_features (
				id, organization_id, project_id, insight_asset_id, asset_type, feature_key, value_kind,
				value_text, value_number, value_bool, value_terms,
				source, confidence, review_state, skill_id, skill_version, extracted_at,
				version, created_by, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				value_kind = VALUES(value_kind), value_text = VALUES(value_text),
				value_number = VALUES(value_number), value_bool = VALUES(value_bool),
				value_terms = VALUES(value_terms), confidence = VALUES(confidence),
				review_state = VALUES(review_state), skill_id = VALUES(skill_id),
				skill_version = VALUES(skill_version), extracted_at = VALUES(extracted_at),
				version = version + 1, updated_at = VALUES(updated_at)`,
				feature.ID, feature.OrganizationID, feature.ProjectID, feature.AssetID,
				feature.AssetType, feature.Key, feature.Value.Kind,
				textValue(feature.Value), numberValue(feature.Value), boolValue(feature.Value), terms,
				feature.Source, feature.Confidence, feature.ReviewState,
				feature.SkillID, feature.SkillVersion, feature.ExtractedAt,
				feature.Version, feature.CreatedBy, feature.CreatedAt, feature.UpdatedAt); txErr != nil {
				return txErr
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.ListAssetFeatures(ctx, input.OrganizationID, input.ProjectID, []string{input.AssetID}, 0)
}

// ListAssetFeatures returns both layers. An empty assetIDs reads the whole
// project, which is what the 特征库 view shows.
func (r MySQLRepository) ListAssetFeatures(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, assetIDs []string, limit int) ([]AssetFeature, error) {
	query := assetFeatureSelect + ` WHERE organization_id = ? AND project_id = ?`
	args := []any{organizationID, projectID}
	if len(assetIDs) > 0 {
		query += ` AND insight_asset_id IN (` + placeholders(len(assetIDs)) + `)`
		for _, assetID := range assetIDs {
			args = append(args, assetID)
		}
	}
	query += ` ORDER BY insight_asset_id ASC, feature_key ASC, source ASC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]AssetFeature, 0)
	for rows.Next() {
		value, scanErr := scanAssetFeature(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// CountAssetFeaturesByReviewState counts one asset's features; an empty state
// counts them all. The 待确认 guard uses both forms.
//
// Only AI rows can be 未复核, and an AI row stops being 未复核 once a human row
// exists for the same key: writing 人工结论 on a feature is itself the review of
// the machine's answer for it. The AI row keeps its own value untouched (§14),
// which is why the human decision has to be read from the other layer instead of
// from the AI row's review_state.
func (r MySQLRepository) CountAssetFeaturesByReviewState(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, assetID string, state ReviewState) (int, error) {
	query := `SELECT COUNT(*) FROM insight_asset_features AS f WHERE f.organization_id = ? AND f.project_id = ? AND f.insight_asset_id = ?`
	args := []any{organizationID, projectID, assetID}
	if state != "" {
		query += ` AND f.review_state = ? AND f.source = ?
			AND NOT EXISTS (
				SELECT 1 FROM insight_asset_features AS h
				WHERE h.organization_id = f.organization_id
				  AND h.insight_asset_id = f.insight_asset_id
				  AND h.feature_key = f.feature_key
				  AND h.source = ?
			)`
		args = append(args, state, SourceAI, SourceHuman)
	}
	var count int
	err := r.DB.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func (r MySQLRepository) queryAssets(ctx context.Context, query string, args ...any) ([]Asset, error) {
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]Asset, 0)
	for rows.Next() {
		value, scanErr := scanAsset(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// transitionAssetTx locks the asset, enforces the optimistic version and the
// allowed source states, then records the move along the 03 §11.1 chain.
func transitionAssetTx(ctx context.Context, tx *sql.Tx, input TransitionAssetInput) (Asset, error) {
	current, err := getAssetForUpdate(ctx, tx, input.OrganizationID, input.ProjectID, input.ID)
	if err != nil {
		return Asset{}, err
	}
	if current.Version != input.ExpectedVersion {
		return Asset{}, ErrVersionConflict
	}
	if !allowsAnalysisStatus(input.From, current.AnalysisStatus) {
		return Asset{}, fmt.Errorf("%w: 素材当前是%s，不允许变更为%s",
			ErrInvalidState, current.AnalysisStatus.Label(), input.To.Label())
	}
	if _, err := tx.ExecContext(ctx, `UPDATE insight_assets
		SET analysis_status = ?, analysis_status_reason = ?, analysis_status_changed_at = ?,
		    version = version + 1, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ? AND version = ?`,
		input.To, input.Reason, input.Now, input.Now,
		input.OrganizationID, input.ProjectID, input.ID, input.ExpectedVersion); err != nil {
		return Asset{}, err
	}
	current.AnalysisStatus = input.To
	current.AnalysisStatusReason = input.Reason
	current.AnalysisStatusChangedAt = &input.Now
	current.Version++
	current.UpdatedAt = input.Now
	return current, nil
}

func getAssetForUpdate(ctx context.Context, tx *sql.Tx, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (Asset, error) {
	value, err := scanAsset(tx.QueryRowContext(ctx, insightAssetSelect+` WHERE organization_id = ? AND project_id = ? AND id = ? FOR UPDATE`, organizationID, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	return value, err
}

const insightAssetSelect = `SELECT id, organization_id, project_id, role, lineage_id, revision, title, source_kind, source_ref, source_job_id, platform_asset_id, platform_asset_version, asset_type, asset_type_source, asset_type_confidence, analysis_status, analysis_status_reason, analysis_status_changed_at, version, created_by, created_at, updated_at FROM insight_assets`
const assetMappingSelect = `SELECT id, organization_id, project_id, platform, platform_object_kind, platform_object_id, platform_object_name, insight_asset_id, status, match_source, matched_by, matched_at, note, version, created_at, updated_at FROM insight_asset_mappings`
const assetFeatureSelect = `SELECT id, organization_id, project_id, insight_asset_id, asset_type, feature_key, value_kind, value_text, value_number, value_bool, value_terms, source, confidence, review_state, skill_id, skill_version, extracted_at, version, created_by, created_at, updated_at FROM insight_asset_features`

func scanAsset(row rowScanner) (Asset, error) {
	var value Asset
	var sourceJobID, platformAssetID sql.NullString
	var platformAssetVersion sql.NullInt64
	var statusChangedAt sql.NullTime
	err := row.Scan(&value.ID, &value.OrganizationID, &value.ProjectID, &value.Role, &value.LineageID, &value.Revision,
		&value.Title, &value.SourceKind, &value.SourceRef, &sourceJobID,
		&platformAssetID, &platformAssetVersion,
		&value.AssetType, &value.AssetTypeSource, &value.AssetTypeConfidence,
		&value.AnalysisStatus, &value.AnalysisStatusReason, &statusChangedAt,
		&value.Version, &value.CreatedBy, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return Asset{}, err
	}
	value.SourceJobID = sourceJobID.String
	value.PlatformAssetID = platformAssetID.String
	value.PlatformAssetVersion = platformAssetVersion.Int64
	if statusChangedAt.Valid {
		value.AnalysisStatusChangedAt = &statusChangedAt.Time
	}
	return value, nil
}

func scanAssetMapping(row rowScanner) (AssetMapping, error) {
	var value AssetMapping
	var assetID, matchedBy sql.NullString
	var matchedAt sql.NullTime
	err := row.Scan(&value.ID, &value.OrganizationID, &value.ProjectID, &value.Platform,
		&value.PlatformObjectKind, &value.PlatformObjectID, &value.PlatformObjectName,
		&assetID, &value.Status, &value.MatchSource, &matchedBy, &matchedAt, &value.Note,
		&value.Version, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return AssetMapping{}, err
	}
	value.AssetID = assetID.String
	value.MatchedBy = matchedBy.String
	if matchedAt.Valid {
		value.MatchedAt = &matchedAt.Time
	}
	return value, nil
}

func scanAssetFeature(row rowScanner) (AssetFeature, error) {
	var value AssetFeature
	var text sql.NullString
	var number sql.NullFloat64
	var boolean sql.NullBool
	var terms []byte
	var extractedAt sql.NullTime
	err := row.Scan(&value.ID, &value.OrganizationID, &value.ProjectID, &value.AssetID,
		&value.AssetType, &value.Key, &value.Value.Kind,
		&text, &number, &boolean, &terms,
		&value.Source, &value.Confidence, &value.ReviewState,
		&value.SkillID, &value.SkillVersion, &extractedAt,
		&value.Version, &value.CreatedBy, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return AssetFeature{}, err
	}
	value.Value.Text = text.String
	value.Value.Number = number.Float64
	value.Value.Bool = boolean.Bool
	if len(terms) > 0 {
		if err := json.Unmarshal(terms, &value.Value.Terms); err != nil {
			return AssetFeature{}, fmt.Errorf("decode asset feature terms: %w", err)
		}
	}
	if extractedAt.Valid {
		value.ExtractedAt = &extractedAt.Time
	}
	return value, nil
}

// Only the column matching the declared kind is written; the rest stay NULL so
// a reader can never pick up a stale value from another shape.
func textValue(value FeatureValue) any {
	if value.Kind == FeatureKindText {
		return value.Text
	}
	return nil
}

func numberValue(value FeatureValue) any {
	if value.Kind == FeatureKindNumber || value.Kind == FeatureKindDuration {
		return value.Number
	}
	return nil
}

func boolValue(value FeatureValue) any {
	if value.Kind == FeatureKindBool {
		return value.Bool
	}
	return nil
}

func marshalTerms(value FeatureValue) (any, error) {
	switch value.Kind {
	case FeatureKindTags, FeatureKindEnum, FeatureKindEnumMul:
		encoded, err := json.Marshal(value.Terms)
		if err != nil {
			return nil, err
		}
		return encoded, nil
	}
	return nil, nil
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func placeholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", count), ", ")
}
