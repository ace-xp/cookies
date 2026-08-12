package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/systems/insights"
)

// registerAssetRoutes exposes 分析素材库 and 内容分析 (03 §9 AM-001~006, §12).
//
// The list endpoints take real filters rather than one catch-all view name,
// because 22 §8.3 requires every visible L2 tab to change the dataset: 全部素材、
// 待匹配、待提取 and 版本关系 are four different queries, not four labels over
// the same rows.
func (s *Server) registerAssetRoutes() {
	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/feature-schemas", s.featureSchemas)
	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/feature-matrix", s.featureMatrix)

	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/assets", s.listAssets)
	s.mux.HandleFunc("POST /api/insights/v1/projects/{project_id}/assets", s.indexAsset)
	// 这一条必须写在 {asset_action} 之前读得懂：Go 1.22 的 ServeMux 按精确度匹配，
	// 字面量段胜过通配段，所以 /assets/similar 不会被当成一条素材的动作。
	s.mux.HandleFunc("POST /api/insights/v1/projects/{project_id}/assets/similar", s.findSimilarAssets)
	s.mux.HandleFunc("POST /api/insights/v1/projects/{project_id}/assets/{asset_action}", s.assetAction)
	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/assets/{asset_id}", s.getAsset)
	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/assets/{asset_id}/lineage", s.listAssetLineage)
	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/assets/{asset_id}/features", s.listAssetFeatures)
	s.mux.HandleFunc("PATCH /api/insights/v1/projects/{project_id}/assets/{asset_id}/features", s.patchAssetFeatures)
	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/assets/{asset_id}/analysis-runs", s.listAssetAnalysisRuns)
	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/analysis-runs", s.listAnalysisRuns)

	// 外部素材是证据，不是资产：单独的路径、单独的表、只读。
	// 它永远不会出现在 /assets 下面——那条路径下的东西是可以被拿去投放的。
	s.mux.HandleFunc("POST /api/insights/v1/projects/{project_id}/external-assets", s.importExternalAsset)
	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/external-assets", s.listExternalAssets)

	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/asset-mappings", s.listAssetMappings)
	s.mux.HandleFunc("POST /api/insights/v1/projects/{project_id}/asset-mappings", s.registerAssetMapping)
	s.mux.HandleFunc("POST /api/insights/v1/projects/{project_id}/asset-mappings/{mapping_action}", s.assetMappingAction)
}

// featureSchemas serves the six type-specific feature systems verbatim from the
// Go source of truth, so the frontend renders each type's own fields instead of
// guessing a shared form (03 §5, MVP 验收②).
func (s *Server) featureSchemas(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"items": insights.AllFeatureSchemas()})
}

