package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"github.com/shikanon/cookies/internal/platform/assets"
	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/platform/ids"
	"github.com/shikanon/cookies/internal/platform/jobruntime"
)

const MaxDocumentBytes int64 = 10 * 1024 * 1024
const maxExtractedBytes int64 = 20 * 1024 * 1024

var ErrInvalidDocument = errors.New("invalid knowledge document")
var ErrExternalConfirmationRequired = errors.New("external research confirmation is required")
var ErrExternalRunnerUnavailable = errors.New("external research runner is not configured")
var ErrInvalidResearchRequest = errors.New("invalid research request")

type ProjectReader interface {
	GetContext(context.Context, contract.ActorContext, contract.ProjectID) (contract.ProjectContext, error)
}

type Service struct {
	DB                 *sql.DB
	Projects           ProjectReader
	Blobs              assets.BlobStore
	Scanner            assets.ContentScanner
	AssetsBucket       string
	Runner             ExternalResearchRunner
	SourceVerifier     ResearchSourceVerifier
	ResearchCompletion ResearchCompletionSink
	Scheduler          ResearchScheduler
	DocumentParser     DocumentParser
	DocumentScheduler  DocumentParseScheduler
	DocumentVision     DocumentVisionParser
	DocumentConverter  DocumentVisionInputConverter
	VisionScheduler    DocumentVisionFallbackScheduler
	VisionModelAlias   string
	DocumentEvents     DocumentEventSink
	JobProgress        jobruntime.ProgressReporter
	JobCanceller       jobruntime.Canceller
	Now                func() time.Time
	NewID              ids.Generator
}

type Document struct {
	DocumentParseState
	DocumentVisionState
	ID                string                  `json:"id"`
	OrganizationID    contract.OrganizationID `json:"organization_id"`
	ProjectID         contract.ProjectID      `json:"project_id"`
	Title             string                  `json:"title,omitempty"`
	SourceURI         string                  `json:"source_uri,omitempty"`
	SourceType        string                  `json:"source_type,omitempty"`
	ChunkCount        int                     `json:"chunk_count,omitempty"`
	ImportedBy        contract.Principal      `json:"imported_by,omitempty"`
	Filename          string                  `json:"filename"`
	MIMEType          string                  `json:"mime_type"`
	SizeBytes         int64                   `json:"size_bytes"`
	ContentSHA256     string                  `json:"content_sha256"`
	TextSHA256        string                  `json:"text_sha256"`
	ExtractedText     string                  `json:"extracted_text,omitempty"`
	Status            string                  `json:"status"`
	ParserCode        string                  `json:"parser_code,omitempty"`
	ParserVersion     string                  `json:"parser_version,omitempty"`
	ParseErrorCode    string                  `json:"parse_error_code,omitempty"`
	ParseErrorMessage string                  `json:"parse_error_message,omitempty"`
	ParseMetadata     json.RawMessage         `json:"parse_metadata,omitempty"`
	ParsedAt          *time.Time              `json:"parsed_at,omitempty"`
	CreatedBy         string                  `json:"created_by"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`
	Blob              assets.ObjectLocation   `json:"-"`
}

const DocumentParseContractVersion = "platform-document-parse/v2"

type DocumentParseState struct {
	ContractVersion    string          `json:"contract_version"`
	ParseStrategy      string          `json:"parse_strategy"`
	ParsePhase         string          `json:"parse_phase"`
	ParseProgress      *int            `json:"parse_progress"`
	ProgressKind       string          `json:"progress_kind"`
	ProcessedPages     *int            `json:"processed_pages"`
	TotalPages         *int            `json:"total_pages"`
	QualityScore       *float64        `json:"quality_score"`
	QualityTier        string          `json:"quality_tier"`
	FallbackReason     string          `json:"fallback_reason"`
	PreviewStatus      string          `json:"preview_status"`
	PageQualitySummary json.RawMessage `json:"page_quality_summary,omitempty"`
	HeartbeatAt        *time.Time      `json:"heartbeat_at"`
}

type DocumentVisionState struct {
	VisionFallbackStatus string     `json:"vision_fallback_status"`
	VisionAttemptID      string     `json:"-"`
	VisionSelectedPages  []int      `json:"vision_selected_pages"`
	VisionCompletedPages []int      `json:"vision_completed_pages"`
	VisionModelAlias     string     `json:"vision_model_alias,omitempty"`
	VisionRouteRevision  string     `json:"vision_route_revision_id,omitempty"`
	VisionModelVersion   string     `json:"vision_model_version,omitempty"`
	VisionErrorCode      string     `json:"vision_error_code,omitempty"`
	VisionErrorMessage   string     `json:"vision_error_message,omitempty"`
	VisionStartedAt      *time.Time `json:"vision_started_at,omitempty"`
	VisionCompletedAt    *time.Time `json:"vision_completed_at,omitempty"`
}

type ResearchRequest struct {
	Mode             string                `json:"mode"`
	RunMode          string                `json:"run_mode,omitempty"`
	Category         string                `json:"category,omitempty"`
	Purpose          string                `json:"purpose,omitempty"`
	SourceRef        *contract.ResourceRef `json:"source_ref,omitempty"`
	Query            string                `json:"query"`
	DocumentIDs      []string              `json:"document_ids"`
	DisclosedFields  []string              `json:"disclosed_fields"`
	Confirmed        bool                  `json:"confirmed"`
	InputSnapshotRef string                `json:"input_snapshot_ref,omitempty"`
	InputSnapshot    json.RawMessage       `json:"input_snapshot,omitempty"`
}

type ExternalResearchInput struct {
	OrganizationID contract.OrganizationID  `json:"organization_id"`
	ProjectID      contract.ProjectID       `json:"project_id"`
	Mode           string                   `json:"mode"`
	Category       string                   `json:"category"`
	Purpose        string                   `json:"purpose"`
	Query          string                   `json:"query"`
	Documents      []ExternalDocument       `json:"documents"`
	ResearchRunID  string                   `json:"research_run_id,omitempty"`
	RunMode        string                   `json:"run_mode,omitempty"`
	Round          int                      `json:"round,omitempty"`
	MaxRounds      int                      `json:"max_rounds,omitempty"`
	PriorFindings  []ExternalFindingSummary `json:"prior_findings,omitempty"`
	OpenGaps       []string                 `json:"open_gaps,omitempty"`
}

type ExternalDocument struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type ExternalResearchResult struct {
	Title            string                    `json:"title"`
	SourceURL        string                    `json:"source_url,omitempty"`
	Content          string                    `json:"content"`
	Citations        []string                  `json:"citations"`
	Sources          []ExternalResearchSource  `json:"sources,omitempty"`
	ProviderCode     string                    `json:"provider_code,omitempty"`
	ModelVersion     string                    `json:"model_version,omitempty"`
	ProviderResponse string                    `json:"provider_response_id,omitempty"`
	Usage            *ResearchUsage            `json:"usage,omitempty"`
	Findings         []ExternalResearchFinding `json:"findings,omitempty"`
	Coverage         map[string]bool           `json:"coverage,omitempty"`
	OpenGaps         []string                  `json:"open_gaps,omitempty"`
	ActionSummary    string                    `json:"action_summary,omitempty"`
	RecommendedStop  bool                      `json:"recommended_stop,omitempty"`
}

type ExternalFindingSummary struct {
	Claim      string `json:"claim"`
	Status     string `json:"status"`
	TargetPath string `json:"target_path"`
}

type ExternalResearchEvidence struct {
	URL     string `json:"url"`
	Excerpt string `json:"excerpt"`
}

type ExternalResearchFinding struct {
	Claim               string                     `json:"claim"`
	TimeScope           string                     `json:"time_scope"`
	Confidence          string                     `json:"confidence"`
	TargetArtifact      string                     `json:"target_artifact"`
	TargetFieldPath     string                     `json:"target_field_path"`
	Implication         string                     `json:"implication"`
	ProposedValue       json.RawMessage            `json:"proposed_value,omitempty"`
	SupportingEvidence  []ExternalResearchEvidence `json:"supporting_evidence"`
	ConflictingEvidence []ExternalResearchEvidence `json:"conflicting_evidence"`
}

type ExternalResearchSource struct {
	SourceClass     string          `json:"source_class"`
	MediaType       string          `json:"media_type"`
	Title           string          `json:"title"`
	URL             string          `json:"url"`
	PublishedAt     *time.Time      `json:"published_at,omitempty"`
	StartIndex      int             `json:"start_index,omitempty"`
	EndIndex        int             `json:"end_index,omitempty"`
	ProviderLocator json.RawMessage `json:"provider_locator,omitempty"`
}

type ResearchUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type ExternalResearchRunner interface {
	Run(context.Context, ExternalResearchInput) ([]ExternalResearchResult, error)
}

// ResearchSourceVerifier reads untrusted public source content through a
// hardened transport. A source is never promoted merely because a model cited
// its URL.
type ResearchSourceVerifier interface {
	Verify(context.Context, string, string) (VerifiedResearchSource, error)
}

