package insights

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/shikanon/cookies/internal/platform/contract"
)

const MiyunProductProfileRuleVersion = "miyun-product-profile-rules/v2"
const MiyunDataCardSchemaV1 = "miyun-data-card/v1"

const maxMiyunAnalysisSources = 32
const maxMiyunCorpusBytes = 2 * 1024 * 1024

type MiyunProjectProduct struct {
	ID   contract.ProductID `json:"id"`
	Name string             `json:"name"`
}

type MiyunProjectSource struct {
	Context      contract.ProjectContext `json:"context"`
	ProjectName  string                  `json:"project_name"`
	BrandName    string                  `json:"brand_name"`
	CategoryName string                  `json:"category_name"`
	Products     []MiyunProjectProduct   `json:"products"`
}

// MiyunProductSource is the safe Project identity projection used by the
// product-analysis form. Product IDs stay server-authoritative; callers only
// need human-readable names to select an existing Project product.
type MiyunProductSource struct {
	ProjectName  string                `json:"project_name"`
	BrandName    string                `json:"brand_name"`
	CategoryName string                `json:"category_name"`
	Products     []MiyunProjectProduct `json:"products"`
}

type MiyunProjectSourceReader interface {
	ReadMiyunProjectSource(context.Context, contract.ActorContext, contract.ProjectID) (MiyunProjectSource, error)
}

type MiyunAssetSource struct {
	Ref      contract.AssetVersionRef `json:"ref"`
	Kind     contract.AssetKind       `json:"kind"`
	MIMEType string                   `json:"mime_type"`
	SHA256   string                   `json:"sha256"`
	Ready    bool                     `json:"ready"`
}

type MiyunAssetSourceReader interface {
	ReadMiyunAssetSource(context.Context, contract.ActorContext, contract.ProjectID, contract.AssetVersionRef) (MiyunAssetSource, error)
}

type MiyunKnowledgeSource struct {
	ID            string `json:"id"`
	Filename      string `json:"filename"`
	MIMEType      string `json:"mime_type"`
	Status        string `json:"status"`
	Text          string `json:"-"`
	TextSHA256    string `json:"text_sha256"`
	ContentSHA256 string `json:"content_sha256"`
}

type MiyunKnowledgeSourceReader interface {
	ReadMiyunKnowledgeSource(context.Context, contract.ActorContext, contract.ProjectID, string) (MiyunKnowledgeSource, error)
}

type MiyunMediaEvidence struct {
	ArtifactID             string   `json:"artifact_id"`
	Status                 string   `json:"status"`
	ContentHash            string   `json:"content_hash"`
	Evidence               []string `json:"evidence"`
	MediaFormatCode        string   `json:"media_format_code,omitempty"`
	ContentStyleCode       string   `json:"content_style_code,omitempty"`
	ContentStyleConfidence float64  `json:"content_style_confidence,omitempty"`
	VisionProviderCode     string   `json:"vision_provider_code,omitempty"`
	VisionModelVersion     string   `json:"vision_model_version,omitempty"`
	ASRProviderCode        string   `json:"asr_provider_code,omitempty"`
	ASRModelVersion        string   `json:"asr_model_version,omitempty"`
}

type MiyunMediaEvidenceReader interface {
	ReadLatestMiyunMediaEvidence(context.Context, contract.ActorContext, contract.ProjectID, contract.AssetVersionRef) (MiyunMediaEvidence, bool, error)
}

type AnalyzeMiyunProductProfileRequest struct {
	ConnectionID         string                     `json:"connection_id"`
	ProductID            contract.ProductID         `json:"product_id,omitempty"`
	ProductName          string                     `json:"product_name,omitempty"`
	CategoryName         string                     `json:"category_name,omitempty"`
	ProductAssetRefs     []contract.AssetVersionRef `json:"product_asset_refs"`
	KnowledgeDocumentIDs []string                   `json:"knowledge_document_ids"`
}

type MiyunProfileQuery struct {
	ProductName          string    `json:"product_name"`
	CategoryID           string    `json:"category_id,omitempty"`
	CategoryName         string    `json:"category_name,omitempty"`
	Keywords             []string  `json:"keywords"`
	MaterialTypes        []string  `json:"material_types"`
	MaterialContentTypes []string  `json:"material_content_types"`
	WindowStart          time.Time `json:"window_start"`
	WindowEnd            time.Time `json:"window_end"`
}

type ConfirmMiyunProductProfileRequest struct {
	ExpectedVersion int64             `json:"expected_version"`
	Query           MiyunProfileQuery `json:"query"`
}