func (s *Server) indexAsset(writer http.ResponseWriter, request *http.Request) {
	var body insights.IndexAssetRequest
	if !decode(writer, request, &body) {
		return
	}
	value, err := s.app.IndexAsset(request.Context(), mustActor(request), projectID(request), body)
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (s *Server) listAssets(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	filter := insights.AssetFilter{LineageID: query.Get("lineage_id"), Limit: queryLimit(request)}
	for _, value := range queryList(request, "status") {
		filter.Statuses = append(filter.Statuses, insights.AnalysisStatus(value))
	}
	for _, value := range queryList(request, "asset_type") {
		filter.AssetTypes = append(filter.AssetTypes, insights.AssetType(value))
	}
	for _, value := range queryList(request, "source_kind") {
		filter.SourceKinds = append(filter.SourceKinds, insights.AssetSourceKind(value))
	}
	values, err := s.app.ListAssets(request.Context(), mustActor(request), projectID(request), filter)
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}

func (s *Server) getAsset(writer http.ResponseWriter, request *http.Request) {
	value, err := s.app.GetAsset(request.Context(), mustActor(request), projectID(request), request.PathValue("asset_id"))
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (s *Server) listAssetLineage(writer http.ResponseWriter, request *http.Request) {
	values, err := s.app.ListAssetLineage(request.Context(), mustActor(request), projectID(request), request.PathValue("asset_id"))
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}

func (s *Server) listAssetFeatures(writer http.ResponseWriter, request *http.Request) {
	values, err := s.app.ListAssetFeatures(request.Context(), mustActor(request), projectID(request), request.PathValue("asset_id"))
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}

// patchAssetFeatures writes the 人工结论 layer. It is a PATCH rather than a PUT
// because it never replaces the AI layer beside it (§14).
func (s *Server) patchAssetFeatures(writer http.ResponseWriter, request *http.Request) {
	var body insights.PatchFeaturesRequest
	if !decode(writer, request, &body) {
		return
	}
	values, err := s.app.PatchFeatures(request.Context(), mustActor(request), projectID(request), request.PathValue("asset_id"), body)
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}

// listAssetAnalysisRuns 供素材详情页的「数据与方法」读这条素材的分析历史
// （03 §82：必须展示 Skill/算法版本、数据截止时间和方法）。
func (s *Server) listAssetAnalysisRuns(writer http.ResponseWriter, request *http.Request) {
	filter := analysisRunFilter(request)
	filter.AssetID = request.PathValue("asset_id")
	values, err := s.app.ListAnalysisRuns(request.Context(), mustActor(request), projectID(request), filter)
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}

// listAnalysisRuns 是项目级的分析任务流水，供能力运营看成功率和耗时（03 §310）。
func (s *Server) listAnalysisRuns(writer http.ResponseWriter, request *http.Request) {
	values, err := s.app.ListAnalysisRuns(request.Context(), mustActor(request), projectID(request), analysisRunFilter(request))
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}

func analysisRunFilter(request *http.Request) insights.AnalysisRunFilter {
	filter := insights.AnalysisRunFilter{
		Kind:  insights.AnalysisRunKind(request.URL.Query().Get("kind")),
		Limit: queryLimit(request),
	}
	for _, value := range queryList(request, "status") {
		filter.Statuses = append(filter.Statuses, insights.AnalysisRunStatus(value))
	}
	return filter
}

// assetAction carries the 分析状态链 verbs of PRD §11.1. Each is an explicit
// decision with an expected version, so two people cannot confirm past each other.
// findSimilarAssets 回答「和这条像的还有哪些」或「这个取值的还有哪些」。
// 它是 ❓「算不出来」的升级通道：本轮样本不够时，从库里把同样取值的素材拉过来。
func (s *Server) findSimilarAssets(writer http.ResponseWriter, request *http.Request) {
	var body insights.SimilarAssetRequest
	if !decode(writer, request, &body) {
		return
	}
	value, err := s.app.FindSimilarAssets(request.Context(), mustActor(request), projectID(request), body)
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

// importExternalAssetBody 单独一个 body 类型，不直接复用服务层的请求结构：
// WindowEnd 在那边是 `json:"-"`，因为它不该被当成一个普通字段随便传，
// 必须走和其他窗口一样的日期解析。
type importExternalAssetBody struct {
	insights.ImportExternalAssetRequest
	WindowEnd string `json:"window_end"`
}

// importExternalAsset 收一条平台外的素材当证据。
//
// 收进来的东西**永远不进共享素材库**：那里的素材是可以被拿去投放的，而这一条
// 没有那份授权。它只读、有用途声明、有留存期限，到期删原件只留派生物。
func (s *Server) importExternalAsset(writer http.ResponseWriter, request *http.Request) {
	var body importExternalAssetBody
	if !decode(writer, request, &body) {
		return
	}
	windowEnd, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(body.WindowEnd), time.UTC)
	if err != nil {
		writeError(writer, request, insights.ErrInvalidRequest)
		return
	}
	payload := body.ImportExternalAssetRequest
	payload.WindowEnd = windowEnd
	value, err := s.app.ImportExternalAsset(request.Context(), mustActor(request), projectID(request), payload)
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (s *Server) listExternalAssets(writer http.ResponseWriter, request *http.Request) {
	values, err := s.app.ListExternalAssets(request.Context(), mustActor(request), projectID(request), queryLimit(request))
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}

func (s *Server) assetAction(writer http.ResponseWriter, request *http.Request) {
	action := request.PathValue("asset_action")
	actor, project := mustActor(request), projectID(request)
	switch {
	case strings.HasSuffix(action, ":identify-type"):
		var body insights.IdentifyAssetTypeRequest
		if !decode(writer, request, &body) {
			return
		}
		value, err := s.app.IdentifyAssetType(request.Context(), actor, project, strings.TrimSuffix(action, ":identify-type"), body)
		if err != nil {
			writeError(writer, request, err)
			return
		}
		writeJSON(writer, http.StatusOK, value)
	case strings.HasSuffix(action, ":extract-features"):
		var body insights.ExtractFeaturesRequest
		if !decode(writer, request, &body) {
			return
		}
		values, err := s.app.ExtractFeatures(request.Context(), actor, project, strings.TrimSuffix(action, ":extract-features"), body)
		if err != nil {
			writeError(writer, request, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": values})
	case strings.HasSuffix(action, ":derive-features"):
		// 量客观变量。和 :analyze 相反——不调模型，不花钱，读的是素材库里
		// 上传时就探测好的元数据，同一条素材按几次结果都一样。
		var body insights.DeriveFeaturesRequest
		if !decode(writer, request, &body) {
			return
		}
		values, err := s.app.DeriveFeatures(request.Context(), actor, project, strings.TrimSuffix(action, ":derive-features"), body)
		if err != nil {
			writeError(writer, request, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": values})
	case strings.HasSuffix(action, ":analyze"):
		// AI 提特征。这是一次真实的模型调用，会花钱也会花时间，
		// 所以只挂在这条显式的动词上——没有任何地方会自动触发它。
		var body insights.AnalyzeAssetRequest
		if !decode(writer, request, &body) {
			return
		}
		value, err := s.app.AnalyzeAsset(request.Context(), actor, project, strings.TrimSuffix(action, ":analyze"), body)
		if err != nil {
			writeError(writer, request, err)
			return
		}
		writeJSON(writer, http.StatusOK, value)
	case strings.HasSuffix(action, ":confirm"):
		s.assetTransition(writer, request, strings.TrimSuffix(action, ":confirm"), s.app.ConfirmAssetAnalysis)
	case strings.HasSuffix(action, ":request-review"):
		s.assetTransition(writer, request, strings.TrimSuffix(action, ":request-review"), s.app.RequestAssetReview)
	case strings.HasSuffix(action, ":retire"):
		s.assetTransition(writer, request, strings.TrimSuffix(action, ":retire"), s.app.RetireAsset)
	default:
		http.NotFound(writer, request)
	}
}

type assetTransitionFunc func(context.Context, contract.ActorContext, contract.ProjectID, string, insights.AssetTransitionRequest) (insights.Asset, error)

func (s *Server) assetTransition(writer http.ResponseWriter, request *http.Request, assetID string, apply assetTransitionFunc) {
	var body insights.AssetTransitionRequest
	if !decode(writer, request, &body) {
		return
	}
	value, err := apply(request.Context(), mustActor(request), projectID(request), assetID, body)
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (s *Server) registerAssetMapping(writer http.ResponseWriter, request *http.Request) {
	var body insights.RegisterAssetMappingRequest
	if !decode(writer, request, &body) {
		return
	}
	value, err := s.app.RegisterAssetMapping(request.Context(), mustActor(request), projectID(request), body)
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (s *Server) listAssetMappings(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	filter := insights.AssetMappingFilter{
		Platform: query.Get("platform"), AssetID: query.Get("asset_id"), Limit: queryLimit(request),
	}
	for _, value := range queryList(request, "status") {
		filter.Statuses = append(filter.Statuses, insights.MappingStatus(value))
	}
	values, err := s.app.ListAssetMappings(request.Context(), mustActor(request), projectID(request), filter)
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": values})
}

func (s *Server) assetMappingAction(writer http.ResponseWriter, request *http.Request) {
	action := request.PathValue("mapping_action")
	if !strings.HasSuffix(action, ":resolve") {
		http.NotFound(writer, request)
		return
	}
	var body insights.ResolveAssetMappingRequest
	if !decode(writer, request, &body) {
		return
	}
	value, err := s.app.ResolveAssetMapping(request.Context(), mustActor(request), projectID(request),
		strings.TrimSuffix(action, ":resolve"), body)
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

// featureMatrix compares several assets on their shared features. asset_ids is
// repeatable or comma separated; the service decides which keys may be compared.
func (s *Server) featureMatrix(writer http.ResponseWriter, request *http.Request) {
	value, err := s.app.GetFeatureMatrix(request.Context(), mustActor(request), projectID(request), queryList(request, "asset_ids"))
	if err != nil {
		writeError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

// queryList accepts both ?status=a&status=b and ?status=a,b so the frontend can
// build filter URLs either way.
func queryList(request *http.Request, key string) []string {
	values := make([]string, 0, 4)
	for _, raw := range request.URL.Query()[key] {
		for _, part := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				values = append(values, trimmed)
			}
		}
	}
	return values
}