type ResearchCompletionSink interface {
	OnResearchCompleted(context.Context, ResearchRun) error
}

type VerifiedResearchSource struct {
	ContentHash  string
	ExcerptFound bool
}

type ResearchScheduler interface {
	Schedule(context.Context, ResearchRun) error
}

type ResearchRetryScheduler interface {
	ScheduleResearchRetry(context.Context, ResearchRun) error
}

type DocumentParseScheduler interface {
	ScheduleDocumentParse(context.Context, Document) error
}

type DocumentParseRetryScheduler interface {
	ScheduleDocumentParseRetry(context.Context, Document) error
}

type ResearchRun struct {
	ContractVersion    string                  `json:"contract_version"`
	ID                 string                  `json:"id"`
	OrganizationID     contract.OrganizationID `json:"organization_id"`
	ProjectID          contract.ProjectID      `json:"project_id"`
	Mode               string                  `json:"mode"`
	RunMode            string                  `json:"run_mode"`
	Category           string                  `json:"category"`
	Purpose            string                  `json:"purpose"`
	SourceRef          *contract.ResourceRef   `json:"source_ref,omitempty"`
	Query              string                  `json:"query"`
	DocumentIDs        []string                `json:"document_ids"`
	DisclosedFields    []string                `json:"disclosed_fields"`
	DisclosedChunkIDs  []string                `json:"disclosed_chunk_ids"`
	Status             string                  `json:"status"`
	CurrentRound       int                     `json:"current_round"`
	MaxRounds          int                     `json:"max_rounds"`
	TimeBudgetSeconds  int                     `json:"time_budget_seconds"`
	TokenBudget        int64                   `json:"token_budget"`
	InputSnapshotRef   string                  `json:"input_snapshot_ref"`
	InputSnapshotHash  contract.ContentHash    `json:"input_snapshot_hash"`
	InputSnapshot      json.RawMessage         `json:"-"`
	Coverage           map[string]bool         `json:"coverage"`
	OpenGaps           []string                `json:"open_gaps"`
	StopReason         string                  `json:"stop_reason"`
	HeartbeatAt        *time.Time              `json:"heartbeat_at"`
	ReportArtifactID   *string                 `json:"report_artifact_id"`
	StartedAt          *time.Time              `json:"started_at,omitempty"`
	CompletedAt        *time.Time              `json:"completed_at,omitempty"`
	ConfirmedBy        string                  `json:"confirmed_by"`
	ConfirmedAt        time.Time               `json:"confirmed_at"`
	ErrorCode          string                  `json:"error_code,omitempty"`
	ErrorMessage       string                  `json:"error_message,omitempty"`
	ProviderCode       string                  `json:"provider_code,omitempty"`
	ModelVersion       string                  `json:"model_version,omitempty"`
	ProviderResponseID string                  `json:"provider_response_id,omitempty"`
	Usage              *ResearchUsage          `json:"usage,omitempty"`
	Artifacts          []ResearchArtifact      `json:"artifacts"`
	Iterations         []ResearchIteration     `json:"iterations"`
	Findings           []ResearchFinding       `json:"findings"`
	CreatedAt          time.Time               `json:"created_at"`
	UpdatedAt          time.Time               `json:"updated_at"`
}

type ResearchIteration struct {
	ID            string               `json:"id"`
	ResearchRunID string               `json:"research_run_id"`
	Round         int                  `json:"round"`
	Status        string               `json:"status"`
	Objective     string               `json:"objective"`
	Query         string               `json:"query"`
	ActionSummary string               `json:"action_summary"`
	SourceIDs     []string             `json:"source_ids"`
	ArtifactIDs   []string             `json:"artifact_ids"`
	FindingIDs    []string             `json:"finding_ids"`
	Coverage      map[string]bool      `json:"coverage"`
	OpenGaps      []string             `json:"open_gaps"`
	Usage         *ResearchUsage       `json:"usage,omitempty"`
	InputHash     contract.ContentHash `json:"input_hash"`
	OutputHash    contract.ContentHash `json:"output_hash"`
	ErrorCode     string               `json:"error_code,omitempty"`
	ErrorMessage  string               `json:"error_message,omitempty"`
	StartedAt     time.Time            `json:"started_at"`
	CompletedAt   *time.Time           `json:"completed_at,omitempty"`
}

type ResearchFindingTarget struct {
	Artifact  string `json:"artifact"`
	FieldPath string `json:"field_path"`
}

type ResearchFinding struct {
	ContractVersion      string                `json:"contract_version"`
	ID                   string                `json:"id"`
	ResearchRunID        string                `json:"research_run_id,omitempty"`
	Claim                string                `json:"claim"`
	Status               string                `json:"status"`
	TimeScope            string                `json:"time_scope"`
	Confidence           string                `json:"confidence"`
	SupportingSourceIDs  []string              `json:"supporting_source_ids"`
	ConflictingSourceIDs []string              `json:"conflicting_source_ids"`
	Target               ResearchFindingTarget `json:"target"`
	Implication          string                `json:"implication"`
	ProposedValue        json.RawMessage       `json:"proposed_value,omitempty"`
	Round                int                   `json:"round"`
	ContentHash          contract.ContentHash  `json:"content_hash"`
}

type ResearchArtifact struct {
	ID             string                  `json:"id"`
	OrganizationID contract.OrganizationID `json:"organization_id"`
	ProjectID      contract.ProjectID      `json:"project_id"`
	ResearchRunID  string                  `json:"research_run_id"`
	SourceType     string                  `json:"source_type"`
	Category       string                  `json:"category"`
	Title          string                  `json:"title"`
	SourceURL      string                  `json:"source_url,omitempty"`
	Content        string                  `json:"content"`
	Citations      []string                `json:"citations"`
	Sources        []ResearchSource        `json:"sources"`
	ContentHash    string                  `json:"content_hash"`
	CreatedAt      time.Time               `json:"created_at"`
}

type ResearchSource struct {
	ID                 string                  `json:"id"`
	OrganizationID     contract.OrganizationID `json:"organization_id"`
	ProjectID          contract.ProjectID      `json:"project_id"`
	ResearchRunID      string                  `json:"research_run_id"`
	SourceClass        string                  `json:"source_class"`
	MediaType          string                  `json:"media_type"`
	Title              string                  `json:"title"`
	URL                string                  `json:"url"`
	CanonicalURL       string                  `json:"canonical_url"`
	Domain             string                  `json:"domain"`
	PublishedAt        *time.Time              `json:"published_at,omitempty"`
	RetrievedAt        time.Time               `json:"retrieved_at"`
	VerificationStatus string                  `json:"verification_status"`
	ContentHash        string                  `json:"content_hash"`
	StartIndex         int                     `json:"start_index"`
	EndIndex           int                     `json:"end_index"`
	SupportLevel       string                  `json:"support_level"`
	ProviderLocator    json.RawMessage         `json:"provider_locator,omitempty"`
}

type Reference struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Content     string   `json:"content"`
	ContentHash string   `json:"content_hash"`
	Citations   []string `json:"citations,omitempty"`
}