type ManualMiyunDataCard struct {
	SchemaVersion            string          `json:"schema_version"`
	CapturedAt               time.Time       `json:"captured_at"`
	FirstPublishedAt         *time.Time      `json:"first_published_at,omitempty"`
	LastPublishedAt          *time.Time      `json:"last_published_at,omitempty"`
	DeliveryDays             int64           `json:"delivery_days"`
	CumulativeImpressionsRaw string          `json:"cumulative_impressions_raw"`
	CumulativeImpressions    int64           `json:"cumulative_impressions"`
	RelatedAds               int64           `json:"related_ads"`
	RelatedCreators          int64           `json:"related_creators"`
	MaterialScore            float64         `json:"material_score"`
	Views                    int64           `json:"views"`
	Likes                    int64           `json:"likes"`
	Comments                 int64           `json:"comments"`
	Shares                   int64           `json:"shares"`
	Saves                    int64           `json:"saves"`
	SourceFields             json.RawMessage `json:"source_fields,omitempty"`
}

type ManualMiyunMaterialRequest struct {
	AssetRef        contract.AssetVersionRef `json:"asset_ref"`
	MiyunMaterialID string                   `json:"miyun_material_id"`
	SourceRef       string                   `json:"source_ref"`
	Title           string                   `json:"title"`
	DataCard        ManualMiyunDataCard      `json:"data_card"`
}

type MiyunManualImportRecord struct {
	Material     MiyunMaterial
	Snapshot     MiyunMaterialSnapshot
	InsightAsset Asset
}

type MiyunManualImportResult struct {
	Material     MiyunMaterial         `json:"material"`
	Snapshot     MiyunMaterialSnapshot `json:"snapshot"`
	InsightAsset Asset                 `json:"insight_asset"`
	Replayed     bool                  `json:"replayed"`
}

