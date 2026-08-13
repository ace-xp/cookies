package insights

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/integrations/crawler"
	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/jobruntime"
)

const (
	MiyunCrawlJobKind          = "insights.miyun.crawl"
	MiyunMaterialImportJobKind = "insights.miyun.material_import"
	MiyunQuerySchemaV1         = "miyun-query/v1"
	MiyunCrawlerCardSchemaV1   = "miyun-crawler-card/v1"
	DefaultMiyunCrawlMaxPages  = 50
)

type CreateMiyunCrawlJobRequest struct {
	ProductProfileID string `json:"product_profile_id"`
	Operation        string `json:"operation"`
	MaxPages         int    `json:"max_pages"`
}

type MiyunMaterialDecisionRequest struct {
	ExpectedVersion int64  `json:"expected_version"`
	Note            string `json:"note,omitempty"`
}

type MiyunMaterialDetail struct {
	Material  MiyunMaterial           `json:"material"`
	Snapshots []MiyunMaterialSnapshot `json:"snapshots"`
}

type MiyunMaterialListOptions struct {
	CrawlJobID      string
	Search          string
	Sort            string
	HandoffEligible bool
	Limit           int
	Offset          int
}

type MiyunMaterialListPage struct {
	Items  []MiyunMaterial `json:"items"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

type MiyunQuerySnapshot struct {
	SchemaVersion        string                     `json:"schema_version"`
	FilterCatalogVersion string                     `json:"filter_catalog_version"`
	Operation            string                     `json:"operation"`
	ProfileID            string                     `json:"profile_id"`
	ConnectionID         string                     `json:"connection_id"`
	MaxPages             int                        `json:"max_pages"`
	Query                crawler.YouShuQuery        `json:"query"`
	FrozenAt             time.Time                  `json:"frozen_at"`
	ProfileInput         json.RawMessage            `json:"profile_input"`
	AssetRefs            []contract.AssetVersionRef `json:"asset_refs"`
	DocumentIDs          []string                   `json:"document_ids"`
}

type MiyunCrawlPageRecord struct {
	Material MiyunMaterial
	Snapshot MiyunMaterialSnapshot
}

type MiyunCrawlRepository interface {
	CreateMiyunCrawlJobIdempotent(context.Context, MiyunCrawlJob) (MiyunCrawlJob, bool, error)
	ListMiyunCrawlJobs(context.Context, contract.OrganizationID, contract.ProjectID, int) ([]MiyunCrawlJob, error)
	GetMiyunCrawlJob(context.Context, contract.OrganizationID, contract.ProjectID, string) (MiyunCrawlJob, error)
	UpdateMiyunCrawlJob(context.Context, MiyunCrawlJob, int64) (MiyunCrawlJob, error)
	UpdateMiyunCrawlJobAndConnection(context.Context, MiyunCrawlJob, int64, MiyunConnection, int64) (MiyunCrawlJob, MiyunConnection, error)
	ApplyMiyunCrawlPage(context.Context, MiyunCrawlJob, int64, []MiyunCrawlPageRecord, bool) (MiyunCrawlJob, error)
	ListMiyunMaterials(context.Context, contract.OrganizationID, contract.ProjectID, MiyunMaterialListOptions) (MiyunMaterialListPage, error)
	GetMiyunMaterial(context.Context, contract.OrganizationID, contract.ProjectID, string) (MiyunMaterial, error)
	DecideMiyunMaterial(context.Context, MiyunMaterial, int64) (MiyunMaterial, error)
	MarkMiyunMaterialImporting(context.Context, MiyunMaterial, int64, string) (MiyunMaterial, error)
	CompleteMiyunMaterialImport(context.Context, MiyunMaterialImportCompletion) (MiyunMaterial, error)
	FailMiyunMaterialImport(context.Context, MiyunMaterial, int64, string, string) (MiyunMaterial, error)
}

type MiyunRuntimeJobs interface {
	Enqueue(context.Context, jobruntime.CreateRequest) (contract.Job, bool, error)
	Get(context.Context, contract.OrganizationID, contract.ProjectID, string) (contract.Job, error)
	RequestCancel(context.Context, contract.OrganizationID, contract.ProjectID, string, int64, time.Time) (contract.Job, error)
	IsCancelRequested(context.Context, contract.OrganizationID, string) (bool, error)
}

type MiyunPageClient interface {
	FetchMiyunPage(context.Context, MiyunConnection, string, crawler.YouShuQuery) (crawler.YouShuPage, error)
}

type MiyunAuthorizedImportRequest struct {
	Actor           contract.ActorContext
	ProjectID       contract.ProjectID
	MaterialID      string
	MiyunMaterialID string
	ResourceURL     string
	ExpectedSize    int64
	SourceRef       string
	SourceRefStatus string
	IdempotencyKey  string
}

type MiyunAuthorizedImportResult struct {
	ExternalImportID string
	AssetRef         contract.AssetVersionRef
	Deduplicated     bool
	ContentSHA256    string
}

type MiyunAuthorizedImporter interface {
	ImportMiyunMaterial(context.Context, MiyunAuthorizedImportRequest) (MiyunAuthorizedImportResult, error)
}

type MiyunSecretCipher interface {
	Encrypt([]byte) ([]byte, string, error)
	Decrypt([]byte, string) ([]byte, error)
}

type MiyunMaterialImportCompletion struct {
	Material        MiyunMaterial
	ExpectedVersion int64
	Result          MiyunAuthorizedImportResult
	InsightAsset    Asset
}

type miyunRuntimePayload struct {
	OrganizationID contract.OrganizationID `json:"organization_id"`
	ProjectID      contract.ProjectID      `json:"project_id"`
	DomainID       string                  `json:"domain_id"`
	ActorID        string                  `json:"actor_id"`
}

func (s Service) CreateMiyunCrawlJob(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, key contract.IdempotencyKey, request CreateMiyunCrawlJobRequest) (MiyunCrawlJob, error) {
	if err := s.miyunCrawlReady(actor, projectID, ScopeWrite); err != nil {
		return MiyunCrawlJob{}, err
	}
	if s.MiyunPages == nil {
		return MiyunCrawlJob{}, ErrInvalidState
	}
	if err := key.Validate(); err != nil || len(key) > 128 {
		return MiyunCrawlJob{}, fmt.Errorf("%w: invalid idempotency key", ErrInvalidRequest)
	}
	request.ProductProfileID = strings.TrimSpace(request.ProductProfileID)
	request.Operation = strings.ToLower(strings.TrimSpace(request.Operation))
	if request.MaxPages == 0 {
		request.MaxPages = DefaultMiyunCrawlMaxPages
	}
	if request.ProductProfileID == "" || (request.Operation != "product" && request.Operation != "cid") || request.MaxPages < 1 || request.MaxPages > DefaultMiyunCrawlMaxPages {
		return MiyunCrawlJob{}, ErrInvalidRequest
	}
	profile, err := s.Miyun.GetMiyunProductProfile(ctx, actor.OrganizationID, projectID, request.ProductProfileID)
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	if profile.Status != MiyunProfileConfirmed {
		return MiyunCrawlJob{}, ErrInvalidState
	}
	connection, err := s.Miyun.GetMiyunConnection(ctx, actor.OrganizationID, projectID, profile.ConnectionID)
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	if connection.Status != MiyunConnectionReady {
		return MiyunCrawlJob{}, ErrInvalidState
	}
	now := s.now()
	query := crawler.YouShuQuery{
		MaterialIDs: []string{}, StartDate: profile.WindowStart.Format("2006-01-02"), EndDate: profile.WindowEnd.Format("2006-01-02"),
		// `_score_desc` is the verified YouShu MaterialListSort value used by
		// the connection probe. `impression_desc` is not a valid upstream enum.
		Keyword: strings.Join(profile.Keywords, " "), Page: 1, Order: "_score_desc", IsExact: crawler.YouShuBool(false),
		// ProductID identifies a cookies Project product, not a confirmed
		// YouShu product identifier. Do not project it into the upstream
		// GraphQL filter; the frozen, operator-confirmed query terms above are
		// the portable product-driven search contract for this MVP.
		ProductID: []string{}, Tpl: []string{}, SearchField: "all", SearchDSL: []json.RawMessage{},
		AccountType: []string{}, IsSearchAiScene: crawler.YouShuInt(0),
	}
	mtypes, err := normalizeMiyunMTypes(profile.MaterialTypes)
	if err != nil {
		return MiyunCrawlJob{}, fmt.Errorf("%w: confirmed profile contains an invalid mtype", ErrInvalidState)
	}
	materialTags, err := normalizeMiyunMaterialTags(profile.MaterialContentTypes)
	if err != nil {
		return MiyunCrawlJob{}, fmt.Errorf("%w: confirmed profile contains an invalid materialTag", ErrInvalidState)
	}
	query.MType = miyunFilterValue(mtypes)
	query.MaterialTag = miyunFilterValue(materialTags)
	snapshot := MiyunQuerySnapshot{
		SchemaVersion: MiyunQuerySchemaV1, Operation: request.Operation, ProfileID: profile.ID, ConnectionID: connection.ID,
		FilterCatalogVersion: MiyunMaterialFilterCatalogVersion,
		MaxPages:             request.MaxPages,
		Query:                query, FrozenAt: now, ProfileInput: append(json.RawMessage(nil), profile.InputSnapshot...),
		AssetRefs: append([]contract.AssetVersionRef(nil), profile.ProductAssetRefs...), DocumentIDs: append([]string(nil), profile.KnowledgeDocumentIDs...),
	}
	queryJSON, err := json.Marshal(snapshot)
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	requestHash, err := contract.CanonicalJSONHash(struct {
		ProjectID        contract.ProjectID         `json:"project_id"`
		Request          CreateMiyunCrawlJobRequest `json:"request"`
		ProfileVersion   int64                      `json:"profile_version"`
		ProfileInputHash string                     `json:"profile_input_hash"`
	}{projectID, request, profile.Version, profile.InputHash})
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	jobID, err := s.idGenerator()("miyuncrawljob")
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	job := MiyunCrawlJob{
		ID: jobID, OrganizationID: actor.OrganizationID, ProjectID: projectID, ConnectionID: connection.ID,
		ProductProfileID: profile.ID, Status: MiyunCrawlJobQueued, Operation: request.Operation,
		QuerySchemaVersion: MiyunQuerySchemaV1, QuerySnapshot: queryJSON, IdempotencyKey: string(key), RequestHash: requestHash,
		RuntimeJobID: jobID, Version: 1, CreatedBy: actor.Principal.ID, CreatedAt: now, UpdatedAt: now,
	}
	stored, replayed, err := s.MiyunCrawl.CreateMiyunCrawlJobIdempotent(ctx, job)
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	payload, _ := json.Marshal(miyunRuntimePayload{actor.OrganizationID, projectID, stored.ID, actor.Principal.ID})
	_, runtimeReplay, err := s.MiyunJobs.Enqueue(ctx, jobruntime.CreateRequest{
		Job: contract.Job{ID: stored.RuntimeJobID, Kind: MiyunCrawlJobKind, OrganizationID: actor.OrganizationID, ProjectID: projectID,
			Status: contract.JobQueued, Progress: 0, CreatedAt: stored.CreatedAt, UpdatedAt: stored.UpdatedAt,
			Cancellable: true, AttemptCount: 0, MaxAttempts: 1000, Version: 1},
		Payload: payload, IdempotencyKey: key, RequestHash: stored.RequestHash,
	})
	if err != nil {
		if errors.Is(err, jobruntime.ErrIdempotencyConflict) {
			return MiyunCrawlJob{}, ErrIdempotencyConflict
		}
		return MiyunCrawlJob{}, err
	}
	if replayed != runtimeReplay {
		return MiyunCrawlJob{}, fmt.Errorf("%w: crawl/runtime idempotency projections disagree", ErrInvalidState)
	}
	return stored, nil
}

func (s Service) ListMiyunCrawlJobs(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, limit int) ([]MiyunCrawlJob, error) {
	if err := s.miyunCrawlReady(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	return s.MiyunCrawl.ListMiyunCrawlJobs(ctx, actor.OrganizationID, projectID, normalizeLimit(limit))
}

func (s Service) GetMiyunCrawlJob(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, jobID string) (MiyunCrawlJob, error) {
	if err := s.miyunCrawlReady(actor, projectID, ScopeRead); err != nil {
		return MiyunCrawlJob{}, err
	}
	return s.MiyunCrawl.GetMiyunCrawlJob(ctx, actor.OrganizationID, projectID, strings.TrimSpace(jobID))
}

func (s Service) CancelMiyunCrawlJob(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, jobID string, expectedVersion int64) (MiyunCrawlJob, error) {
	if err := s.miyunCrawlReady(actor, projectID, ScopeWrite); err != nil {
		return MiyunCrawlJob{}, err
	}
	current, err := s.MiyunCrawl.GetMiyunCrawlJob(ctx, actor.OrganizationID, projectID, strings.TrimSpace(jobID))
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	if current.Version != expectedVersion {
		return MiyunCrawlJob{}, ErrVersionConflict
	}
	runtimeJob, err := s.MiyunJobs.Get(ctx, actor.OrganizationID, projectID, current.RuntimeJobID)
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	if _, err := s.MiyunJobs.RequestCancel(ctx, actor.OrganizationID, projectID, current.RuntimeJobID, runtimeJob.Version, s.now()); err != nil {
		return MiyunCrawlJob{}, err
	}
	current.Status, current.CooldownUntil = MiyunCrawlJobCancelled, nil
	current.UpdatedAt = s.now()
	return s.MiyunCrawl.UpdateMiyunCrawlJob(ctx, current, expectedVersion)
}

func (s Service) RetryMiyunCrawlJob(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, jobID string, key contract.IdempotencyKey) (MiyunCrawlJob, error) {
	current, err := s.GetMiyunCrawlJob(ctx, actor, projectID, jobID)
	if err != nil {
		return MiyunCrawlJob{}, err
	}
	if current.Status != MiyunCrawlJobFailed && current.Status != MiyunCrawlJobPartial && current.Status != MiyunCrawlJobAuthRequired {
		return MiyunCrawlJob{}, ErrInvalidState
	}
	maxPages := DefaultMiyunCrawlMaxPages
	var frozen MiyunQuerySnapshot
	if json.Unmarshal(current.QuerySnapshot, &frozen) == nil && frozen.MaxPages >= 1 && frozen.MaxPages <= DefaultMiyunCrawlMaxPages {
		maxPages = frozen.MaxPages
	}
	return s.CreateMiyunCrawlJob(ctx, actor, projectID, key, CreateMiyunCrawlJobRequest{ProductProfileID: current.ProductProfileID, Operation: current.Operation, MaxPages: maxPages})
}

func (s Service) ListMiyunMaterials(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, options MiyunMaterialListOptions) (MiyunMaterialListPage, error) {
	if err := s.miyunCrawlReady(actor, projectID, ScopeRead); err != nil {
		return MiyunMaterialListPage{}, err
	}
	options.CrawlJobID = strings.TrimSpace(options.CrawlJobID)
	options.Search = strings.TrimSpace(options.Search)
	options.Sort = strings.TrimSpace(options.Sort)
	options.Limit = normalizeLimit(options.Limit)
	if options.Offset < 0 || options.Offset > 1000000 || len([]rune(options.Search)) > 200 || !validMiyunMaterialSort(options.Sort) {
		return MiyunMaterialListPage{}, ErrInvalidRequest
	}
	return s.MiyunCrawl.ListMiyunMaterials(ctx, actor.OrganizationID, projectID, options)
}

func validMiyunMaterialSort(value string) bool {
	switch value {
	case "", "delivery_days", "cumulative_impressions", "related_ads", "related_creators", "material_score":
		return true
	}
	return false
}

func (s Service) GetMiyunMaterial(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, materialID string) (MiyunMaterial, error) {
	if err := s.miyunCrawlReady(actor, projectID, ScopeRead); err != nil {
		return MiyunMaterial{}, err
	}
	return s.MiyunCrawl.GetMiyunMaterial(ctx, actor.OrganizationID, projectID, strings.TrimSpace(materialID))
}

func (s Service) GetMiyunMaterialDetail(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, materialID string) (MiyunMaterialDetail, error) {
	material, err := s.GetMiyunMaterial(ctx, actor, projectID, materialID)
	if err != nil {
		return MiyunMaterialDetail{}, err
	}
	snapshots, err := s.Miyun.ListMiyunMaterialSnapshots(ctx, actor.OrganizationID, projectID, material.ID)
	if err != nil {
		return MiyunMaterialDetail{}, err
	}
	return MiyunMaterialDetail{Material: material, Snapshots: snapshots}, nil
}

func (s Service) DecideMiyunMaterial(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, materialID string, confirmed bool, request MiyunMaterialDecisionRequest) (MiyunMaterial, error) {
	if err := s.miyunCrawlReady(actor, projectID, ScopeConfirm); err != nil {
		return MiyunMaterial{}, err
	}
	if request.ExpectedVersion < 1 || len([]rune(strings.TrimSpace(request.Note))) > 1000 {
		return MiyunMaterial{}, ErrInvalidRequest
	}
	current, err := s.MiyunCrawl.GetMiyunMaterial(ctx, actor.OrganizationID, projectID, strings.TrimSpace(materialID))
	if err != nil {
		return MiyunMaterial{}, err
	}
	if current.Version != request.ExpectedVersion || current.SelectionStatus != MiyunMaterialDiscovered {
		if current.Version != request.ExpectedVersion {
			return MiyunMaterial{}, ErrVersionConflict
		}
		return MiyunMaterial{}, ErrInvalidState
	}
	now := s.now()
	current.DecisionBy, current.DecisionAt, current.DecisionNote, current.UpdatedAt = actor.Principal.ID, &now, strings.TrimSpace(request.Note), now
	if confirmed {
		current.SelectionStatus = MiyunMaterialConfirmed
	} else {
		current.SelectionStatus, current.ImportStatus = MiyunMaterialRejected, MiyunMaterialImportSkipped
	}
	updated, err := s.MiyunCrawl.DecideMiyunMaterial(ctx, current, request.ExpectedVersion)
	if err != nil || !confirmed {
		return updated, err
	}
	if err := s.enqueueMiyunMaterialImport(ctx, actor, updated); err != nil {
		return MiyunMaterial{}, err
	}
	return updated, nil
}

func (s Service) RetryMiyunMaterialImport(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, materialID string, expectedVersion int64) (MiyunMaterial, error) {
	if err := s.miyunCrawlReady(actor, projectID, ScopeWrite); err != nil {
		return MiyunMaterial{}, err
	}
	current, err := s.MiyunCrawl.GetMiyunMaterial(ctx, actor.OrganizationID, projectID, strings.TrimSpace(materialID))
	if err != nil {
		return MiyunMaterial{}, err
	}
	if current.Version != expectedVersion {
		return MiyunMaterial{}, ErrVersionConflict
	}
	if current.SelectionStatus != MiyunMaterialConfirmed ||
		(current.ImportStatus != MiyunMaterialImportPending && current.ImportStatus != MiyunMaterialImportFailed) {
		return MiyunMaterial{}, ErrInvalidState
	}
	if err := s.enqueueMiyunMaterialImport(ctx, actor, current); err != nil {
		return MiyunMaterial{}, err
	}
	return current, nil
}

func (s Service) enqueueMiyunMaterialImport(ctx context.Context, actor contract.ActorContext, material MiyunMaterial) error {
	id, err := s.idGenerator()("miyunimportjob")
	if err != nil {
		return err
	}
	runtimePayload := miyunRuntimePayload{material.OrganizationID, material.ProjectID, material.ID, actor.Principal.ID}
	payload, err := json.Marshal(runtimePayload)
	if err != nil {
		return err
	}
	hash, err := contract.CanonicalJSONHash(runtimePayload)
	if err != nil {
		return err
	}
	key := contract.IdempotencyKey(fmt.Sprintf("miyun_import_%s_%d", material.ID, material.Version))
	_, _, err = s.MiyunJobs.Enqueue(ctx, jobruntime.CreateRequest{Job: contract.Job{
		ID: id, Kind: MiyunMaterialImportJobKind, OrganizationID: material.OrganizationID, ProjectID: material.ProjectID,
		Status: contract.JobQueued, CreatedAt: s.now(), UpdatedAt: s.now(), Cancellable: false, MaxAttempts: 3, Version: 1,
	}, Payload: payload, IdempotencyKey: key, RequestHash: hash})
	return err
}

func (s Service) miyunCrawlReady(actor contract.ActorContext, projectID contract.ProjectID, scope contract.Scope) error {
	if err := s.miyunReady(actor, projectID, scope); err != nil {
		return err
	}
	if s.MiyunCrawl == nil || s.MiyunJobs == nil {
		return fmt.Errorf("Miyun crawl dependencies are incomplete")
	}
	return nil
}

func (s Service) HandleMiyunCrawlJob(ctx context.Context, claim jobruntime.Claim) (jobruntime.Result, error) {
	payload, err := decodeMiyunRuntimePayload(claim, MiyunCrawlJobKind)
	if err != nil {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_CRAWL_PAYLOAD_INVALID", err)
	}
	if s.MiyunCrawl == nil || s.MiyunPages == nil || s.MiyunSecrets == nil || s.MiyunJobs == nil {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_CRAWL_UNAVAILABLE", errors.New("Miyun crawl dependencies are incomplete"))
	}
	job, err := s.MiyunCrawl.GetMiyunCrawlJob(ctx, payload.OrganizationID, payload.ProjectID, payload.DomainID)
	if err != nil {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_CRAWL_NOT_FOUND", err)
	}
	if job.RuntimeJobID != claim.Job.ID {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_CRAWL_SCOPE_MISMATCH", ErrInvalidState)
	}
	cancelled, err := s.MiyunJobs.IsCancelRequested(ctx, payload.OrganizationID, claim.Job.ID)
	if err != nil {
		return jobruntime.Result{}, err
	}
	if cancelled || job.Status == MiyunCrawlJobCancelled {
		if job.Status != MiyunCrawlJobCancelled {
			job.Status, job.CooldownUntil, job.UpdatedAt = MiyunCrawlJobCancelled, nil, s.now()
			_, _ = s.MiyunCrawl.UpdateMiyunCrawlJob(ctx, job, job.Version)
		}
		return jobruntime.Result{}, nil
	}
	if job.Status == MiyunCrawlJobSucceeded || job.Status == MiyunCrawlJobPartial || job.Status == MiyunCrawlJobFailed || job.Status == MiyunCrawlJobAuthRequired {
		return jobruntime.Result{Ref: &contract.ResourceRef{Type: "miyun_crawl_job", ID: job.ID, Version: &job.Version}}, nil
	}
	connection, err := s.Miyun.GetMiyunConnection(ctx, payload.OrganizationID, payload.ProjectID, job.ConnectionID)
	if err != nil {
		return jobruntime.Result{}, err
	}
	if connection.Status == MiyunConnectionAuthRequired || connection.Status == MiyunConnectionDisabled {
		return s.persistMiyunAuthRequired(ctx, job, connection, "connection", "AUTH_REQUIRED")
	}
	now := s.now()
	if connection.CooldownUntil != nil {
		if now.Before(*connection.CooldownUntil) {
			job.Status, job.CooldownUntil, job.UpdatedAt = MiyunCrawlJobCoolingDown, connection.CooldownUntil, now
			updated, updateErr := s.MiyunCrawl.UpdateMiyunCrawlJob(ctx, job, job.Version)
			if updateErr == nil {
				job = updated
			}
			return jobruntime.Result{}, jobruntime.DeferredError{AvailableAt: *connection.CooldownUntil}
		}
		// Reserve the sole post-cooldown probe by moving the connection's
		// deadline forward under its optimistic version. Concurrent jobs lose
		// the version race and defer after rereading.
		probeUntil := now.Add(s.miyunCooldown())
		connection.CooldownUntil, connection.UpdatedAt = &probeUntil, now
		connection, err = s.Miyun.UpdateMiyunConnection(ctx, connection, connection.Version)
		if errors.Is(err, ErrVersionConflict) {
			fresh, readErr := s.Miyun.GetMiyunConnection(ctx, payload.OrganizationID, payload.ProjectID, job.ConnectionID)
			if readErr != nil {
				return jobruntime.Result{}, readErr
			}
			if fresh.CooldownUntil != nil {
				return jobruntime.Result{}, jobruntime.DeferredError{AvailableAt: *fresh.CooldownUntil}
			}
		}
		if err != nil {
			return jobruntime.Result{}, err
		}
	}
	var frozen MiyunQuerySnapshot
	if json.Unmarshal(job.QuerySnapshot, &frozen) != nil || frozen.SchemaVersion != MiyunQuerySchemaV1 || frozen.ProfileID != job.ProductProfileID || frozen.ConnectionID != job.ConnectionID {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_QUERY_SNAPSHOT_INVALID", ErrInvalidState)
	}
	if frozen.FilterCatalogVersion != "" && frozen.FilterCatalogVersion != MiyunMaterialFilterCatalogVersion {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_QUERY_SNAPSHOT_INVALID", ErrInvalidState)
	}
	// Snapshots created before max_pages was introduced remain executable and
	// adopt the safe default. New snapshots cannot exceed the public limit.
	if frozen.MaxPages == 0 {
		frozen.MaxPages = DefaultMiyunCrawlMaxPages
	}
	if frozen.MaxPages < 1 || frozen.MaxPages > DefaultMiyunCrawlMaxPages {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_QUERY_SNAPSHOT_INVALID", ErrInvalidState)
	}
	pageNumber := job.CompletedPages + 1
	frozen.Query.Page = int(pageNumber)
	job.Status, job.CooldownUntil, job.LastErrorKind, job.LastErrorCode, job.UpdatedAt = MiyunCrawlJobRunning, nil, "", "", now
	job, err = s.MiyunCrawl.UpdateMiyunCrawlJob(ctx, job, job.Version)
	if err != nil {
		return jobruntime.Result{}, err
	}
	page, err := s.MiyunPages.FetchMiyunPage(ctx, connection, job.Operation, frozen.Query)
	if err != nil {
		return s.handleMiyunPageError(ctx, job, connection, err)
	}
	cancelled, err = s.MiyunJobs.IsCancelRequested(ctx, payload.OrganizationID, claim.Job.ID)
	if err != nil {
		return jobruntime.Result{}, err
	}
	if cancelled {
		job.Status, job.CooldownUntil, job.UpdatedAt = MiyunCrawlJobCancelled, nil, s.now()
		if _, err := s.MiyunCrawl.UpdateMiyunCrawlJob(ctx, job, job.Version); err != nil {
			return jobruntime.Result{}, err
		}
		return jobruntime.Result{}, nil
	}
	if connection.CooldownUntil != nil || connection.LastErrorKind != "" {
		connection.CooldownUntil, connection.LastErrorKind, connection.LastErrorCode = nil, "", ""
		connection.LastSuccessfulRequestAt, connection.UpdatedAt = &now, now
		_, _ = s.Miyun.UpdateMiyunConnection(ctx, connection, connection.Version)
	}
	records := make([]MiyunCrawlPageRecord, 0, len(page.Materials))
	for _, source := range page.Materials {
		record, mapErr := s.miyunCrawlRecord(payload, job, pageNumber, source, now)
		if mapErr != nil {
			return jobruntime.Result{}, mapErr
		}
		records = append(records, record)
	}
	finished := miyunPageFinished(page) || pageNumber >= int64(frozen.MaxPages)
	job, err = s.MiyunCrawl.ApplyMiyunCrawlPage(ctx, job, pageNumber, records, finished)
	if err != nil {
		return jobruntime.Result{}, err
	}
	if !finished {
		return jobruntime.Result{}, jobruntime.DeferredError{AvailableAt: s.now().Add(200 * time.Millisecond)}
	}
	return jobruntime.Result{Ref: &contract.ResourceRef{Type: "miyun_crawl_job", ID: job.ID, Version: &job.Version}}, nil
}

func (s Service) HandleMiyunMaterialImportJob(ctx context.Context, claim jobruntime.Claim) (jobruntime.Result, error) {
	payload, err := decodeMiyunRuntimePayload(claim, MiyunMaterialImportJobKind)
	if err != nil {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_IMPORT_PAYLOAD_INVALID", err)
	}
	if s.MiyunCrawl == nil || s.MiyunImports == nil || s.MiyunSecrets == nil {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_IMPORT_UNAVAILABLE", errors.New("Miyun import dependencies are incomplete"))
	}
	material, err := s.MiyunCrawl.GetMiyunMaterial(ctx, payload.OrganizationID, payload.ProjectID, payload.DomainID)
	if err != nil {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_MATERIAL_NOT_FOUND", err)
	}
	if material.SelectionStatus != MiyunMaterialConfirmed {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_MATERIAL_NOT_CONFIRMED", ErrInvalidState)
	}
	if material.ImportStatus == MiyunMaterialImportImported || material.ImportStatus == MiyunMaterialImportDeduplicated {
		return jobruntime.Result{Ref: &contract.ResourceRef{Type: "asset_version", ID: string(material.PlatformAssetID), Version: &material.PlatformAssetVersion}}, nil
	}
	plaintext, err := s.MiyunSecrets.Decrypt(material.ResourceURLCiphertext, material.ResourceURLKeyVersion)
	if err != nil {
		return jobruntime.Result{}, terminalMiyunExecution("MIYUN_RESOURCE_DECRYPT_FAILED", err)
	}
	material.LastImportErrorKind, material.LastImportErrorCode, material.UpdatedAt = "", "", s.now()
	material, err = s.MiyunCrawl.MarkMiyunMaterialImporting(ctx, material, material.Version, claim.Job.ID)
	if err != nil {
		return jobruntime.Result{}, err
	}
	actor := miyunWorkerActor(payload)
	result, err := s.MiyunImports.ImportMiyunMaterial(ctx, MiyunAuthorizedImportRequest{
		Actor: actor, ProjectID: payload.ProjectID, MaterialID: material.ID, MiyunMaterialID: material.MiyunMaterialID,
		ResourceURL: string(plaintext), SourceRef: material.SourceRef, SourceRefStatus: material.SourceRefStatus,
		ExpectedSize:   material.ResourceExpectedSize,
		IdempotencyKey: "miyun_" + material.MiyunMaterialID,
	})
	for index := range plaintext {
		plaintext[index] = 0
	}
	if err != nil {
		kind, code := classifyMiyunImportError(err)
		material.ImportStatus, material.LastImportErrorKind, material.LastImportErrorCode, material.UpdatedAt = MiyunMaterialImportFailed, kind, code, s.now()
		_, _ = s.MiyunCrawl.FailMiyunMaterialImport(ctx, material, material.Version, kind, code)
		return jobruntime.Result{}, jobruntime.ExecutionError{JobError: contract.JobError{Code: code, Message: "Miyun material import failed", Retryable: false}}
	}
	insightID, err := s.idGenerator()("insightasset")
	if err != nil {
		return jobruntime.Result{}, err
	}
	now := s.now()
	insightAsset := Asset{
		// 米云采回来的素材现在就是分析对象——它们进队列、跑特征提取。
		// 台账那一套是给平台自己产的素材用的，米云不走那条路。
		Role: AssetRoleAnalysis,
		ID:   insightID, OrganizationID: payload.OrganizationID, ProjectID: payload.ProjectID, LineageID: insightID, Revision: 1,
		Title: material.Title, SourceKind: AssetSourceMiyun, SourceRef: miyunTraceableSourceRef(material),
		SourceJobID:     material.FirstSeenCrawlJobID,
		PlatformAssetID: string(result.AssetRef.AssetID), PlatformAssetVersion: result.AssetRef.Version,
		// 这句直接显示在界面上，所以用中文，并说清下一步该谁做什么：
		// 采回来的素材已经是分析对象，只差有人认出它是哪类广告。
		AnalysisStatus: AnalysisAwaitingData, AnalysisStatusReason: "米云采集已入库，等待识别广告类型后即可提取变量。",
		AnalysisStatusChangedAt: &now, Version: 1, CreatedBy: payload.ActorID, CreatedAt: now, UpdatedAt: now,
	}
	material, err = s.MiyunCrawl.CompleteMiyunMaterialImport(ctx, MiyunMaterialImportCompletion{Material: material, ExpectedVersion: material.Version, Result: result, InsightAsset: insightAsset})
	if err != nil {
		return jobruntime.Result{}, err
	}
	return jobruntime.Result{Ref: &contract.ResourceRef{Type: "asset_version", ID: string(material.PlatformAssetID), Version: &material.PlatformAssetVersion}}, nil
}

func decodeMiyunRuntimePayload(claim jobruntime.Claim, expectedKind string) (miyunRuntimePayload, error) {
	var payload miyunRuntimePayload
	if claim.Job.Kind != expectedKind || json.Unmarshal(claim.Payload, &payload) != nil || payload.OrganizationID == "" || payload.ProjectID == "" || payload.DomainID == "" || payload.ActorID == "" {
		return payload, ErrInvalidRequest
	}
	if payload.OrganizationID != claim.Job.OrganizationID || payload.ProjectID != claim.Job.ProjectID {
		return payload, ErrInvalidState
	}
	return payload, nil
}

func miyunWorkerActor(payload miyunRuntimePayload) contract.ActorContext {
	// A material-import job continues the confirmed user's authorized action.
	// Its actor ID is a user ID captured in the immutable job payload, not a
	// registered service identity.  Keeping that principal kind is necessary for
	// the Assets project-scope check before an external-import ledger is opened.
	return contract.ActorContext{OrganizationID: payload.OrganizationID, Principal: contract.Principal{Kind: contract.PrincipalUser, ID: payload.ActorID}, Scopes: []contract.Scope{ScopeRead, ScopeWrite, ScopeConfirm, "assets.write"}}
}

func (s Service) miyunCrawlRecord(payload miyunRuntimePayload, job MiyunCrawlJob, sourcePage int64, source crawler.YouShuMaterial, now time.Time) (MiyunCrawlPageRecord, error) {
	ciphertext, keyVersion, err := s.MiyunSecrets.Encrypt([]byte(source.Resource.URL))
	if err != nil {
		return MiyunCrawlPageRecord{}, err
	}
	materialID := stableMiyunID("miyunmaterial", string(payload.ProjectID), source.MaterialID)
	snapshotID := stableMiyunID("miyunsnapshot", job.ID, fmt.Sprintf("%d", sourcePage), source.MaterialID)
	deliveryDays := int64(0)
	if !source.FirstSeenAt.IsZero() && !source.LastSeenAt.IsZero() && !source.LastSeenAt.Before(source.FirstSeenAt) {
		deliveryDays = int64(source.LastSeenAt.Sub(source.FirstSeenAt).Hours()/24) + 1
	}
	raw, _ := json.Marshal(map[string]any{
		"schema_version": MiyunCrawlerCardSchemaV1, "material_id": source.MaterialID, "channel_id": source.ChannelID,
		"material_type": source.MaterialType, "impression_inc_2y": source.ImpressionRaw,
		"related_creators_status": "unknown",
	})
	impressionRaw := strings.TrimSpace(source.ImpressionRaw)
	if impressionRaw == "" {
		impressionRaw = fmt.Sprintf("%d", source.ImpressionInc2Y)
	}
	return MiyunCrawlPageRecord{
		Material: MiyunMaterial{
			ID: materialID, OrganizationID: payload.OrganizationID, ProjectID: payload.ProjectID, MiyunMaterialID: source.MaterialID,
			FirstSeenCrawlJobID: job.ID, ImportMethod: MiyunImportCrawler, ResourceID: source.Resource.ID,
			ResourceURLCiphertext: ciphertext, ResourceURLKeyVersion: keyVersion, SourceRefStatus: "unknown", Title: strings.TrimSpace(source.Slogan),
			ResourceExpectedSize: source.Resource.Size,
			SelectionStatus:      MiyunMaterialDiscovered, ImportStatus: MiyunMaterialImportPending,
			Version: 1, CreatedBy: payload.ActorID, CreatedAt: now, UpdatedAt: now,
		},
		Snapshot: MiyunMaterialSnapshot{
			ID: snapshotID, OrganizationID: payload.OrganizationID, ProjectID: payload.ProjectID, MaterialID: materialID,
			CrawlJobID: job.ID, SourcePage: sourcePage, ImportMethod: MiyunImportCrawler, SchemaVersion: MiyunCrawlerCardSchemaV1,
			CapturedAt: now, FirstPublishedAt: timePointerUnlessZero(source.FirstSeenAt), LastPublishedAt: timePointerUnlessZero(source.LastSeenAt),
			DeliveryDays: deliveryDays, CumulativeImpressions: source.ImpressionInc2Y, CumulativeImpressionsRaw: impressionRaw,
			RelatedAds: source.CntAdID, RelatedCreatorsRaw: "unknown", RelatedCreatorsKnown: false, MaterialScore: source.Score,
			Views: source.Social.View, Likes: source.Social.Like, Comments: source.Social.Comment, Shares: source.Social.Share, Saves: source.Social.Save,
			SanitizedRaw: raw, CreatedAt: now,
		},
	}, nil
}

func (s Service) handleMiyunPageError(ctx context.Context, job MiyunCrawlJob, connection MiyunConnection, cause error) (jobruntime.Result, error) {
	var upstream *crawler.YouShuError
	if !errors.As(cause, &upstream) {
		job.Status, job.LastErrorKind, job.LastErrorCode, job.UpdatedAt = MiyunCrawlJobFailed, "transport", "UNKNOWN", s.now()
		if _, err := s.MiyunCrawl.UpdateMiyunCrawlJob(ctx, job, job.Version); err != nil {
			return jobruntime.Result{}, err
		}
		return jobruntime.Result{}, jobruntime.ExecutionError{JobError: contract.JobError{Code: "MIYUN_CRAWL_FAILED", Message: "Miyun crawl failed", Retryable: false}}
	}
	now := s.now()
	switch upstream.Kind {
	case crawler.YouShuRateLimited:
		until := now.Add(s.miyunCooldown())
		if upstream.Code == "" {
			upstream.Code = "RATE_LIMITED"
		}
		job.Status, job.CooldownUntil, job.LastErrorKind, job.LastErrorCode, job.UpdatedAt = MiyunCrawlJobCoolingDown, &until, string(upstream.Kind), upstream.Code, now
		connection.CooldownUntil, connection.LastErrorKind, connection.LastErrorCode, connection.LastErrorAt, connection.UpdatedAt = &until, string(upstream.Kind), upstream.Code, &now, now
		if _, _, err := s.MiyunCrawl.UpdateMiyunCrawlJobAndConnection(ctx, job, job.Version, connection, connection.Version); err != nil {
			return jobruntime.Result{}, err
		}
		return jobruntime.Result{}, jobruntime.DeferredError{AvailableAt: until}
	case crawler.YouShuAuthRequired:
		return s.persistMiyunAuthRequired(ctx, job, connection, string(upstream.Kind), upstream.Code)
	default:
		job.Status, job.LastErrorKind, job.LastErrorCode, job.UpdatedAt = MiyunCrawlJobFailed, string(upstream.Kind), upstream.Code, now
		if job.LastErrorCode == "" {
			job.LastErrorCode = strings.ToUpper(string(upstream.Kind))
		}
		if _, err := s.MiyunCrawl.UpdateMiyunCrawlJob(ctx, job, job.Version); err != nil {
			return jobruntime.Result{}, err
		}
		return jobruntime.Result{}, jobruntime.ExecutionError{JobError: contract.JobError{Code: "MIYUN_" + job.LastErrorCode, Message: "Miyun crawl failed", Retryable: false}}
	}
}

func (s Service) persistMiyunAuthRequired(ctx context.Context, job MiyunCrawlJob, connection MiyunConnection, kind, code string) (jobruntime.Result, error) {
	now := s.now()
	if strings.TrimSpace(code) == "" {
		code = "AUTH_REQUIRED"
	}
	job.Status, job.CooldownUntil, job.LastErrorKind, job.LastErrorCode, job.UpdatedAt = MiyunCrawlJobAuthRequired, nil, kind, code, now
	connection.Status, connection.CooldownUntil = MiyunConnectionAuthRequired, nil
	connection.LastErrorKind, connection.LastErrorCode, connection.LastErrorAt, connection.UpdatedAt = kind, code, &now, now
	if _, _, err := s.MiyunCrawl.UpdateMiyunCrawlJobAndConnection(ctx, job, job.Version, connection, connection.Version); err != nil {
		return jobruntime.Result{}, err
	}
	return jobruntime.Result{}, jobruntime.ExecutionError{JobError: contract.JobError{Code: "MIYUN_AUTH_REQUIRED", Message: "Miyun authentication requires operator refresh", Retryable: false}}
}

func terminalMiyunExecution(code string, _ error) error {
	return jobruntime.ExecutionError{JobError: contract.JobError{Code: code, Message: "Miyun job cannot continue", Retryable: false}}
}

func (s Service) miyunCooldown() time.Duration {
	if s.MiyunCooldown > 0 {
		return s.MiyunCooldown
	}
	return 5 * time.Minute
}

func stableMiyunID(prefix string, values ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return fmt.Sprintf("%s_%x", prefix, hash[:16])
}

func miyunPageFinished(page crawler.YouShuPage) bool {
	if len(page.Materials) == 0 || page.Limit <= 0 {
		return true
	}
	bound := page.Total
	if page.MaxTotal > 0 && (bound == 0 || page.MaxTotal < bound) {
		bound = page.MaxTotal
	}
	return bound <= 0 || page.Page*page.Limit >= bound
}

func timePointerUnlessZero(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}

func classifyMiyunImportError(err error) (string, string) {
	var download *crawler.YouShuDownloadError
	if errors.As(err, &download) {
		return "download", strings.ToUpper(string(download.Kind))
	}
	return "external_import", "EXTERNAL_IMPORT_FAILED"
}

func miyunTraceableSourceRef(material MiyunMaterial) string {
	if material.SourceRefStatus == "verified" && material.SourceRef != "" {
		return material.SourceRef
	}
	return "miyun://material/" + material.MiyunMaterialID
}