func (s Service) CreateDocument(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, filename, declaredMIME string, source io.Reader, size int64) (Document, error) {
	if s.DB == nil || s.Projects == nil || s.Blobs == nil || s.Scanner == nil {
		return Document{}, fmt.Errorf("knowledge service dependencies are incomplete")
	}
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return Document{}, err
	}
	filename = strings.TrimSpace(filepath.Base(filename))
	extension := strings.ToLower(filepath.Ext(filename))
	if filename == "" || len(filename) > 512 ||
		!supportedDocumentExtension(extension) ||
		size < 1 || size > MaxDocumentBytes {
		return Document{}, ErrInvalidDocument
	}
	content, err := io.ReadAll(io.LimitReader(source, MaxDocumentBytes+1))
	if err != nil || int64(len(content)) != size || int64(len(content)) > MaxDocumentBytes {
		return Document{}, ErrInvalidDocument
	}
	if err := s.Scanner.Scan(ctx, bytes.NewReader(content)); err != nil {
		return Document{}, err
	}
	if declaredMIME != "" && !allowedMIME(extension, declaredMIME) {
		return Document{}, ErrInvalidDocument
	}
	if extension == ".pdf" && !validPDFContainer(content) {
		return Document{}, ErrInvalidDocument
	}
	mimeType := defaultDocumentMIME(extension)
	parseStrategy := documentParseStrategy(extension)
	asyncParse := parseStrategy != "text_native"
	if asyncParse && (s.DocumentParser == nil || s.DocumentScheduler == nil) {
		return Document{}, fmt.Errorf("document parser and scheduler are required for %s files", extension)
	}
	if extension == ".docx" && asyncParse {
		if _, _, err := extractDocument(extension, content); err != nil {
			return Document{}, err
		}
	}
	contentSum := sha256.Sum256(content)
	contentHash := hex.EncodeToString(contentSum[:])
	if asyncParse {
		existing, err := scanDocument(s.DB.QueryRowContext(ctx, documentSelect+`
			WHERE organization_id = ? AND project_id = ? AND content_sha256 = ? AND status IN ('ready', 'partial')
			ORDER BY parsed_at DESC, created_at DESC LIMIT 1`,
			actor.OrganizationID, projectID, contentHash,
		))
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Document{}, err
		}
	}
	extracted := ""
	if !asyncParse {
		var err error
		extracted, mimeType, err = extractDocument(extension, content)
		if err != nil {
			return Document{}, err
		}
	}
	id, err := s.newID("knowledgedoc")
	if err != nil {
		return Document{}, err
	}
	textSum := sha256.Sum256([]byte(extracted))
	now := s.now()
	key := knowledgeDocumentObjectPrefix(actor.OrganizationID, projectID, id) + "source" + extension
	object, err := s.Blobs.Put(ctx, s.AssetsBucket, key, bytes.NewReader(content), size, mimeType)
	if err != nil {
		return Document{}, err
	}
	if object.Bucket != s.AssetsBucket || object.Key != key || object.SizeBytes != size {
		if knowledgeDocumentLocationInScope(actor.OrganizationID, projectID, id, object.ObjectLocation, s.AssetsBucket) {
			_ = s.Blobs.Delete(ctx, object.ObjectLocation)
		}
		return Document{}, fmt.Errorf("document blob store returned an unexpected object location")
	}
	document := Document{
		DocumentParseState: DocumentParseState{
			ContractVersion: DocumentParseContractVersion, ParseStrategy: parseStrategy,
			ParsePhase: "queued", ParseProgress: intPointer(0), ProgressKind: "milestone",
			QualityTier: "unknown", PreviewStatus: "building", HeartbeatAt: &now,
		},
		DocumentVisionState: DocumentVisionState{
			VisionFallbackStatus: "not_requested", VisionSelectedPages: []int{}, VisionCompletedPages: []int{},
		},
		ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID,
		Title: filename, SourceType: "docs",
		Filename: filename, MIMEType: mimeType, SizeBytes: size,
		ContentSHA256: contentHash, TextSHA256: hex.EncodeToString(textSum[:]),
		ExtractedText: extracted, Status: "parse_queued", CreatedBy: actor.Principal.ID,
		CreatedAt: now, UpdatedAt: now, Blob: object.ObjectLocation,
	}
	if !asyncParse {
		document.Status = "parsing"
		document.ParsePhase = "extracting"
		document.ParseProgress = intPointer(40)
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO platform_knowledge_documents
		(id, organization_id, project_id, title, source_uri, source_type, chunk_count,
		 filename, mime_type, size_bytes, content_sha256,
		 text_sha256, extracted_text, object_provider, object_bucket, object_key,
		 object_version_id, object_etag, status, parse_strategy, parse_phase, parse_progress,
		 progress_kind, processed_pages, total_pages, quality_score, quality_tier,
		 fallback_reason, preview_status, page_quality_summary, heartbeat_at,
		 parser_code, parser_version, parse_error_code, parse_error_message,
		 created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		document.ID, document.OrganizationID, document.ProjectID, document.Title, nil,
		document.SourceType, document.ChunkCount, document.Filename, document.MIMEType,
		document.SizeBytes, document.ContentSHA256, document.TextSHA256, document.ExtractedText,
		object.Provider, object.Bucket, object.Key, object.VersionID, object.ETag, document.Status,
		document.ParseStrategy, document.ParsePhase, document.ParseProgress, document.ProgressKind,
		document.ProcessedPages, document.TotalPages, document.QualityScore, document.QualityTier,
		document.FallbackReason, document.PreviewStatus, nullableJSON(document.PageQualitySummary), document.HeartbeatAt,
		nullable(document.ParserCode), nullable(document.ParserVersion),
		nullable(document.ParseErrorCode), nullable(document.ParseErrorMessage),
		document.CreatedBy, document.CreatedAt, document.UpdatedAt)
	if err != nil {
		_ = s.Blobs.Delete(ctx, object.ObjectLocation)
		return Document{}, err
	}
	if asyncParse {
		if err := s.DocumentScheduler.ScheduleDocumentParse(ctx, document); err != nil {
			_, _ = s.DB.ExecContext(ctx, `DELETE FROM platform_knowledge_documents
				WHERE organization_id = ? AND project_id = ? AND id = ?`,
				document.OrganizationID, document.ProjectID, document.ID)
			_ = s.Blobs.Delete(ctx, object.ObjectLocation)
			return Document{}, err
		}
		s.recordDocumentEvent(ctx, document, DocumentEventParseStarted, "accepted", document.Status, nil)
		return document, nil
	}
	parsed := ParsedDocument{
		Text: extracted, MIMEType: mimeType, ParserCode: "native", ParserVersion: "1",
		Metadata: json.RawMessage(`{}`),
	}
	chunks := chunksForParsedDocument(document, parsed)
	if len(chunks) == 0 {
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM platform_knowledge_documents
			WHERE organization_id = ? AND project_id = ? AND id = ?`,
			document.OrganizationID, document.ProjectID, document.ID)
		_ = s.Blobs.Delete(ctx, object.ObjectLocation)
		return Document{}, ErrInvalidDocument
	}
	quality := evaluateParsedDocumentQuality(parsed, chunks)
	if err := s.persistParsedDocument(ctx, document, parsed, chunks, quality); err != nil {
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM platform_knowledge_documents
			WHERE organization_id = ? AND project_id = ? AND id = ?`,
			document.OrganizationID, document.ProjectID, document.ID)
		_ = s.Blobs.Delete(ctx, object.ObjectLocation)
		return Document{}, err
	}
	return s.GetDocument(ctx, actor, projectID, document.ID)
}

func supportedDocumentExtension(extension string) bool {
	switch extension {
	case ".md", ".txt", ".html", ".htm", ".docx", ".xlsx", ".pdf", ".ppt", ".pptx":
		return true
	default:
		return false
	}
}

func validPDFContainer(content []byte) bool {
	if len(content) < 12 || !bytes.HasPrefix(content, []byte("%PDF-")) {
		return false
	}
	tail := content
	if len(tail) > 2048 {
		tail = tail[len(tail)-2048:]
	}
	return bytes.Contains(tail, []byte("%%EOF"))
}