func (s Service) AnalyzeMiyunProductProfile(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, request AnalyzeMiyunProductProfileRequest) (MiyunProductProfile, error) {
	if err := s.miyunReady(actor, projectID, ScopeWrite); err != nil {
		return MiyunProductProfile{}, err
	}
	if s.MiyunAssets == nil || s.MiyunKnowledge == nil {
		return MiyunProductProfile{}, fmt.Errorf("Miyun product analysis sources are incomplete")
	}
	request.ConnectionID = strings.TrimSpace(request.ConnectionID)
	request.ProductID = contract.ProductID(strings.TrimSpace(string(request.ProductID)))
	request.ProductName = strings.TrimSpace(request.ProductName)
	request.CategoryName = strings.TrimSpace(request.CategoryName)
	if request.ConnectionID == "" || len(request.ConnectionID) > 96 || len(request.ProductID) > 96 || len([]rune(request.ProductName)) > 255 || len([]rune(request.CategoryName)) > 255 || request.ProductAssetRefs == nil || request.KnowledgeDocumentIDs == nil {
		return MiyunProductProfile{}, fmt.Errorf("%w: connection_id, optional product identity, product_asset_refs, and knowledge_document_ids are required", ErrInvalidRequest)
	}
	connection, err := s.Miyun.GetMiyunConnection(ctx, actor.OrganizationID, projectID, request.ConnectionID)
	if err != nil {
		return MiyunProductProfile{}, err
	}
	if connection.Status != MiyunConnectionReady {
		return MiyunProductProfile{}, fmt.Errorf("%w: Miyun connection is not ready", ErrInvalidState)
	}
	projectSource, err := s.MiyunProjects.ReadMiyunProjectSource(ctx, actor, projectID)
	if err != nil {
		return MiyunProductProfile{}, err
	}
	if projectSource.Context.ProjectID != projectID || projectSource.Context.OrganizationID != actor.OrganizationID || projectSource.Context.Validate() != nil {
		return MiyunProductProfile{}, fmt.Errorf("%w: project source is inconsistent", ErrInvalidState)
	}
	productName := request.ProductName
	categoryName := request.CategoryName
	pendingProductIdentity := false
	if request.ProductID != "" {
		productInContext := false
		for _, productID := range projectSource.Context.ProductIDs {
			if productID == request.ProductID {
				productInContext = true
				break
			}
		}
		for _, product := range projectSource.Products {
			if product.ID == request.ProductID {
				productName = strings.TrimSpace(product.Name)
				break
			}
		}
		if productName == "" || !productInContext {
			return MiyunProductProfile{}, fmt.Errorf("%w: selected product does not belong to this project", ErrNotFound)
		}
	} else {
		pendingProductIdentity = true
		if productName == "" {
			return MiyunProductProfile{}, fmt.Errorf("%w: product_name is required when product_id is omitted", ErrInvalidRequest)
		}
		request.ProductID = pendingMiyunProductID(projectID, productName)
	}
	if categoryName == "" {
		categoryName = strings.TrimSpace(projectSource.CategoryName)
	}

	assetRefs, err := normalizeMiyunAssetRefs(request.ProductAssetRefs)
	if err != nil {
		return MiyunProductProfile{}, err
	}
	documentIDs, err := normalizeMiyunStrings("knowledge document ID", request.KnowledgeDocumentIDs, 96)
	if err != nil {
		return MiyunProductProfile{}, err
	}
	if len(assetRefs) > maxMiyunAnalysisSources || len(documentIDs) > maxMiyunAnalysisSources {
		return MiyunProductProfile{}, fmt.Errorf("%w: at most %d assets and documents may be analyzed", ErrInvalidRequest, maxMiyunAnalysisSources)
	}
	assets := make([]MiyunAssetSource, 0, len(assetRefs))
	mediaEvidence := make([]MiyunMediaEvidence, 0, len(assetRefs))
	corpus := []string{productName, projectSource.BrandName, categoryName}
	for _, ref := range assetRefs {
		source, readErr := s.MiyunAssets.ReadMiyunAssetSource(ctx, actor, projectID, ref)
		if readErr != nil {
			return MiyunProductProfile{}, readErr
		}
		if source.Ref != ref || !source.Ready || !miyunProductAssetSupported(source) {
			return MiyunProductProfile{}, fmt.Errorf("%w: selected asset is not a ready supported product asset", ErrInvalidState)
		}
		assets = append(assets, source)
		if s.MiyunMedia != nil {
			evidence, found, evidenceErr := s.MiyunMedia.ReadLatestMiyunMediaEvidence(ctx, actor, projectID, ref)
			if evidenceErr != nil {
				return MiyunProductProfile{}, evidenceErr
			}
			if found && (evidence.Status == "ready" || evidence.Status == "partial") {
				mediaEvidence = append(mediaEvidence, evidence)
				corpus = append(corpus, evidence.Evidence...)
			}
		}
	}
	documents := make([]MiyunKnowledgeSource, 0, len(documentIDs))
	for _, documentID := range documentIDs {
		document, readErr := s.MiyunKnowledge.ReadMiyunKnowledgeSource(ctx, actor, projectID, documentID)
		if readErr != nil {
			return MiyunProductProfile{}, readErr
		}
		if document.ID != documentID || document.Status != "ready" || strings.TrimSpace(document.Text) == "" || len(document.TextSHA256) != 64 {
			return MiyunProductProfile{}, fmt.Errorf("%w: knowledge document %q is not ready for analysis", ErrInvalidState, documentID)
		}
		documents = append(documents, document)
		corpus = append(corpus, document.Text)
	}

	analysisTime := s.now()
	input := struct {
		RuleVersion          string                 `json:"rule_version"`
		FilterCatalogVersion string                 `json:"filter_catalog_version"`
		AnalysisDate         string                 `json:"analysis_date"`
		Project              MiyunProjectSource     `json:"project"`
		ProductID            contract.ProductID     `json:"product_id"`
		Assets               []MiyunAssetSource     `json:"assets"`
		Documents            []MiyunKnowledgeSource `json:"documents"`
		Media                []MiyunMediaEvidence   `json:"media_evidence"`
	}{MiyunProductProfileRuleVersion, MiyunMaterialFilterCatalogVersion, dateOnly(analysisTime).Format("2006-01-02"), projectSource, request.ProductID, assets, documents, mediaEvidence}
	inputSnapshot, err := json.Marshal(input)
	if err != nil {
		return MiyunProductProfile{}, err
	}
	inputHash, err := contract.CanonicalJSONHash(input)
	if err != nil {
		return MiyunProductProfile{}, err
	}
	boundedCorpus, corpusTruncated := boundedMiyunCorpus(corpus)
	query, sources, warnings := deriveMiyunProfileQuery(productName, projectSource.BrandName, categoryName, boundedCorpus, mediaEvidence, analysisTime)
	if pendingProductIdentity {
		warnings = append(warnings, "product_identity_pending_confirmation")
	}
	if corpusTruncated {
		warnings = append(warnings, "analysis_corpus_truncated")
	}
	sources = attachMiyunFieldSourceRefs(sources, projectID, request.ProductID, assets, documents, mediaEvidence)
	id, err := s.idGenerator()("miyunprofile")
	if err != nil {
		return MiyunProductProfile{}, err
	}
	now := analysisTime
	profile := MiyunProductProfile{
		ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID,
		ConnectionID: request.ConnectionID, Status: MiyunProfileDraft,
		ProductID: request.ProductID, ProductName: query.ProductName, BrandName: strings.TrimSpace(projectSource.BrandName),
		CategoryID: query.CategoryID, CategoryName: query.CategoryName, Keywords: query.Keywords, MaterialTypes: query.MaterialTypes,
		MaterialContentTypes: query.MaterialContentTypes, WindowStart: query.WindowStart, WindowEnd: query.WindowEnd,
		ProjectContextVersion: projectSource.Context.ProjectContextVersion, ProductAssetRefs: assetRefs,
		KnowledgeDocumentIDs: documentIDs, RuleVersion: MiyunProductProfileRuleVersion,
		AnalysisMethod: "rules", InputHash: inputHash, InputSnapshot: inputSnapshot,
		FieldSources: sources, AnalysisWarnings: warnings,
		Version: 1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	}
	return s.Miyun.CreateMiyunProductProfileDraft(ctx, profile)
}