func (s Service) ImportDocument(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, request ImportDocumentRequest) (Document, error) {
	if err := request.Validate(); err != nil {
		return Document{}, err
	}
	filename := strings.TrimSpace(request.Title)
	if !strings.HasSuffix(strings.ToLower(filename), ".md") {
		filename += ".md"
	}
	content := []byte(request.Text)
	document, err := s.CreateDocument(ctx, actor, projectID, filename, "text/markdown", bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return Document{}, err
	}
	document.Title = strings.TrimSpace(request.Title)
	document.SourceURI = strings.TrimSpace(request.SourceURI)
	document.SourceType = normalizedSourceType(request.SourceType)
	document.ChunkCount = max(document.ChunkCount, 1)
	document.ImportedBy = actor.Principal
	_, err = s.DB.ExecContext(ctx, `UPDATE platform_knowledge_documents
		SET title = ?, source_uri = ?, source_type = ?, chunk_count = ?, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ?`,
		document.Title, nullable(document.SourceURI), document.SourceType, document.ChunkCount,
		document.UpdatedAt, actor.OrganizationID, projectID, document.ID)
	if err != nil {
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM platform_knowledge_documents
			WHERE organization_id = ? AND project_id = ? AND id = ?`,
			actor.OrganizationID, projectID, document.ID)
		_ = s.Blobs.Delete(ctx, document.Blob)
		return Document{}, err
	}
	return document, nil
}

func (s Service) ListDocuments(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, limit int) ([]Document, error) {
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, documentSelect+` WHERE organization_id = ? AND project_id = ?
		ORDER BY created_at DESC LIMIT ?`, actor.OrganizationID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Document{}
	for rows.Next() {
		value, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		value.ExtractedText = ""
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s Service) Search(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, request SearchRequest) ([]SearchResult, error) {
	if request.Limit == 0 {
		request.Limit = 10
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	return s.searchChunks(ctx, actor.OrganizationID, projectID, nil, request.Query, request.Limit)
}

func (s Service) GetDocument(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id string) (Document, error) {
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return Document{}, err
	}
	value, err := scanDocument(s.DB.QueryRowContext(ctx, documentSelect+` WHERE organization_id = ? AND project_id = ? AND id = ?`,
		actor.OrganizationID, projectID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	return value, err
}

// OpenDocumentOriginal reopens the immutable source bytes for a document that
// belongs to the caller's project. The returned stream is owned by the caller.
// Object identity, metadata, byte length, and SHA-256 are checked before it is
// handed off so a stale or substituted blob cannot be consumed as the original.
func (s Service) ExtractDocumentMedia(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id string) ([]ExtractedDocumentMedia, error) {
	document, err := s.GetDocument(ctx, actor, projectID, id)
	if err != nil {
		return nil, err
	}
	if document.MIMEType != "application/pdf" || s.Blobs == nil {
		return nil, ErrInvalidDocument
	}
	extractor, ok := s.DocumentParser.(DocumentMediaExtractor)
	if !ok {
		return nil, fmt.Errorf("document media extractor is unavailable")
	}
	stream, info, err := s.Blobs.Open(ctx, document.Blob)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	return extractor.ExtractMedia(ctx, DocumentParseRequest{
		Filename: document.Filename, MIMEType: document.MIMEType, Size: info.SizeBytes, Source: stream,
	})
}

func (s Service) OpenDocumentOriginal(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id string) (io.ReadCloser, Document, error) {
	if s.Blobs == nil {
		return nil, Document{}, fmt.Errorf("knowledge blob store is unavailable")
	}
	document, err := s.GetDocument(ctx, actor, projectID, id)
	if err != nil {
		return nil, Document{}, err
	}
	stream, info, err := s.Blobs.Open(ctx, document.Blob)
	if err != nil {
		return nil, Document{}, err
	}
	content, verifyErr := verifyDocumentOriginal(stream, info, document)
	closeErr := stream.Close()
	if verifyErr != nil {
		return nil, Document{}, ErrInvalidDocument
	}
	if closeErr != nil {
		return nil, Document{}, closeErr
	}
	return io.NopCloser(bytes.NewReader(content)), document, nil
}

// OpenDocumentOriginalStream returns the authorized immutable object without
// buffering it. Consumers that need an end-to-end hash check must verify the
// returned bytes while copying; this boundary still validates Project scope,
// object identity, length, and MIME type before exposing the stream.
func (s Service) OpenDocumentOriginalStream(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id string) (io.ReadCloser, Document, error) {
	if s.Blobs == nil {
		return nil, Document{}, fmt.Errorf("knowledge blob store is unavailable")
	}
	document, err := s.GetDocument(ctx, actor, projectID, id)
	if err != nil {
		return nil, Document{}, err
	}
	stream, info, err := s.Blobs.Open(ctx, document.Blob)
	if err != nil {
		return nil, Document{}, err
	}
	if info.SizeBytes != document.SizeBytes || info.ObjectLocation != document.Blob ||
		(strings.TrimSpace(info.MIMEType) != "" && !strings.EqualFold(strings.TrimSpace(strings.Split(info.MIMEType, ";")[0]), strings.TrimSpace(strings.Split(document.MIMEType, ";")[0]))) {
		_ = stream.Close()
		return nil, Document{}, ErrInvalidDocument
	}
	return &verifiedDocumentStream{ReadCloser: stream, remaining: document.SizeBytes, expectedSHA256: document.ContentSHA256, hash: sha256.New()}, document, nil
}

type verifiedDocumentStream struct {
	io.ReadCloser
	remaining      int64
	expectedSHA256 string
	hash           hash.Hash
	verified       bool
}

func (s *verifiedDocumentStream) Read(p []byte) (int, error) {
	n, err := s.ReadCloser.Read(p)
	if n > 0 {
		s.remaining -= int64(n)
		_, _ = s.hash.Write(p[:n])
	}
	if err == io.EOF {
		s.verified = true
		if s.remaining != 0 || !strings.EqualFold(hex.EncodeToString(s.hash.Sum(nil)), s.expectedSHA256) {
			return n, ErrInvalidDocument
		}
	}
	return n, err
}

func verifyDocumentOriginal(stream io.Reader, info assets.ObjectInfo, document Document) ([]byte, error) {
	if info.SizeBytes != document.SizeBytes || info.ObjectLocation != document.Blob {
		return nil, ErrInvalidDocument
	}
	openedMIME := strings.ToLower(strings.TrimSpace(strings.Split(info.MIMEType, ";")[0]))
	recordedMIME := strings.ToLower(strings.TrimSpace(strings.Split(document.MIMEType, ";")[0]))
	if openedMIME != "" && openedMIME != recordedMIME {
		return nil, ErrInvalidDocument
	}
	content, err := io.ReadAll(io.LimitReader(stream, document.SizeBytes+1))
	if err != nil || int64(len(content)) != document.SizeBytes {
		return nil, ErrInvalidDocument
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != document.ContentSHA256 {
		return nil, ErrInvalidDocument
	}
	return content, nil
}

func (s Service) GetReference(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id string) (Reference, error) {
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return Reference{}, err
	}
	document, err := scanDocument(s.DB.QueryRowContext(ctx, documentSelect+` WHERE organization_id = ? AND project_id = ? AND id = ?`,
		actor.OrganizationID, projectID, id))
	if err == nil {
		return Reference{
			ID: document.ID, Kind: "document", Title: document.Filename,
			Content: document.ExtractedText, ContentHash: document.TextSHA256,
		}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Reference{}, err
	}
	var artifact ResearchArtifact
	var sourceURL sql.NullString
	var citations []byte
	err = s.DB.QueryRowContext(ctx, `SELECT id, organization_id, project_id, research_run_id,
		source_type, title, source_url, content, citations, content_hash, created_at
		FROM platform_research_artifacts
		WHERE organization_id = ? AND project_id = ? AND id = ?`,
		actor.OrganizationID, projectID, id).Scan(
		&artifact.ID, &artifact.OrganizationID, &artifact.ProjectID, &artifact.ResearchRunID,
		&artifact.SourceType, &artifact.Title, &sourceURL, &artifact.Content, &citations,
		&artifact.ContentHash, &artifact.CreatedAt,
	)
	if err != nil {
		return Reference{}, err
	}
	artifact.SourceURL = sourceURL.String
	if err := json.Unmarshal(citations, &artifact.Citations); err != nil {
		return Reference{}, err
	}
	return Reference{
		ID: artifact.ID, Kind: "research_artifact", Title: artifact.Title,
		Content: artifact.Content, ContentHash: artifact.ContentHash,
		Citations: append([]string(nil), artifact.Citations...),
	}, nil
}

func (s Service) RunResearch(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, request ResearchRequest) (ResearchRun, error) {
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return ResearchRun{}, err
	}
	request.Mode = strings.ToLower(strings.TrimSpace(request.Mode))
	if request.Mode == "" {
		request.Mode = "web"
	}
	if !validResearchCategory(request.Category, true) {
		return ResearchRun{}, ErrInvalidResearchRequest
	}
	request.Category = normalizedResearchCategory(request.Category)
	request.Query = strings.TrimSpace(request.Query)
	if !request.Confirmed {
		return ResearchRun{}, ErrExternalConfirmationRequired
	}
	var err error
	request.Purpose, request.SourceRef, err = validateResearchContext(request.Purpose, request.SourceRef)
	if err != nil {
		return ResearchRun{}, err
	}
	request.DocumentIDs, request.DisclosedFields, err = validateResearchRequest(request)
	if err != nil {
		return ResearchRun{}, err
	}
	for _, id := range request.DocumentIDs {
		value, err := s.GetDocument(ctx, actor, projectID, id)
		if err != nil {
			return ResearchRun{}, err
		}
		if value.Status != "ready" || value.ChunkCount < 1 {
			return ResearchRun{}, ErrInvalidResearchRequest
		}
	}
	id, err := s.newID("researchrun")
	if err != nil {
		return ResearchRun{}, err
	}
	now := s.now()
	runMode := strings.ToLower(strings.TrimSpace(request.RunMode))
	if runMode == "" {
		if request.Purpose == "deep_research" {
			runMode = "deep"
		} else {
			runMode = "quick"
		}
	}
	if (request.Purpose == "deep_research" && runMode != "deep") ||
		(request.Purpose == "conversation_web_search" && runMode != "quick") {
		return ResearchRun{}, ErrInvalidResearchRequest
	}
	snapshotRef := strings.TrimSpace(request.InputSnapshotRef)
	snapshot := append(json.RawMessage(nil), request.InputSnapshot...)
	if runMode == "deep" {
		if snapshotRef == "" || len(snapshot) == 0 || len(snapshot) > 64*1024 || !json.Valid(snapshot) {
			return ResearchRun{}, ErrInvalidResearchRequest
		}
	} else {
		snapshotRef = "strategy_message:" + nullableResourceIDString(request.SourceRef)
		snapshot, _ = json.Marshal(map[string]any{
			"query": request.Query, "source_ref": request.SourceRef,
			"document_ids": request.DocumentIDs,
		})
	}
	snapshotHash, err := contract.NewContentHash(json.RawMessage(snapshot))
	if err != nil {
		return ResearchRun{}, err
	}
	maxRounds, timeBudget, tokenBudget := 1, 120, int64(12000)
	if runMode == "deep" {
		maxRounds, timeBudget, tokenBudget = 6, 900, 72000
	}
	status := "queued"
	if s.Scheduler == nil {
		status = "planning"
	}
	run := ResearchRun{
		ContractVersion: "strategy-research-run/v2",
		ID:              id, OrganizationID: actor.OrganizationID, ProjectID: projectID,
		Mode: request.Mode, RunMode: runMode, Category: request.Category, Purpose: request.Purpose, SourceRef: request.SourceRef, Query: request.Query,
		DocumentIDs:     append([]string(nil), request.DocumentIDs...),
		DisclosedFields: append([]string(nil), request.DisclosedFields...), Status: status,
		CurrentRound: 0, MaxRounds: maxRounds, TimeBudgetSeconds: timeBudget, TokenBudget: tokenBudget,
		InputSnapshotRef: snapshotRef, InputSnapshotHash: snapshotHash, InputSnapshot: snapshot,
		Coverage: map[string]bool{}, OpenGaps: []string{}, StopReason: "",
		ConfirmedBy: actor.Principal.ID, ConfirmedAt: now, Artifacts: []ResearchArtifact{},
		Iterations: []ResearchIteration{}, Findings: []ResearchFinding{},
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO platform_research_runs
		(id, contract_version, organization_id, project_id, mode, run_mode, category, purpose, source_type, source_id,
		 query_text, document_ids, disclosed_fields, disclosed_chunk_ids, status, current_round, max_rounds,
		 time_budget_seconds, token_budget, input_snapshot_ref, input_snapshot_hash, input_snapshot_json,
		 coverage_json, open_gaps_json, stop_reason, confirmed_by, confirmed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.ContractVersion, run.OrganizationID, run.ProjectID, run.Mode, run.RunMode, run.Category, run.Purpose,
		nullableResourceType(run.SourceRef), nullableResourceID(run.SourceRef), run.Query, jsonBytes(run.DocumentIDs),
		jsonBytes(run.DisclosedFields), jsonBytes(run.DisclosedChunkIDs),
		run.Status, run.CurrentRound, run.MaxRounds, run.TimeBudgetSeconds, run.TokenBudget,
		run.InputSnapshotRef, run.InputSnapshotHash, []byte(run.InputSnapshot), jsonBytes(run.Coverage),
		jsonBytes(run.OpenGaps), run.StopReason, run.ConfirmedBy, run.ConfirmedAt, now, now); err != nil {
		return ResearchRun{}, err
	}
	if s.Runner == nil {
		run.Status, run.ErrorCode, run.ErrorMessage = "failed", "EXTERNAL_RUNNER_UNAVAILABLE", ErrExternalRunnerUnavailable.Error()
		run.StopReason = "runner_unavailable"
		run.UpdatedAt = s.now()
		_, _ = s.DB.ExecContext(ctx, `UPDATE platform_research_runs SET status = ?, error_code = ?,
			error_message = ?, stop_reason = ?, completed_at = ?, updated_at = ? WHERE organization_id = ? AND project_id = ? AND id = ?`,
			run.Status, run.ErrorCode, run.ErrorMessage, run.StopReason, run.UpdatedAt, run.UpdatedAt, run.OrganizationID, run.ProjectID, run.ID)
		return run, nil
	}
	if s.Scheduler != nil {
		if err := s.Scheduler.Schedule(ctx, run); err != nil {
			run.Status, run.ErrorCode, run.ErrorMessage = "failed", "RESEARCH_SCHEDULE_FAILED", "研究任务暂时无法进入执行队列"
			run.UpdatedAt = s.now()
			_, _ = s.DB.ExecContext(ctx, `UPDATE platform_research_runs SET status = ?, error_code = ?,
				error_message = ?, updated_at = ? WHERE organization_id = ? AND project_id = ? AND id = ?`,
				run.Status, run.ErrorCode, run.ErrorMessage, run.UpdatedAt, actor.OrganizationID, projectID, run.ID)
		}
		return run, nil
	}
	documents, err := s.selectResearchChunks(
		ctx, run.OrganizationID, run.ProjectID, run.DocumentIDs, run.Query,
	)
	if err != nil {
		return ResearchRun{}, err
	}
	if run.RunMode == "deep" {
		return s.executeDeepResearch(ctx, run, documents, nil, false)
	}
	return s.executeResearch(ctx, run, documents)
}

// RunConversationWebSearch executes the query before the owning conversation
// agent generates its answer. Unlike external research, it deliberately skips
// the standalone research scheduler: the durable AgentTask is already the
// orchestration boundary and must not answer until this run is terminal.
//
// A completed run is reused on AgentTask retry so a model failure after search
// does not issue the same paid web request again. A running run is either a
// legacy scheduled search (wait for it) or an interrupted inline run (resume
// it); the source-level unique key prevents concurrent duplicate creation.
func (s Service) RunConversationWebSearch(
	ctx context.Context,
	actor contract.ActorContext,
	projectID contract.ProjectID,
	messageID string,
	query string,
) (ResearchRun, error) {
	messageID = strings.TrimSpace(messageID)
	query = strings.TrimSpace(query)
	if messageID == "" || query == "" {
		return ResearchRun{}, ErrInvalidResearchRequest
	}
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return ResearchRun{}, err
	}
	existing, found, err := s.conversationWebSearchRun(ctx, actor.OrganizationID, projectID, messageID)
	if err != nil {
		return ResearchRun{}, err
	}
	if found {
		if strings.TrimSpace(existing.Query) != query {
			return ResearchRun{}, ErrInvalidResearchRequest
		}
		if researchStatusActive(existing.Status) {
			scheduled, err := s.conversationWebSearchHasScheduledJob(ctx, existing)
			if err != nil {
				return ResearchRun{}, err
			}
			if !scheduled {
				documents, err := s.selectResearchChunks(
					ctx, existing.OrganizationID, existing.ProjectID, existing.DocumentIDs, existing.Query,
				)
				if err != nil {
					return ResearchRun{}, err
				}
				resumed, err := s.executeResearch(ctx, existing, documents)
				if err != nil {
					return ResearchRun{}, err
				}
				return s.waitForConversationWebSearch(ctx, resumed)
			}
		}
		return s.waitForConversationWebSearch(ctx, existing)
	}

	inline := s
	inline.Scheduler = nil
	run, err := inline.RunResearch(ctx, actor, projectID, ResearchRequest{
		Mode: "web", Category: "general", Purpose: "conversation_web_search",
		SourceRef: &contract.ResourceRef{Type: "strategy_message", ID: messageID},
		Query:     query, DocumentIDs: []string{}, DisclosedFields: []string{"query"}, Confirmed: true,
	})
	if err != nil {
		var mysqlError *mysqlDriver.MySQLError
		if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
			existing, found, loadErr := s.conversationWebSearchRun(ctx, actor.OrganizationID, projectID, messageID)
			if loadErr != nil {
				return ResearchRun{}, loadErr
			}
			if found {
				return s.waitForConversationWebSearch(ctx, existing)
			}
		}
		return ResearchRun{}, err
	}
	if run.Status != "completed" || len(run.Artifacts) == 0 {
		return run, fmt.Errorf("conversation web search did not complete: %s", run.ErrorCode)
	}
	return run, nil
}

func (s Service) conversationWebSearchHasScheduledJob(ctx context.Context, run ResearchRun) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM platform_jobs
		WHERE organization_id = ? AND idempotency_key = ?`,
		run.OrganizationID, "knowledge_research_"+run.ID).Scan(&count)
	return count > 0, err
}