// GetMiyunProductSource exposes only the Project-scoped identity facts the
// analysis form needs. It is deliberately not a global product catalogue.
func (s Service) GetMiyunProductSource(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID) (MiyunProductSource, error) {
	if err := s.miyunReady(actor, projectID, ScopeRead); err != nil {
		return MiyunProductSource{}, err
	}
	if s.MiyunProjects == nil {
		return MiyunProductSource{}, fmt.Errorf("Miyun Project source is unavailable")
	}
	value, err := s.MiyunProjects.ReadMiyunProjectSource(ctx, actor, projectID)
	if err != nil {
		return MiyunProductSource{}, err
	}
	if value.Context.ProjectID != projectID || value.Context.OrganizationID != actor.OrganizationID || value.Context.Validate() != nil {
		return MiyunProductSource{}, fmt.Errorf("%w: project source is inconsistent", ErrInvalidState)
	}
	return MiyunProductSource{
		ProjectName: strings.TrimSpace(value.ProjectName), BrandName: strings.TrimSpace(value.BrandName),
		CategoryName: strings.TrimSpace(value.CategoryName), Products: append([]MiyunProjectProduct(nil), value.Products...),
	}, nil
}

func pendingMiyunProductID(projectID contract.ProjectID, productName string) contract.ProductID {
	digest := sha256.Sum256([]byte(string(projectID) + "\n" + strings.TrimSpace(productName)))
	return contract.ProductID(fmt.Sprintf("project_input:%x", digest[:12]))
}

func (s Service) ConfirmMiyunProductProfile(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, profileID string, request ConfirmMiyunProductProfileRequest) (MiyunProductProfile, error) {
	if err := s.miyunReady(actor, projectID, ScopeConfirm); err != nil {
		return MiyunProductProfile{}, err
	}
	if request.ExpectedVersion < 1 || strings.TrimSpace(profileID) == "" {
		return MiyunProductProfile{}, ErrInvalidRequest
	}
	current, err := s.Miyun.GetMiyunProductProfile(ctx, actor.OrganizationID, projectID, strings.TrimSpace(profileID))
	if err != nil {
		return MiyunProductProfile{}, err
	}
	if current.Status != MiyunProfileDraft {
		return MiyunProductProfile{}, ErrInvalidState
	}
	if request.ExpectedVersion != current.Version {
		return MiyunProductProfile{}, ErrVersionConflict
	}
	if err := validateMiyunProfileQuery(request.Query); err != nil {
		return MiyunProductProfile{}, err
	}
	now := s.now()
	current.ProductName = strings.TrimSpace(request.Query.ProductName)
	current.CategoryID = strings.TrimSpace(request.Query.CategoryID)
	current.CategoryName = strings.TrimSpace(request.Query.CategoryName)
	current.Keywords, _ = normalizeMiyunStrings("keyword", request.Query.Keywords, 100)
	current.MaterialTypes, _ = normalizeMiyunMTypes(request.Query.MaterialTypes)
	current.MaterialContentTypes, _ = normalizeMiyunMaterialTags(request.Query.MaterialContentTypes)
	current.WindowStart, current.WindowEnd = dateOnly(request.Query.WindowStart), dateOnly(request.Query.WindowEnd)
	current.Status, current.ConfirmedBy, current.ConfirmedAt = MiyunProfileConfirmed, actor.Principal.ID, &now
	current.FieldSources = confirmedMiyunFieldSources(current.FieldSources)
	current.UpdatedAt = now
	return s.Miyun.ConfirmMiyunProductProfile(ctx, current, request.ExpectedVersion)
}

func (s Service) ListMiyunProductProfiles(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, limit int) ([]MiyunProductProfile, error) {
	if err := s.miyunReady(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	if _, err := s.MiyunProjects.ReadMiyunProjectSource(ctx, actor, projectID); err != nil {
		return nil, err
	}
	return s.Miyun.ListMiyunProductProfiles(ctx, actor.OrganizationID, projectID, normalizeLimit(limit))
}

func (s Service) GetMiyunProductProfile(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, profileID string) (MiyunProductProfile, error) {
	if err := s.miyunReady(actor, projectID, ScopeRead); err != nil {
		return MiyunProductProfile{}, err
	}
	if _, err := s.MiyunProjects.ReadMiyunProjectSource(ctx, actor, projectID); err != nil {
		return MiyunProductProfile{}, err
	}
	return s.Miyun.GetMiyunProductProfile(ctx, actor.OrganizationID, projectID, strings.TrimSpace(profileID))
}

func (s Service) ManualImportMiyunMaterial(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, key contract.IdempotencyKey, request ManualMiyunMaterialRequest) (MiyunManualImportResult, error) {
	if err := s.miyunReady(actor, projectID, ScopeWrite); err != nil {
		return MiyunManualImportResult{}, err
	}
	if s.MiyunAssets == nil {
		return MiyunManualImportResult{}, fmt.Errorf("Miyun asset source is unavailable")
	}
	if err := key.Validate(); err != nil || len(key) > 128 {
		return MiyunManualImportResult{}, fmt.Errorf("%w: invalid idempotency key", ErrInvalidRequest)
	}
	request.MiyunMaterialID = strings.TrimSpace(request.MiyunMaterialID)
	request.SourceRef = strings.TrimSpace(request.SourceRef)
	request.Title = strings.TrimSpace(request.Title)
	if err := validateManualMiyunMaterialRequest(request); err != nil {
		return MiyunManualImportResult{}, err
	}
	assetSource, err := s.MiyunAssets.ReadMiyunAssetSource(ctx, actor, projectID, request.AssetRef)
	if err != nil {
		return MiyunManualImportResult{}, err
	}
	if assetSource.Ref != request.AssetRef || !assetSource.Ready || assetSource.Kind != contract.AssetVideo || assetSource.MIMEType != "video/mp4" {
		return MiyunManualImportResult{}, fmt.Errorf("%w: manual import requires a ready MP4 AssetVersion in this project", ErrInvalidRequest)
	}
	requestHash, err := contract.CanonicalJSONHash(request)
	if err != nil {
		return MiyunManualImportResult{}, err
	}
	materialID, err := s.idGenerator()("miyunmaterial")
	if err != nil {
		return MiyunManualImportResult{}, err
	}
	snapshotID, err := s.idGenerator()("miyunsnapshot")
	if err != nil {
		return MiyunManualImportResult{}, err
	}
	insightAssetID, err := s.idGenerator()("insightasset")
	if err != nil {
		return MiyunManualImportResult{}, err
	}
	now := s.now()
	title := request.Title
	if title == "" {
		title = "Miyun material " + request.MiyunMaterialID
	}
	sanitizedRaw, err := json.Marshal(request.DataCard)
	if err != nil {
		return MiyunManualImportResult{}, err
	}
	record := MiyunManualImportRecord{
		Material: MiyunMaterial{
			ID: materialID, OrganizationID: actor.OrganizationID, ProjectID: projectID,
			MiyunMaterialID: request.MiyunMaterialID, ImportMethod: MiyunImportManual,
			ManualIdempotencyKey: string(key), ManualRequestHash: requestHash,
			SourceRef: request.SourceRef, SourceRefStatus: "verified", Title: title, SelectionStatus: MiyunMaterialDiscovered,
			ImportStatus: MiyunMaterialImportImported, PlatformAssetID: request.AssetRef.AssetID,
			PlatformAssetVersion: request.AssetRef.Version, InsightAssetID: insightAssetID,
			Version: 1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
		},
		Snapshot: MiyunMaterialSnapshot{
			ID: snapshotID, OrganizationID: actor.OrganizationID, ProjectID: projectID,
			MaterialID: materialID, ImportMethod: MiyunImportManual,
			SchemaVersion: request.DataCard.SchemaVersion, CapturedAt: request.DataCard.CapturedAt,
			FirstPublishedAt: request.DataCard.FirstPublishedAt, LastPublishedAt: request.DataCard.LastPublishedAt,
			DeliveryDays: request.DataCard.DeliveryDays, CumulativeImpressions: request.DataCard.CumulativeImpressions,
			CumulativeImpressionsRaw: request.DataCard.CumulativeImpressionsRaw,
			RelatedAds:               request.DataCard.RelatedAds, RelatedCreators: request.DataCard.RelatedCreators,
			RelatedCreatorsRaw: fmt.Sprintf("%d", request.DataCard.RelatedCreators), RelatedCreatorsKnown: true,
			MaterialScore: request.DataCard.MaterialScore, Views: request.DataCard.Views, Likes: request.DataCard.Likes,
			Comments: request.DataCard.Comments, Shares: request.DataCard.Shares, Saves: request.DataCard.Saves,
			SanitizedRaw: sanitizedRaw, CreatedAt: now,
		},
		InsightAsset: Asset{
			Role: AssetRoleAnalysis,
			ID:   insightAssetID, OrganizationID: actor.OrganizationID, ProjectID: projectID,
			LineageID: insightAssetID, Revision: 1, Title: title, SourceKind: AssetSourceExternal,
			SourceRef: request.SourceRef, PlatformAssetID: string(request.AssetRef.AssetID),
			PlatformAssetVersion: request.AssetRef.Version, AnalysisStatus: AnalysisAwaitingData,
			AnalysisStatusReason:    "Manual Miyun import registered; awaiting type identification and analysis.",
			AnalysisStatusChangedAt: &now, Version: 1, CreatedBy: actor.Principal.ID,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	return s.Miyun.CreateManualMiyunMaterial(ctx, record)
}

func (s Service) miyunReady(actor contract.ActorContext, projectID contract.ProjectID, scope contract.Scope) error {
	if s.Miyun == nil || s.MiyunProjects == nil {
		return fmt.Errorf("Miyun product analysis dependencies are incomplete")
	}
	if err := actor.Validate(); err != nil || strings.TrimSpace(string(projectID)) == "" {
		return ErrInvalidRequest
	}
	if !actor.HasScope(scope) {
		return fmt.Errorf("%s scope is required", scope)
	}
	return nil
}

func validateManualMiyunMaterialRequest(request ManualMiyunMaterialRequest) error {
	if err := request.AssetRef.Validate(); err != nil || request.MiyunMaterialID == "" || len(request.MiyunMaterialID) > 191 || len(request.Title) > 255 {
		return fmt.Errorf("%w: asset_ref, Miyun material ID, and bounded title are required", ErrInvalidRequest)
	}
	parsed, err := url.ParseRequestURI(request.SourceRef)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || miyunURLHasSecret(parsed) || len(request.SourceRef) > 512 {
		return fmt.Errorf("%w: source_ref must be an HTTPS URL", ErrInvalidRequest)
	}
	card := request.DataCard
	if card.SchemaVersion != MiyunDataCardSchemaV1 || card.CapturedAt.IsZero() || strings.TrimSpace(card.CumulativeImpressionsRaw) == "" || len(card.CumulativeImpressionsRaw) > 64 {
		return fmt.Errorf("%w: versioned Miyun data card is required", ErrInvalidRequest)
	}
	if card.DeliveryDays < 0 || card.CumulativeImpressions < 0 || card.RelatedAds < 0 || card.RelatedCreators < 0 || card.MaterialScore < 0 || card.Views < 0 || card.Likes < 0 || card.Comments < 0 || card.Shares < 0 || card.Saves < 0 {
		return fmt.Errorf("%w: Miyun data card metrics must be non-negative", ErrInvalidRequest)
	}
	if (card.FirstPublishedAt == nil) != (card.LastPublishedAt == nil) || (card.FirstPublishedAt != nil && card.LastPublishedAt.Before(*card.FirstPublishedAt)) {
		return fmt.Errorf("%w: Miyun data card publication window is invalid", ErrInvalidRequest)
	}
	if len(card.SourceFields) > 0 {
		var raw map[string]any
		if !json.Valid(card.SourceFields) || json.Unmarshal(card.SourceFields, &raw) != nil || containsMiyunSecret(raw) {
			return fmt.Errorf("%w: data card source_fields are invalid or contain forbidden data", ErrInvalidRequest)
		}
		if raw == nil {
			return fmt.Errorf("%w: data card source_fields must be an object", ErrInvalidRequest)
		}
	}
	return nil
}

func validateMiyunProfileQuery(query MiyunProfileQuery) error {
	if strings.TrimSpace(query.ProductName) == "" || len([]rune(query.ProductName)) > 255 {
		return fmt.Errorf("%w: product_name is required", ErrInvalidRequest)
	}
	if len(query.CategoryID) > 96 || len([]rune(query.CategoryName)) > 255 {
		return fmt.Errorf("%w: category identity is too long", ErrInvalidRequest)
	}
	if _, err := normalizeMiyunStrings("keyword", query.Keywords, 100); err != nil || len(query.Keywords) == 0 {
		return fmt.Errorf("%w: at least one valid keyword is required", ErrInvalidRequest)
	}
	if _, err := normalizeMiyunMTypes(query.MaterialTypes); err != nil {
		return err
	}
	if _, err := normalizeMiyunMaterialTags(query.MaterialContentTypes); err != nil {
		return err
	}
	if query.WindowStart.IsZero() || query.WindowEnd.IsZero() || dateOnly(query.WindowEnd).Before(dateOnly(query.WindowStart)) {
		return fmt.Errorf("%w: query date window is invalid", ErrInvalidRequest)
	}
	return nil
}

func deriveMiyunProfileQuery(productName, brandName, categoryName string, corpus []string, media []MiyunMediaEvidence, now time.Time) (MiyunProfileQuery, []MiyunProfileFieldSource, []string) {
	keywords := []string{strings.TrimSpace(productName)}
	if value := strings.TrimSpace(brandName); value != "" && value != strings.TrimSpace(productName) {
		keywords = append(keywords, value)
	}
	if value := strings.TrimSpace(categoryName); value != "" {
		keywords = append(keywords, value)
	}
	keywords = append(keywords, extractMiyunKeywords(corpus...)...)
	keywords, _ = normalizeMiyunStrings("keyword", keywords, 100)
	if len(keywords) > 20 {
		keywords = keywords[:20]
	}
	materialTypes, contentTypes := inferMiyunMaterialFilters(media, corpus...)
	end := dateOnly(now)
	query := MiyunProfileQuery{
		ProductName: strings.TrimSpace(productName), CategoryName: strings.TrimSpace(categoryName),
		Keywords: keywords, MaterialTypes: materialTypes, MaterialContentTypes: contentTypes,
		WindowStart: end.AddDate(0, 0, -29), WindowEnd: end,
	}
	sources := []MiyunProfileFieldSource{
		{Field: "product_name", SourceKind: "project_product", SourceRefs: []string{}, Confidence: "high", ReviewState: "suggested", Explanation: "Copied from the selected Project product."},
		{Field: "category", SourceKind: "project_workbench", SourceRefs: []string{}, Confidence: confidenceForValue(categoryName), ReviewState: reviewForValue(categoryName), Explanation: explanationForValue(categoryName, "Suggested from the Project workbench category.")},
		{Field: "keywords", SourceKind: "deterministic_rules", SourceRefs: []string{}, Confidence: "medium", ReviewState: "suggested", Explanation: "Derived deterministically from Project identity and selected ready evidence."},
		{Field: "material_types", SourceKind: "versioned_miyun_catalog", SourceRefs: []string{}, Confidence: confidenceForList(materialTypes), ReviewState: reviewForList(materialTypes), Explanation: explanationForList(materialTypes, "Mapped technical media format to the versioned Miyun mtype catalog.")},
		{Field: "material_content_types", SourceKind: "versioned_miyun_catalog", SourceRefs: []string{}, Confidence: confidenceForList(contentTypes), ReviewState: reviewForList(contentTypes), Explanation: explanationForList(contentTypes, "Mapped multimodal evidence to the versioned Miyun materialTag catalog.")},
		{Field: "date_window", SourceKind: "deterministic_rules", SourceRefs: []string{}, Confidence: "medium", ReviewState: "suggested", Explanation: "Defaults to the latest 30 calendar days and requires human confirmation."},
	}
	warnings := []string{"model_not_used:deterministic_rules"}
	if strings.TrimSpace(categoryName) == "" {
		warnings = append(warnings, "category_unknown")
	}
	if len(contentTypes) == 0 {
		warnings = append(warnings, "material_content_types_unknown")
	}
	if len(materialTypes) == 0 {
		warnings = append(warnings, "material_types_unknown")
	}
	return query, sources, warnings
}

func extractMiyunKeywords(values ...string) []string {
	result := []string{}
	for _, value := range values {
		fields := strings.FieldsFunc(value, func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune(",，。；;、|/\\\n\r\t:：()（）[]【】", r)
		})
		for _, field := range fields {
			field = strings.TrimSpace(field)
			if count := len([]rune(field)); count >= 2 && count <= 24 {
				result = append(result, field)
			}
			if len(result) >= 40 {
				return result
			}
		}
	}
	return result
}

func inferMiyunMaterialFilters(media []MiyunMediaEvidence, values ...string) ([]string, []string) {
	mtypes := []string{}
	tags := []string{}
	for _, evidence := range media {
		if value := miyunMTypeFromMediaFormat(evidence.MediaFormatCode); value != "" {
			mtypes = append(mtypes, value)
		}
		if evidence.ContentStyleConfidence >= 0.6 {
			if value := miyunMaterialTagFromContentStyle(evidence.ContentStyleCode); value != "" {
				tags = append(tags, value)
			}
		}
	}
	legacyTags := inferMiyunContentTypes(values...)
	tags = append(tags, legacyTags...)
	mtypes, _ = normalizeMiyunMTypes(mtypes)
	tags, _ = normalizeMiyunMaterialTags(tags)
	return mtypes, tags
}

func inferMiyunContentTypes(values ...string) []string {
	joined := strings.ToLower(strings.Join(values, "\n"))
	rules := []struct {
		Value string
		Terms []string
	}{
		{"1", []string{"单人口播", "talking head"}},
		{"5", []string{"商品展示", "产品展示", "product demo"}},
		{"3", []string{"剧情演绎", "剧情", "情景剧", "story"}},
	}
	result := []string{}
	for _, rule := range rules {
		for _, term := range rule.Terms {
			if strings.Contains(joined, term) {
				result = append(result, rule.Value)
				break
			}
		}
	}
	return result
}

func boundedMiyunCorpus(values []string) ([]string, bool) {
	remaining := maxMiyunCorpusBytes
	result := make([]string, 0, len(values))
	truncated := false
	for index, value := range values {
		if remaining == 0 {
			truncated = true
			break
		}
		if len(value) > remaining {
			truncated = true
			value = value[:remaining]
			for value != "" && !utf8.ValidString(value) {
				value = value[:len(value)-1]
			}
		}
		result = append(result, value)
		remaining -= len(value)
		if index < len(values)-1 && remaining == 0 {
			truncated = true
		}
	}
	return result, truncated
}

func attachMiyunFieldSourceRefs(sources []MiyunProfileFieldSource, projectID contract.ProjectID, productID contract.ProductID, assets []MiyunAssetSource, documents []MiyunKnowledgeSource, media []MiyunMediaEvidence) []MiyunProfileFieldSource {
	evidenceRefs := []string{"product:" + string(productID), "project:" + string(projectID)}
	for _, asset := range assets {
		evidenceRefs = append(evidenceRefs, fmt.Sprintf("asset:%s:%d", asset.Ref.AssetID, asset.Ref.Version))
	}
	for _, document := range documents {
		evidenceRefs = append(evidenceRefs, "knowledge:"+document.ID)
	}
	for _, artifact := range media {
		evidenceRefs = append(evidenceRefs, "media:"+artifact.ArtifactID)
	}
	sort.Strings(evidenceRefs)
	uniqueRefs := evidenceRefs[:0]
	for _, reference := range evidenceRefs {
		if len(uniqueRefs) == 0 || uniqueRefs[len(uniqueRefs)-1] != reference {
			uniqueRefs = append(uniqueRefs, reference)
		}
	}
	evidenceRefs = uniqueRefs
	result := append([]MiyunProfileFieldSource(nil), sources...)
	for index := range result {
		switch result[index].Field {
		case "product_name":
			result[index].SourceRefs = []string{"product:" + string(productID)}
		case "category":
			result[index].SourceRefs = []string{"project:" + string(projectID)}
		case "date_window":
			result[index].SourceRefs = []string{"rule:" + MiyunProductProfileRuleVersion}
		default:
			result[index].SourceRefs = append([]string(nil), evidenceRefs...)
		}
	}
	return result
}

func miyunURLHasSecret(value *url.URL) bool {
	for key := range value.Query() {
		normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
		for _, forbidden := range []string{"cookie", "authorization", "session", "password", "token", "signature"} {
			if strings.Contains(normalized, forbidden) {
				return true
			}
		}
	}
	return false
}

func normalizeMiyunAssetRefs(values []contract.AssetVersionRef) ([]contract.AssetVersionRef, error) {
	result := append([]contract.AssetVersionRef(nil), values...)
	for _, ref := range result {
		if err := ref.Validate(); err != nil {
			return nil, fmt.Errorf("%w: invalid product asset reference", ErrInvalidRequest)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].AssetID == result[j].AssetID {
			return result[i].Version < result[j].Version
		}
		return result[i].AssetID < result[j].AssetID
	})
	for index := 1; index < len(result); index++ {
		if result[index] == result[index-1] {
			return nil, fmt.Errorf("%w: duplicate product asset reference", ErrInvalidRequest)
		}
	}
	return result, nil
}

func normalizeMiyunStrings(label string, values []string, maxRunes int) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > maxRunes {
			return nil, fmt.Errorf("%w: %s is empty or too long", ErrInvalidRequest, label)
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	return result, nil
}

func miyunProductAssetSupported(value MiyunAssetSource) bool {
	if value.Kind == contract.AssetVideo {
		return value.MIMEType == "video/mp4"
	}
	if value.Kind == contract.AssetImage {
		return value.MIMEType == "image/jpeg" || value.MIMEType == "image/png" || value.MIMEType == "image/webp"
	}
	return false
}

func confirmedMiyunFieldSources(values []MiyunProfileFieldSource) []MiyunProfileFieldSource {
	result := append([]MiyunProfileFieldSource(nil), values...)
	for index := range result {
		result[index].SourceKind = "human_confirmation"
		result[index].Confidence = "high"
		result[index].ReviewState = "human_confirmed"
		result[index].Explanation = "Reviewed and confirmed by an operator; original analysis lineage remains in input_snapshot."
	}
	return result
}

func dateOnly(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func confidenceForValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return "medium"
}
func reviewForValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return "suggested"
}
func explanationForValue(value, known string) string {
	if strings.TrimSpace(value) == "" {
		return "No supported Project source supplied this field; operator input is required."
	}
	return known
}
func confidenceForList(value []string) string {
	if len(value) == 0 {
		return "unknown"
	}
	return "medium"
}
func reviewForList(value []string) string {
	if len(value) == 0 {
		return "unknown"
	}
	return "suggested"
}
func explanationForList(value []string, known string) string {
	if len(value) == 0 {
		return "No explicit supported phrase was found; operator input is required."
	}
	return known
}