func (s Service) conversationWebSearchRun(
	ctx context.Context,
	organizationID contract.OrganizationID,
	projectID contract.ProjectID,
	messageID string,
) (ResearchRun, bool, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM platform_research_runs
		WHERE organization_id = ? AND project_id = ? AND purpose = 'conversation_web_search'
		  AND source_type = 'strategy_message' AND source_id = ?
		ORDER BY created_at DESC LIMIT 1`, organizationID, projectID, messageID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ResearchRun{}, false, nil
	}
	if err != nil {
		return ResearchRun{}, false, err
	}
	run, err := s.getResearchRun(ctx, organizationID, projectID, id)
	return run, err == nil, err
}

func (s Service) waitForConversationWebSearch(ctx context.Context, run ResearchRun) (ResearchRun, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		switch run.Status {
		case "completed":
			if len(run.Artifacts) == 0 {
				return run, fmt.Errorf("conversation web search returned no artifact")
			}
			return run, nil
		case "failed", "cancelled", "partially_completed":
			return run, fmt.Errorf("conversation web search did not complete: %s", run.ErrorCode)
		case "queued", "planning", "searching", "reading", "cross_checking", "drafting", "auditing":
		default:
			return run, fmt.Errorf("conversation web search has invalid status %q", run.Status)
		}
		select {
		case <-ctx.Done():
			return ResearchRun{}, ctx.Err()
		case <-ticker.C:
			var err error
			run, err = s.getResearchRun(ctx, run.OrganizationID, run.ProjectID, run.ID)
			if err != nil {
				return ResearchRun{}, err
			}
		}
	}
}

func (s Service) executeResearch(ctx context.Context, run ResearchRun, documents []ExternalDocument) (ResearchRun, error) {
	run.DisclosedChunkIDs = make([]string, 0, len(documents))
	for _, document := range documents {
		run.DisclosedChunkIDs = append(run.DisclosedChunkIDs, document.ID)
	}
	run.UpdatedAt = s.now()
	run.Status = "searching"
	if run.StartedAt == nil {
		started := run.UpdatedAt
		run.StartedAt = &started
	}
	heartbeat := run.UpdatedAt
	run.HeartbeatAt = &heartbeat
	if _, err := s.DB.ExecContext(ctx, `UPDATE platform_research_runs
		SET disclosed_chunk_ids = ?, status = 'searching', started_at = COALESCE(started_at, ?),
			heartbeat_at = ?, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ? AND status IN ('queued', 'planning', 'searching')`,
		jsonBytes(run.DisclosedChunkIDs), run.UpdatedAt, run.UpdatedAt, run.UpdatedAt,
		run.OrganizationID, run.ProjectID, run.ID); err != nil {
		return ResearchRun{}, err
	}
	results, err := s.Runner.Run(ctx, ExternalResearchInput{
		OrganizationID: run.OrganizationID,
		ProjectID:      run.ProjectID,
		Mode:           run.Mode,
		Category:       run.Category,
		Purpose:        run.Purpose,
		Query:          run.Query,
		Documents:      documents,
	})
	if err != nil {
		run.Status, run.ErrorCode, run.ErrorMessage = "failed", "EXTERNAL_RESEARCH_FAILED", "外部研究调用失败"
		run.StopReason = "provider_error"
		run.UpdatedAt = s.now()
		run.CompletedAt = &run.UpdatedAt
		_, _ = s.DB.ExecContext(ctx, `UPDATE platform_research_runs SET status = ?, error_code = ?,
			error_message = ?, stop_reason = ?, completed_at = ?, heartbeat_at = ?, updated_at = ?
			WHERE organization_id = ? AND project_id = ? AND id = ?`,
			run.Status, run.ErrorCode, run.ErrorMessage, run.StopReason, run.UpdatedAt, run.UpdatedAt,
			run.UpdatedAt, run.OrganizationID, run.ProjectID, run.ID)
		return run, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ResearchRun{}, err
	}
	defer tx.Rollback()
	for _, result := range results {
		artifact, err := s.insertArtifact(ctx, tx, run, result)
		if err != nil {
			return ResearchRun{}, err
		}
		run.Artifacts = append(run.Artifacts, artifact)
		if run.ProviderCode == "" {
			run.ProviderCode = strings.TrimSpace(result.ProviderCode)
			run.ModelVersion = strings.TrimSpace(result.ModelVersion)
			run.ProviderResponseID = strings.TrimSpace(result.ProviderResponse)
			run.Usage = result.Usage
		}
	}
	run.Status = "completed"
	run.StopReason = "quick_search_completed"
	run.UpdatedAt = s.now()
	run.CompletedAt = &run.UpdatedAt
	if _, err := tx.ExecContext(ctx, `UPDATE platform_research_runs SET status = ?,
		provider_code = ?, model_version = ?, provider_response_id = ?, usage_json = ?,
		stop_reason = ?, heartbeat_at = ?, completed_at = ?, updated_at = ?
		WHERE organization_id = ? AND project_id = ? AND id = ?`,
		run.Status, nullable(run.ProviderCode), nullable(run.ModelVersion),
		nullable(run.ProviderResponseID), nullableJSONValue(run.Usage), run.StopReason,
		run.UpdatedAt, run.UpdatedAt, run.UpdatedAt,
		run.OrganizationID, run.ProjectID, run.ID); err != nil {
		return ResearchRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return ResearchRun{}, err
	}
	return run, nil
}

func (s Service) GetResearchRun(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id string) (ResearchRun, error) {
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return ResearchRun{}, err
	}
	return s.getResearchRun(ctx, actor.OrganizationID, projectID, id)
}

func (s Service) ListResearchFindings(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, runID string) ([]ResearchFinding, error) {
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	if _, err := s.getResearchRun(ctx, actor.OrganizationID, projectID, runID); err != nil {
		return nil, err
	}
	return s.listResearchFindings(ctx, actor.OrganizationID, projectID, strings.TrimSpace(runID))
}

func (s Service) GetResearchReport(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, runID string) (ResearchArtifact, error) {
	run, err := s.GetResearchRun(ctx, actor, projectID, runID)
	if err != nil {
		return ResearchArtifact{}, err
	}
	if run.ReportArtifactID == nil {
		return ResearchArtifact{}, ErrNotFound
	}
	return s.GetResearchArtifact(ctx, actor, projectID, *run.ReportArtifactID)
}

func (s Service) ListResearchRuns(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, limit int) ([]ResearchRun, error) {
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.DB.QueryContext(ctx, researchRunSelect+`
		WHERE organization_id = ? AND project_id = ?
		ORDER BY created_at DESC LIMIT ?`, actor.OrganizationID, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []ResearchRun{}
	for rows.Next() {
		value, err := scanResearchRun(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range values {
		values[index].Artifacts, err = s.listResearchArtifacts(ctx, values[index].OrganizationID, values[index].ProjectID, values[index].ID)
		if err != nil {
			return nil, err
		}
		values[index].Iterations, err = s.listResearchIterations(ctx, values[index].OrganizationID, values[index].ProjectID, values[index].ID)
		if err != nil {
			return nil, err
		}
		values[index].Findings, err = s.listResearchFindings(ctx, values[index].OrganizationID, values[index].ProjectID, values[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

const researchRunSelect = `SELECT id, contract_version, organization_id, project_id, mode, run_mode, query_text,
	category, purpose, COALESCE(source_type, ''), COALESCE(source_id, ''), document_ids, disclosed_fields, disclosed_chunk_ids, status,
	current_round, max_rounds, time_budget_seconds, token_budget, input_snapshot_ref, input_snapshot_hash,
	COALESCE(input_snapshot_json, JSON_OBJECT()), coverage_json, open_gaps_json, stop_reason,
	heartbeat_at, report_artifact_id, started_at, completed_at, confirmed_by, confirmed_at,
	COALESCE(error_code, ''), COALESCE(error_message, ''),
	COALESCE(provider_code, ''), COALESCE(model_version, ''),
	COALESCE(provider_response_id, ''), usage_json, created_at, updated_at
	FROM platform_research_runs`

type researchRunScanner interface {
	Scan(...any) error
}

func scanResearchRun(scanner researchRunScanner) (ResearchRun, error) {
	var value ResearchRun
	var documentIDs, disclosedFields, disclosedChunkIDs, snapshot, coverage, openGaps []byte
	var usage []byte
	var sourceType, sourceID string
	var heartbeatAt, startedAt, completedAt sql.NullTime
	var reportArtifactID sql.NullString
	err := scanner.Scan(
		&value.ID, &value.ContractVersion, &value.OrganizationID, &value.ProjectID, &value.Mode, &value.RunMode, &value.Query,
		&value.Category, &value.Purpose, &sourceType, &sourceID, &documentIDs, &disclosedFields, &disclosedChunkIDs,
		&value.Status, &value.CurrentRound, &value.MaxRounds, &value.TimeBudgetSeconds, &value.TokenBudget,
		&value.InputSnapshotRef, &value.InputSnapshotHash, &snapshot, &coverage, &openGaps, &value.StopReason,
		&heartbeatAt, &reportArtifactID, &startedAt, &completedAt, &value.ConfirmedBy, &value.ConfirmedAt,
		&value.ErrorCode, &value.ErrorMessage, &value.ProviderCode, &value.ModelVersion,
		&value.ProviderResponseID, &usage, &value.CreatedAt, &value.UpdatedAt,
	)
	if err != nil {
		return ResearchRun{}, err
	}
	if sourceType != "" && sourceID != "" {
		value.SourceRef = &contract.ResourceRef{Type: sourceType, ID: sourceID}
	}
	if err := json.Unmarshal(documentIDs, &value.DocumentIDs); err != nil {
		return ResearchRun{}, err
	}
	if err := json.Unmarshal(disclosedFields, &value.DisclosedFields); err != nil {
		return ResearchRun{}, err
	}
	if err := json.Unmarshal(disclosedChunkIDs, &value.DisclosedChunkIDs); err != nil {
		return ResearchRun{}, err
	}
	value.InputSnapshot = append(json.RawMessage(nil), snapshot...)
	if err := json.Unmarshal(coverage, &value.Coverage); err != nil {
		return ResearchRun{}, err
	}
	if err := json.Unmarshal(openGaps, &value.OpenGaps); err != nil {
		return ResearchRun{}, err
	}
	value.HeartbeatAt = nullTimePointer(heartbeatAt)
	value.StartedAt = nullTimePointer(startedAt)
	value.CompletedAt = nullTimePointer(completedAt)
	if reportArtifactID.Valid {
		value.ReportArtifactID = &reportArtifactID.String
	}
	if len(usage) > 0 {
		value.Usage = &ResearchUsage{}
		if err := json.Unmarshal(usage, value.Usage); err != nil {
			return ResearchRun{}, err
		}
	}
	value.Artifacts = []ResearchArtifact{}
	value.Iterations = []ResearchIteration{}
	value.Findings = []ResearchFinding{}
	return value, nil
}

func (s Service) getResearchRun(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, id string) (ResearchRun, error) {
	value, err := scanResearchRun(s.DB.QueryRowContext(ctx, researchRunSelect+`
		WHERE organization_id = ? AND project_id = ? AND id = ?`,
		organizationID, projectID, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return ResearchRun{}, ErrNotFound
	}
	if err != nil {
		return ResearchRun{}, err
	}
	value.Artifacts, err = s.listResearchArtifacts(ctx, organizationID, projectID, value.ID)
	if err != nil {
		return ResearchRun{}, err
	}
	value.Iterations, err = s.listResearchIterations(ctx, organizationID, projectID, value.ID)
	if err != nil {
		return ResearchRun{}, err
	}
	value.Findings, err = s.listResearchFindings(ctx, organizationID, projectID, value.ID)
	if err != nil {
		return ResearchRun{}, err
	}
	return value, nil
}

func (s Service) listResearchArtifacts(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, runID string) ([]ResearchArtifact, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, organization_id, project_id, research_run_id,
		source_type, category, title, COALESCE(source_url, ''), content, citations, content_hash, created_at
		FROM platform_research_artifacts
		WHERE organization_id = ? AND project_id = ? AND research_run_id = ?
		ORDER BY created_at ASC`, organizationID, projectID, runID)
	if err != nil {
		return nil, err
	}
	values := []ResearchArtifact{}
	for rows.Next() {
		var value ResearchArtifact
		var citations []byte
		if err := rows.Scan(
			&value.ID, &value.OrganizationID, &value.ProjectID, &value.ResearchRunID,
			&value.SourceType, &value.Category, &value.Title, &value.SourceURL, &value.Content, &citations,
			&value.ContentHash, &value.CreatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(citations, &value.Citations); err != nil {
			return nil, err
		}
		value.Sources = []ResearchSource{}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range values {
		values[index].Sources, err = s.listArtifactSources(
			ctx, string(organizationID), string(projectID), values[index].ID,
		)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (s Service) ListResearchArtifacts(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, category string, limit int) ([]ResearchArtifact, error) {
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	query := `SELECT id, organization_id, project_id, research_run_id, source_type, category,
		title, COALESCE(source_url, ''), content, citations, content_hash, created_at
		FROM platform_research_artifacts
		WHERE organization_id = ? AND project_id = ?`
	args := []any{actor.OrganizationID, projectID}
	if category = strings.ToLower(strings.TrimSpace(category)); category != "" && category != "all" {
		if !validResearchCategory(category, false) {
			return nil, ErrInvalidResearchRequest
		}
		query += ` AND category = ?`
		args = append(args, normalizedResearchCategory(category))
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	values := make([]ResearchArtifact, 0)
	for rows.Next() {
		value, err := scanResearchArtifact(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range values {
		values[index].Sources, err = s.listArtifactSources(
			ctx, string(actor.OrganizationID), string(projectID), values[index].ID,
		)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (s Service) GetResearchArtifact(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, id string) (ResearchArtifact, error) {
	if _, err := s.Projects.GetContext(ctx, actor, projectID); err != nil {
		return ResearchArtifact{}, err
	}
	value, err := scanResearchArtifact(s.DB.QueryRowContext(ctx, `SELECT id, organization_id, project_id,
		research_run_id, source_type, category, title, COALESCE(source_url, ''), content,
		citations, content_hash, created_at FROM platform_research_artifacts
		WHERE organization_id = ? AND project_id = ? AND id = ?`,
		actor.OrganizationID, projectID, strings.TrimSpace(id)))
	if err != nil {
		return ResearchArtifact{}, err
	}
	value.Sources, err = s.listArtifactSources(ctx, string(actor.OrganizationID), string(projectID), value.ID)
	return value, err
}

func scanResearchArtifact(scanner interface{ Scan(...any) error }) (ResearchArtifact, error) {
	var value ResearchArtifact
	var citations []byte
	if err := scanner.Scan(
		&value.ID, &value.OrganizationID, &value.ProjectID, &value.ResearchRunID,
		&value.SourceType, &value.Category, &value.Title, &value.SourceURL, &value.Content,
		&citations, &value.ContentHash, &value.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ResearchArtifact{}, ErrNotFound
		}
		return ResearchArtifact{}, err
	}
	if err := json.Unmarshal(citations, &value.Citations); err != nil {
		return ResearchArtifact{}, err
	}
	value.Sources = []ResearchSource{}
	return value, nil
}

func validateResearchRequest(request ResearchRequest) ([]string, []string, error) {
	if request.Mode != "web" || request.Query == "" ||
		len(request.Query) > 2000 || len(request.DocumentIDs) > 20 {
		return nil, nil, ErrInvalidResearchRequest
	}
	documentIDs := make([]string, 0, len(request.DocumentIDs))
	seenDocuments := map[string]struct{}{}
	for _, value := range request.DocumentIDs {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, nil, ErrInvalidResearchRequest
		}
		if _, exists := seenDocuments[value]; exists {
			continue
		}
		seenDocuments[value] = struct{}{}
		documentIDs = append(documentIDs, value)
	}
	disclosed := map[string]struct{}{}
	for _, value := range request.DisclosedFields {
		value = strings.TrimSpace(value)
		if value != "query" && value != "document_content" {
			return nil, nil, ErrInvalidResearchRequest
		}
		disclosed[value] = struct{}{}
	}
	if _, ok := disclosed["query"]; !ok {
		return nil, nil, ErrInvalidResearchRequest
	}
	_, disclosesDocuments := disclosed["document_content"]
	if disclosesDocuments != (len(documentIDs) > 0) {
		return nil, nil, ErrInvalidResearchRequest
	}
	fields := []string{"query"}
	if disclosesDocuments {
		fields = append(fields, "document_content")
	}
	return documentIDs, fields, nil
}

func validateResearchContext(purpose string, sourceRef *contract.ResourceRef) (string, *contract.ResourceRef, error) {
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		purpose = "deep_research"
	}
	switch purpose {
	case "deep_research":
		if sourceRef == nil || sourceRef.Type != "strategy_workspace" || strings.TrimSpace(sourceRef.ID) == "" {
			return "", nil, ErrInvalidResearchRequest
		}
		return purpose, &contract.ResourceRef{Type: sourceRef.Type, ID: strings.TrimSpace(sourceRef.ID)}, nil
	case "conversation_web_search":
		if sourceRef == nil || sourceRef.Type != "strategy_message" || strings.TrimSpace(sourceRef.ID) == "" {
			return "", nil, ErrInvalidResearchRequest
		}
		return purpose, &contract.ResourceRef{Type: sourceRef.Type, ID: strings.TrimSpace(sourceRef.ID)}, nil
	default:
		return "", nil, ErrInvalidResearchRequest
	}
}

func nullableResourceType(ref *contract.ResourceRef) any {
	if ref == nil || strings.TrimSpace(ref.Type) == "" {
		return nil
	}
	return strings.TrimSpace(ref.Type)
}

func nullableResourceID(ref *contract.ResourceRef) any {
	if ref == nil || strings.TrimSpace(ref.ID) == "" {
		return nil
	}
	return strings.TrimSpace(ref.ID)
}

func nullableResourceIDString(ref *contract.ResourceRef) string {
	if ref == nil {
		return "unknown"
	}
	value := strings.TrimSpace(ref.ID)
	if value == "" {
		return "unknown"
	}
	return value
}

func (s Service) insertArtifact(ctx context.Context, tx *sql.Tx, run ResearchRun, result ExternalResearchResult) (ResearchArtifact, error) {
	if strings.TrimSpace(result.Title) == "" || strings.TrimSpace(result.Content) == "" {
		return ResearchArtifact{}, fmt.Errorf("external research returned an incomplete artifact")
	}
	id, err := s.newID("researchartifact")
	if err != nil {
		return ResearchArtifact{}, err
	}
	hash, err := contract.CanonicalJSONHash(result)
	if err != nil {
		return ResearchArtifact{}, err
	}
	value := ResearchArtifact{
		ID: id, OrganizationID: run.OrganizationID, ProjectID: run.ProjectID,
		ResearchRunID: run.ID, SourceType: run.Mode, Category: run.Category,
		Title:     strings.TrimSpace(result.Title),
		SourceURL: strings.TrimSpace(result.SourceURL), Content: strings.TrimSpace(result.Content),
		Citations: append([]string(nil), result.Citations...), ContentHash: hash, CreatedAt: s.now(),
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO platform_research_artifacts
		(id, organization_id, project_id, research_run_id, source_type, category, title, source_url,
		 content, citations, content_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.OrganizationID, value.ProjectID, value.ResearchRunID, value.SourceType, value.Category,
		value.Title, nullable(value.SourceURL), value.Content, jsonBytes(value.Citations),
		value.ContentHash, value.CreatedAt)
	if err != nil {
		return ResearchArtifact{}, err
	}
	value.Sources = make([]ResearchSource, 0, len(result.Sources))
	for _, source := range result.Sources {
		inserted, insertErr := s.insertResearchSource(ctx, tx, run, value.ID, source)
		if insertErr != nil {
			return ResearchArtifact{}, insertErr
		}
		value.Sources = append(value.Sources, inserted)
	}
	return value, nil
}

func normalizedResearchCategory(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "audience", "competitor", "industry":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "general"
	}
}

func validResearchCategory(value string, allowEmpty bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return allowEmpty
	case "general", "audience", "competitor", "industry":
		return true
	default:
		return false
	}
}

func extractDocument(extension string, content []byte) (string, string, error) {
	switch extension {
	case ".md", ".txt":
		content = bytes.TrimPrefix(content, []byte{0xef, 0xbb, 0xbf})
		if !utf8.Valid(content) {
			return "", "", ErrInvalidDocument
		}
		return strings.TrimSpace(string(content)), defaultDocumentMIME(extension), nil
	case ".xlsx":
		text, err := extractXLSX(content)
		if err != nil || text == "" {
			return "", "", ErrInvalidDocument
		}
		return text, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", nil
	case ".docx":
		reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
		if err != nil {
			return "", "", ErrInvalidDocument
		}
		for _, file := range reader.File {
			if file.Name != "word/document.xml" || file.UncompressedSize64 > uint64(maxExtractedBytes) {
				continue
			}
			stream, err := file.Open()
			if err != nil {
				return "", "", ErrInvalidDocument
			}
			text, err := extractWordXML(io.LimitReader(stream, maxExtractedBytes+1))
			_ = stream.Close()
			if err != nil || text == "" {
				return "", "", ErrInvalidDocument
			}
			return text, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", nil
		}
	}
	return "", "", ErrInvalidDocument
}

func extractWordXML(source io.Reader) (string, error) {
	decoder := xml.NewDecoder(source)
	var builder strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "t" {
				var text string
				if err := decoder.DecodeElement(&text, &value); err != nil {
					return "", err
				}
				builder.WriteString(text)
			} else if value.Name.Local == "tab" {
				builder.WriteByte('\t')
			} else if value.Name.Local == "br" {
				builder.WriteByte('\n')
			}
		case xml.EndElement:
			if value.Name.Local == "p" {
				builder.WriteByte('\n')
			}
		}
		if int64(builder.Len()) > maxExtractedBytes {
			return "", ErrInvalidDocument
		}
	}
	return strings.TrimSpace(builder.String()), nil
}

func allowedMIME(extension, value string) bool {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	switch extension {
	case ".md":
		return value == "text/markdown" || value == "text/plain" || value == "application/octet-stream"
	case ".txt":
		return value == "text/plain" || value == "application/octet-stream"
	case ".html", ".htm":
		return value == "text/html" || value == "application/xhtml+xml" || value == "application/octet-stream"
	case ".docx":
		return value == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" ||
			value == "application/octet-stream"
	case ".xlsx":
		return value == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" ||
			value == "application/octet-stream" || value == "application/zip"
	case ".pdf":
		return value == "application/pdf" || value == "application/octet-stream"
	case ".pptx":
		return value == "application/vnd.openxmlformats-officedocument.presentationml.presentation" ||
			value == "application/octet-stream"
	case ".ppt":
		return value == PowerPointLegacyMIME || value == "application/octet-stream"
	default:
		return false
	}
}

func defaultDocumentMIME(extension string) string {
	switch extension {
	case ".md":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".pdf":
		return "application/pdf"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".ppt":
		return PowerPointLegacyMIME
	default:
		return "application/octet-stream"
	}
}

func documentParseStrategy(extension string) string {
	switch extension {
	// xlsx 跟 md/txt 一样能本地解出文本（extractXLSX），不必绕 Tika。
	case ".md", ".txt", ".xlsx":
		return "text_native"
	default:
		return "tika_text"
	}
}

const documentSelect = `SELECT id, organization_id, project_id, title, COALESCE(source_uri, ''),
	source_type, chunk_count, filename, mime_type, size_bytes,
	content_sha256, text_sha256, extracted_text, status,
	parse_strategy, parse_phase, parse_progress, progress_kind,
	processed_pages, total_pages, quality_score, quality_tier,
	fallback_reason, preview_status, page_quality_summary, heartbeat_at,
	vision_fallback_status, vision_selected_pages, vision_completed_pages,
	vision_attempt_id,
	vision_model_alias, vision_route_revision_id, vision_model_version,
	vision_error_code, vision_error_message, vision_started_at, vision_completed_at,
	COALESCE(parser_code, ''), COALESCE(parser_version, ''),
	COALESCE(parse_error_code, ''), COALESCE(parse_error_message, ''),
	COALESCE(parse_metadata, JSON_OBJECT()), parsed_at,
	created_by, created_at, updated_at,
	object_provider, object_bucket, object_key, object_version_id, object_etag
	FROM platform_knowledge_documents`

type scanner interface {
	Scan(...any) error
}

func scanDocument(row scanner) (Document, error) {
	var value Document
	var versionID, etag sql.NullString
	var parsedAt, heartbeatAt, visionStartedAt, visionCompletedAt sql.NullTime
	var parseProgress, processedPages, totalPages sql.NullInt64
	var qualityScore sql.NullFloat64
	var pageQualitySummary, visionSelectedPages, visionCompletedPages []byte
	err := row.Scan(
		&value.ID, &value.OrganizationID, &value.ProjectID, &value.Title, &value.SourceURI,
		&value.SourceType, &value.ChunkCount, &value.Filename, &value.MIMEType,
		&value.SizeBytes, &value.ContentSHA256, &value.TextSHA256, &value.ExtractedText,
		&value.Status, &value.ParseStrategy, &value.ParsePhase, &parseProgress, &value.ProgressKind,
		&processedPages, &totalPages, &qualityScore, &value.QualityTier,
		&value.FallbackReason, &value.PreviewStatus, &pageQualitySummary, &heartbeatAt,
		&value.VisionFallbackStatus, &visionSelectedPages, &visionCompletedPages, &value.VisionAttemptID,
		&value.VisionModelAlias, &value.VisionRouteRevision, &value.VisionModelVersion,
		&value.VisionErrorCode, &value.VisionErrorMessage, &visionStartedAt, &visionCompletedAt,
		&value.ParserCode, &value.ParserVersion,
		&value.ParseErrorCode, &value.ParseErrorMessage, &value.ParseMetadata, &parsedAt,
		&value.CreatedBy, &value.CreatedAt, &value.UpdatedAt,
		&value.Blob.Provider, &value.Blob.Bucket, &value.Blob.Key, &versionID, &etag,
	)
	value.Blob.VersionID, value.Blob.ETag = versionID.String, etag.String
	value.ContractVersion = DocumentParseContractVersion
	if len(pageQualitySummary) > 0 {
		value.PageQualitySummary = append(json.RawMessage(nil), pageQualitySummary...)
	}
	value.VisionSelectedPages = []int{}
	value.VisionCompletedPages = []int{}
	if len(visionSelectedPages) > 0 {
		if err := json.Unmarshal(visionSelectedPages, &value.VisionSelectedPages); err != nil {
			return Document{}, err
		}
	}
	if len(visionCompletedPages) > 0 {
		if err := json.Unmarshal(visionCompletedPages, &value.VisionCompletedPages); err != nil {
			return Document{}, err
		}
	}
	if parseProgress.Valid {
		value.ParseProgress = intPointer(int(parseProgress.Int64))
	}
	if processedPages.Valid {
		value.ProcessedPages = intPointer(int(processedPages.Int64))
	}
	if totalPages.Valid {
		value.TotalPages = intPointer(int(totalPages.Int64))
	}
	if qualityScore.Valid {
		score := qualityScore.Float64
		value.QualityScore = &score
	}
	if heartbeatAt.Valid {
		value.HeartbeatAt = &heartbeatAt.Time
	}
	if parsedAt.Valid {
		value.ParsedAt = &parsedAt.Time
	}
	if visionStartedAt.Valid {
		value.VisionStartedAt = &visionStartedAt.Time
	}
	if visionCompletedAt.Valid {
		value.VisionCompletedAt = &visionCompletedAt.Time
	}
	return value, err
}

func intPointer(value int) *int {
	return &value
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Service) newID(prefix string) (string, error) {
	if s.NewID != nil {
		return s.NewID(prefix)
	}
	return ids.New(prefix)
}

func jsonBytes(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func nullableJSONValue(value any) any {
	if value == nil {
		return nil
	}
	return jsonBytes(value)
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
