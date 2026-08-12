package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/systems/insights"
)

func TestInsightsHTTPExposesReportExperienceAndPreLaunchLoop(t *testing.T) {
	t.Parallel()
	app := &applicationStub{
		report:     insights.InsightReport{ID: "insightreport_1", Version: 1},
		experience: insights.Experience{ID: "experience_1"},
	}
	server := New(app)
	tests := []struct {
		method string
		path   string
		body   string
		status int
		want   string
	}{
		{http.MethodPost, "/api/insights/v1/projects/project_1/reports", `{"execution_id":"deliveryexecution_1","summary":"摘要","findings":["发现"]}`, 201, "insightreport_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/reports/insightreport_1:confirm", `{"expected_version":1}`, 200, "insightreport_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/reports/insightreport_1:create-experience", `{"expected_report_version":1,"conclusion":"结论","conditions":[],"counterexamples":[]}`, 201, "experience_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/prelaunch", "", 200, "experience_1"},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(test.method, test.path, test.body))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("%s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
	}
}

// 定格窗口按天传，不能只传一头。补一个默认值等于替人挑了半个窗口，
// 而报告上会写得像是他自己选的。
func TestCreateReportWindowMustBeWholeOrAbsent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		window string
		status int
		start  string
	}{
		{"两头都有", `,"window":{"start":"2026-07-01","end":"2026-07-14"}`, 201, "2026-07-01"},
		{"两头都没有", "", 201, ""},
		{"只有开头", `,"window":{"start":"2026-07-01","end":""}`, 400, ""},
		{"日期写错", `,"window":{"start":"2026/07/01","end":"2026-07-14"}`, 400, ""},
	}
	for _, test := range tests {
		app := &applicationStub{report: insights.InsightReport{ID: "insightreport_1", Version: 1}}
		server := New(app)
		response := httptest.NewRecorder()
		body := `{"execution_id":"deliveryexecution_1"` + test.window + `}`
		server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/insights/v1/projects/project_1/reports", body))
		if response.Code != test.status {
			t.Fatalf("%s status=%d，想要 %d，body=%s", test.name, response.Code, test.status, response.Body.String())
		}
		var got string
		if !app.reportWindow.Start.IsZero() {
			got = app.reportWindow.Start.Format("2006-01-02")
		}
		if got != test.start {
			t.Fatalf("%s 窗口起点=%q，想要 %q", test.name, got, test.start)
		}
	}
}

// 记一笔的窗口必填，且按天解析。窗口缺一头就不知道往哪份复盘草稿记，
// 这时候挑一个默认窗口，记下来的那条会挂在人根本没看过的一段数据上。
func TestPinFindingRequiresAWholeDayWindow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		window string
		status int
		start  string
	}{
		{"两头都有", `{"start":"2026-07-01","end":"2026-07-14"}`, 200, "2026-07-01"},
		{"只有开头", `{"start":"2026-07-01","end":""}`, 400, ""},
		{"完全没有", `{}`, 400, ""},
		{"日期写错", `{"start":"2026/07/01","end":"2026-07-14"}`, 400, ""},
	}
	for _, test := range tests {
		app := &applicationStub{report: insights.InsightReport{ID: "insightreport_1", Version: 1}}
		server := New(app)
		response := httptest.NewRecorder()
		body := `{"dimension":"drivers","variable":"duration","window":` + test.window + `}`
		server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/insights/v1/projects/project_1/findings", body))
		if response.Code != test.status {
			t.Fatalf("%s status=%d，想要 %d，body=%s", test.name, response.Code, test.status, response.Body.String())
		}
		var got string
		if !app.pinned.Window.Start.IsZero() {
			got = app.pinned.Window.Start.Format("2006-01-02")
		}
		if got != test.start {
			t.Fatalf("%s 窗口起点=%q，想要 %q", test.name, got, test.start)
		}
	}
}

// 判定不能从请求里穿过去。请求体里带 verdict 直接退回 400（解码器不认多余字段），
// 而不是静默忽略：静默忽略的话，前端可以一直传着，谁也不知道它没生效。
func TestPinFindingRejectsAVerdictInTheRequestBody(t *testing.T) {
	t.Parallel()
	app := &applicationStub{report: insights.InsightReport{ID: "insightreport_1", Version: 1}}
	server := New(app)
	response := httptest.NewRecorder()
	body := `{"dimension":"drivers","variable":"duration","verdict":"explained",` +
		`"window":{"start":"2026-07-01","end":"2026-07-14"}}`
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/insights/v1/projects/project_1/findings", body))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("带 verdict 的请求 status=%d，想要 400，body=%s", response.Code, response.Body.String())
	}
	if app.pinned.Dimension != "" {
		t.Fatalf("被拒的请求不该到达服务层：%+v", app.pinned)
	}
}

// 提交复盘走独立路径，报告 ID 从路径里取，执行和版本从请求体里取。三个值任何一个
// 串位，提交的都是另一份复盘或另一次投放，而返回的 200 看起来一切正常。
func TestSubmitReviewTakesReportIDFromPathAndBodyFromJSON(t *testing.T) {
	t.Parallel()
	app := &applicationStub{report: insights.InsightReport{ID: "insightreport_7", Version: 3}}
	server := New(app)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost,
		"/api/insights/v1/projects/project_1/reports/insightreport_7/submit",
		`{"execution_id":"insightexecution_2","expected_version":3}`))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d，想要 200，body=%s", response.Code, response.Body.String())
	}
	if app.submittedReportID != "insightreport_7" {
		t.Errorf("报告 ID=%q，想要 insightreport_7", app.submittedReportID)
	}
	if app.submitted.ExecutionID != "insightexecution_2" || app.submitted.ExpectedVersion != 3 {
		t.Errorf("请求体没原样传下去：%+v", app.submitted)
	}
}

// 人工删减走 :drop-finding。索引和 dropped 都要原样传到后面——
// 传错一个索引，删掉的就是另一条发现，而人看不出来。
func TestDropReportFindingPassesIndexAndFlag(t *testing.T) {
	t.Parallel()
	app := &applicationStub{report: insights.InsightReport{ID: "insightreport_1", Version: 2}}
	server := New(app)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost,
		"/api/insights/v1/projects/project_1/reports/insightreport_1:drop-finding",
		`{"expected_version":1,"index":3,"dropped":true}`))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if app.droppedIndex != 3 || !app.droppedFlag {
		t.Fatalf("index=%d dropped=%v，想要 3/true", app.droppedIndex, app.droppedFlag)
	}
}

// 跨渠道比较默认关闭（03 §10.3②）。缺参数、写别的值都算关闭——
// 只有显式 true 才打开，因为打开意味着把不可直接比较的渠道并排放。
func TestPreLaunchCrossChannelIsOffUnlessExplicitlyTrue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		query string
		want  bool
	}{
		{"", false},
		{"?cross_channel=false", false},
		{"?cross_channel=1", false},
		{"?cross_channel=true", true},
	}
	for _, test := range tests {
		app := &applicationStub{experience: insights.Experience{ID: "experience_1"}}
		server := New(app)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(http.MethodGet,
			"/api/insights/v1/projects/project_1/prelaunch"+test.query, ""))
		if response.Code != http.StatusOK {
			t.Fatalf("%q status=%d", test.query, response.Code)
		}
		if app.preLaunchFilter.CrossChannel != test.want {
			t.Fatalf("%q cross_channel=%v，想要 %v", test.query, app.preLaunchFilter.CrossChannel, test.want)
		}
	}
}

func TestPreLaunchPassesScopeFiltersThrough(t *testing.T) {
	t.Parallel()
	app := &applicationStub{experience: insights.Experience{ID: "experience_1"}}
	server := New(app)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodGet,
		"/api/insights/v1/projects/project_1/prelaunch?channel=douyin&creative_type=short_video&objective=conversion&q=首图", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	got := app.preLaunchFilter
	if got.Channel != "douyin" || got.CreativeType != "short_video" ||
		got.Objective != "conversion" || got.Query != "首图" {
		t.Fatalf("筛选条件 = %#v", got)
	}
}

func TestInsightsHTTPExposesExperienceLifecycleActions(t *testing.T) {
	t.Parallel()
	app := &applicationStub{experience: insights.Experience{ID: "experience_1"}}
	server := New(app)
	tests := []struct {
		method string
		path   string
		body   string
		status int
		want   string
	}{
		{http.MethodPost, "/api/insights/v1/projects/project_1/experiences/experience_1:confirm", `{"expected_version":1}`, 200, "experience_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/experiences/experience_1:reject", `{"expected_version":1,"reason":"证据不足"}`, 200, "experience_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/experiences/experience_1:request-review", `{"expected_version":2,"reason":"新数据冲突"}`, 200, "experience_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/experiences/experience_1:retire", `{"expected_version":3,"reason":"结论已过时"}`, 200, "experience_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/experiences/experience_1:revise", `{"expected_version":2,"conclusion":"新结论","conditions":[],"counterexamples":[],"reason":"补充样本"}`, 201, "experience_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/experiences/experience_1:record-reference", `{"consumer_kind":"strategy","consumer_id":"strategy_1","outcome":"adopted","note":""}`, 201, "experienceref_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/experiences/experience_1:unknown", `{}`, 404, ""},
		{http.MethodGet, "/api/insights/v1/projects/project_1/experiences/experience_1/references", "", 200, "experienceref_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/experiences/experience_1/audits", "", 200, "experienceaudit_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/experiences/experience_1/lineage", "", 200, "experience_1"},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(test.method, test.path, test.body))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("%s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
	}
}

// The experience library filters by lifecycle status, so the 待确认 queue and
// the quotable list are separate reads rather than one client-side split.
func TestListExperiencesForwardsStatusFilter(t *testing.T) {
	t.Parallel()
	app := &applicationStub{experience: insights.Experience{ID: "experience_1"}}
	server := New(app)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodGet, "/api/insights/v1/projects/project_1/experiences?status=pending", ""))
	if response.Code != http.StatusOK || app.listedStatus != insights.ExperiencePending {
		t.Fatalf("status=%d filter=%q", response.Code, app.listedStatus)
	}
}

// lookup 和「{id}:动词」共用同一段路径，很容易被通配符那条路由吃掉——
// 吃掉的表现是 404，不是编译错，所以这里钉一下路由确实分得开、条件确实传到了服务层。
func TestExperienceLookupIsNotSwallowedByTheActionRoute(t *testing.T) {
	t.Parallel()
	app := &applicationStub{experience: insights.Experience{ID: "experience_1"}}
	server := New(app)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost,
		"/api/insights/v1/projects/project_1/experiences/lookup",
		`{"channel":"抖音","include_observed":true}`))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if app.lookup.Channel != "抖音" || !app.lookup.IncludeObserved {
		t.Fatalf("条件没传到服务层：%#v", app.lookup)
	}
	if !strings.Contains(response.Body.String(), `"matched"`) {
		t.Fatalf("命中理由要一起给出去：%s", response.Body.String())
	}
}

func TestInsightsHTTPExposesAssetAnalysisSurface(t *testing.T) {
	t.Parallel()
	app := &applicationStub{
		asset:   insights.Asset{ID: "insightasset_1", Version: 1},
		mapping: insights.AssetMapping{ID: "insightassetmapping_1", Version: 1},
		feature: insights.AssetFeature{ID: "assetfeature_1", Key: "article_type"},
	}
	server := New(app)
	tests := []struct {
		method string
		path   string
		body   string
		status int
		want   string
	}{
		{http.MethodPost, "/api/insights/v1/projects/project_1/assets", `{"title":"素材","source_kind":"upload"}`, 201, "insightasset_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/assets", "", 200, "insightasset_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/assets/insightasset_1", "", 200, "insightasset_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/assets/insightasset_1/lineage", "", 200, "insightasset_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/assets/insightasset_1/features", "", 200, "article_type"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/assets/insightasset_1:identify-type",
			`{"expected_version":1,"asset_type":"wechat_article","source":"human","confidence":"","reason":""}`, 200, "insightasset_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/assets/insightasset_1:extract-features",
			`{"expected_version":1,"skill_id":"skill_1","skill_version":"v1","features":[{"key":"article_type","value":{"kind":"enum","terms":["知识"]},"confidence":"low"}]}`, 200, "article_type"},
		{http.MethodPatch, "/api/insights/v1/projects/project_1/assets/insightasset_1/features",
			`{"expected_version":2,"features":[{"key":"article_type","value":{"kind":"enum","terms":["案例"]}}],"reason":"人工修正"}`, 200, "article_type"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/assets/insightasset_1:confirm", `{"expected_version":3,"reason":""}`, 200, "insightasset_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/assets/insightasset_1:request-review", `{"expected_version":4,"reason":"新数据冲突"}`, 200, "insightasset_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/assets/insightasset_1:retire", `{"expected_version":5,"reason":"源文件下线"}`, 200, "insightasset_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/assets/insightasset_1:unknown", `{}`, 404, ""},
		{http.MethodPost, "/api/insights/v1/projects/project_1/asset-mappings",
			`{"platform":"demo_platform","platform_object_kind":"creative","platform_object_id":"cr-1","platform_object_name":"创意","asset_id":"","match_source":"","note":""}`, 201, "insightassetmapping_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/asset-mappings", "", 200, "insightassetmapping_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/asset-mappings/insightassetmapping_1:resolve",
			`{"expected_version":1,"status":"matched","asset_id":"insightasset_1","note":""}`, 200, "insightassetmapping_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/asset-mappings/insightassetmapping_1:unknown", `{}`, 404, ""},
		{http.MethodGet, "/api/insights/v1/projects/project_1/feature-schemas", "", 200, "wechat_article"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/feature-matrix?asset_ids=insightasset_1", "", 200, "共同特征"},
		// similar 是字面量段，不能被 {asset_action} 吃掉：吃掉的话它会被当成
		// 一条叫 similar 的素材的未知动作，返回 404。
		{http.MethodPost, "/api/insights/v1/projects/project_1/assets/similar",
			`{"features":{"duration":"15s"}}`, 200, "insightasset_2"},
		// 外部素材走自己的路径，和 /assets 不共用任何一段。
		{http.MethodPost, "/api/insights/v1/projects/project_1/external-assets",
			`{"title":"同行的一条 15 秒竖版","purpose":"benchmark","source_note":"公开投放素材","window_end":"2026-07-30"}`,
			201, "externalasset_1"},
		// 没有 window_end 就算不出留存期限，直接 400——不许用默认值蒙混过去。
		{http.MethodPost, "/api/insights/v1/projects/project_1/external-assets",
			`{"title":"同行的一条 15 秒竖版","purpose":"benchmark"}`, 400, ""},
		{http.MethodGet, "/api/insights/v1/projects/project_1/external-assets", "", 200, "externalasset_1"},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(test.method, test.path, test.body))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
}

// AI 提特征只挂在一条显式的动词上，分析留痕可按素材读也可按项目读。
func TestInsightsHTTPExposesFeatureExtraction(t *testing.T) {
	t.Parallel()
	app := &applicationStub{
		feature:     insights.AssetFeature{ID: "assetfeature_1", Key: "article_type"},
		analysisRun: insights.AnalysisRun{ID: "insightrun_1", SkillID: "insight.extract.wechat_article"},
	}
	server := New(app)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost,
		"/api/insights/v1/projects/project_1/assets/insightasset_1:analyze",
		`{"expected_version":1,"content":"公众号正文","note":"重发版"}`))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "insightrun_1") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if app.analyzedAssetID != "insightasset_1" || app.analyzeRequest.Content != "公众号正文" ||
		app.analyzeRequest.Note != "重发版" || app.analyzeRequest.ExpectedVersion != 1 {
		t.Fatalf("asset=%q request=%#v", app.analyzedAssetID, app.analyzeRequest)
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodGet,
		"/api/insights/v1/projects/project_1/assets/insightasset_1/analysis-runs?status=failed&limit=5", ""))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "insightrun_1") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if app.runFilter.AssetID != "insightasset_1" || app.runFilter.Limit != 5 ||
		len(app.runFilter.Statuses) != 1 || app.runFilter.Statuses[0] != insights.AnalysisRunFailed {
		t.Fatalf("filter=%#v", app.runFilter)
	}

	// 项目级的流水不带素材条件，否则能力运营看到的成功率只是某一条素材的。
	response = httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodGet,
		"/api/insights/v1/projects/project_1/analysis-runs?kind=feature_extraction", ""))
	if response.Code != http.StatusOK || app.runFilter.AssetID != "" ||
		app.runFilter.Kind != insights.AnalysisRunFeatureExtraction {
		t.Fatalf("status=%d filter=%#v", response.Code, app.runFilter)
	}
}

// 22 §8.3 要求每个可见 L2 标签都真实改变数据集，所以筛选条件必须原样传到服务层，
// 而不是前端拿到同一批数据自己分。
func TestAssetQueriesForwardEveryFilterToTheService(t *testing.T) {
	t.Parallel()
	app := &applicationStub{}
	server := New(app)

	server.ServeHTTP(httptest.NewRecorder(), authenticatedRequest(http.MethodGet,
		"/api/insights/v1/projects/project_1/assets?status=awaiting_match,analysable&asset_type=wechat_article&source_kind=upload&lineage_id=insightasset_1&limit=7", ""))
	filter := app.assetFilter
	if len(filter.Statuses) != 2 || filter.Statuses[0] != insights.AnalysisAwaitingMatch ||
		filter.Statuses[1] != insights.AnalysisAnalysable {
		t.Fatalf("statuses=%#v", filter.Statuses)
	}
	if len(filter.AssetTypes) != 1 || filter.AssetTypes[0] != insights.AssetTypeWechatArticle ||
		len(filter.SourceKinds) != 1 || filter.SourceKinds[0] != insights.AssetSourceUpload ||
		filter.LineageID != "insightasset_1" || filter.Limit != 7 {
		t.Fatalf("filter=%#v", filter)
	}

	server.ServeHTTP(httptest.NewRecorder(), authenticatedRequest(http.MethodGet,
		"/api/insights/v1/projects/project_1/asset-mappings?status=unmatched&platform=demo_platform", ""))
	if len(app.mappingFilter.Statuses) != 1 || app.mappingFilter.Statuses[0] != insights.MappingUnmatched ||
		app.mappingFilter.Platform != "demo_platform" {
		t.Fatalf("mappingFilter=%#v", app.mappingFilter)
	}

	server.ServeHTTP(httptest.NewRecorder(), authenticatedRequest(http.MethodGet,
		"/api/insights/v1/projects/project_1/feature-matrix?asset_ids=a&asset_ids=b,c", ""))
	if len(app.matrixAssetIDs) != 3 || app.matrixAssetIDs[2] != "c" {
		t.Fatalf("matrixAssetIDs=%#v", app.matrixAssetIDs)
	}
}

func authenticatedRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	ctx := contract.WithRequestContext(request.Context(), contract.RequestContext{
		RequestID: "req_1", TraceID: "trace_1",
		Actor: contract.ActorContext{
			OrganizationID: "org_1", Principal: contract.Principal{Kind: contract.PrincipalUser, ID: "user_1"},
			Scopes: []contract.Scope{insights.ScopeRead, insights.ScopeWrite, insights.ScopeConfirm},
		},
	})
	return request.WithContext(ctx)
}

type applicationStub struct {
	report          insights.InsightReport
	experience      insights.Experience
	listedStatus    insights.ExperienceStatus
	lookup          insights.ExperienceLookup
	preLaunchFilter insights.PreLaunchFilter

	asset          insights.Asset
	mapping        insights.AssetMapping
	feature        insights.AssetFeature
	assetFilter    insights.AssetFilter
	mappingFilter  insights.AssetMappingFilter
	matrixAssetIDs []string
	similarRequest insights.SimilarAssetRequest

	externalRequest insights.ImportExternalAssetRequest
	externalLimit   int

	analysisRun     insights.AnalysisRun
	analyzeRequest  insights.AnalyzeAssetRequest
	analyzedAssetID string
	runFilter       insights.AnalysisRunFilter

	dataSource        insights.DataSource
	importBatch       insights.ImportBatch
	dataSourceFilter  insights.DataSourceFilter
	importBatchFilter insights.ImportBatchFilter
	importedRows      int
	window            insights.MetricWindow

	qualityReport  insights.QualityReport
	qualityRequest insights.ResolveQualityIssueRequest

	capabilityOperations insights.CapabilityOperations
	settings             insights.InsightSettings
	thresholds           insights.ResolvedThresholds
	thresholdRequest     insights.SaveThresholdsRequest
	thresholdHistory     []insights.ThresholdSet

	experiment        insights.Experiment
	readout           insights.ExperimentReadout
	attachResult      insights.AttachExperimentAssetResult
	experimentRequest insights.CreateExperimentRequest
	experimentFilter  insights.ExperimentFilter
	experimentID      string
	variantID         string
	attachedAssetID   string
	interpretation    string
	reportWindow      insights.MetricWindow
	pinned            insights.PinFindingRequest
	droppedIndex      int
	droppedFlag       bool
	submitted         insights.SubmitReviewRequest
	submittedReportID string

	registerErr error
}

func (s *applicationStub) CreateReport(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, request insights.CreateReportRequest) (insights.InsightReport, error) {
	s.reportWindow = request.Window
	return s.report, nil
}
func (s *applicationStub) PinFinding(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, request insights.PinFindingRequest) (insights.InsightReport, error) {
	s.pinned = request
	return s.report, nil
}
func (s *applicationStub) DropReportFinding(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, _ string, _ int64, index int, dropped bool) (insights.InsightReport, error) {
	s.droppedIndex = index
	s.droppedFlag = dropped
	return s.report, nil
}
func (s *applicationStub) ListReports(context.Context, contract.ActorContext, contract.ProjectID, int) ([]insights.InsightReport, error) {
	return []insights.InsightReport{s.report}, nil
}
func (s *applicationStub) ConfirmReport(context.Context, contract.ActorContext, contract.ProjectID, string, int64) (insights.InsightReport, error) {
	return s.report, nil
}
func (s *applicationStub) SubmitReview(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, reportID string, request insights.SubmitReviewRequest) (insights.InsightReport, error) {
	s.submittedReportID, s.submitted = reportID, request
	return s.report, nil
}
func (s *applicationStub) CreateExperience(context.Context, contract.ActorContext, contract.ProjectID, string, int64, insights.CreateExperienceRequest) (insights.Experience, error) {
	return s.experience, nil
}
func (s *applicationStub) ListExperiences(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, status insights.ExperienceStatus, _ int) ([]insights.Experience, error) {
	s.listedStatus = status
	return []insights.Experience{s.experience}, nil
}
func (s *applicationStub) LookupExperiences(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, lookup insights.ExperienceLookup) ([]insights.ExperienceMatch, error) {
	s.lookup = lookup
	return []insights.ExperienceMatch{{Experience: s.experience, Matched: []string{"渠道"}, Default: true}}, nil
}
func (s *applicationStub) ConfirmExperience(context.Context, contract.ActorContext, contract.ProjectID, string, int64) (insights.Experience, error) {
	return s.experience, nil
}
func (s *applicationStub) RejectExperience(context.Context, contract.ActorContext, contract.ProjectID, string, insights.ExperienceTransitionRequest) (insights.Experience, error) {
	return s.experience, nil
}
func (s *applicationStub) RequestExperienceReview(context.Context, contract.ActorContext, contract.ProjectID, string, insights.ExperienceTransitionRequest) (insights.Experience, error) {
	return s.experience, nil
}
func (s *applicationStub) RetireExperience(context.Context, contract.ActorContext, contract.ProjectID, string, insights.ExperienceTransitionRequest) (insights.Experience, error) {
	return s.experience, nil
}
func (s *applicationStub) ReviseExperience(context.Context, contract.ActorContext, contract.ProjectID, string, insights.ReviseExperienceRequest) (insights.Experience, error) {
	return s.experience, nil
}
func (s *applicationStub) RecordExperienceReference(context.Context, contract.ActorContext, contract.ProjectID, string, insights.RecordExperienceReferenceRequest) (insights.ExperienceReference, error) {
	return insights.ExperienceReference{ID: "experienceref_1", ExperienceID: s.experience.ID}, nil
}
func (s *applicationStub) ListExperienceReferences(context.Context, contract.ActorContext, contract.ProjectID, string, int) ([]insights.ExperienceReference, error) {
	return []insights.ExperienceReference{{ID: "experienceref_1", ExperienceID: s.experience.ID}}, nil
}
func (s *applicationStub) ListProjectExperienceReferences(context.Context, contract.ActorContext, contract.ProjectID, int) ([]insights.ExperienceReference, error) {
	return []insights.ExperienceReference{{ID: "experienceref_1", ExperienceID: s.experience.ID}}, nil
}
func (s *applicationStub) ListExperienceAudits(context.Context, contract.ActorContext, contract.ProjectID, string, int) ([]insights.ExperienceAudit, error) {
	return []insights.ExperienceAudit{{ID: "experienceaudit_1", ExperienceID: s.experience.ID}}, nil
}
func (s *applicationStub) ListExperienceLineage(context.Context, contract.ActorContext, contract.ProjectID, string) ([]insights.Experience, error) {
	return []insights.Experience{s.experience}, nil
}
func (s *applicationStub) GetPreLaunch(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, filter insights.PreLaunchFilter) (insights.PreLaunchInsight, error) {
	s.preLaunchFilter = filter
	return insights.PreLaunchInsight{ExperienceReferences: []insights.Experience{s.experience}}, nil
}
func (s *applicationStub) GetPerformance(context.Context, contract.ActorContext, contract.ProjectID) (insights.PerformanceOverview, error) {
	return insights.PerformanceOverview{}, nil
}

func (s *applicationStub) IndexAsset(context.Context, contract.ActorContext, contract.ProjectID, insights.IndexAssetRequest) (insights.Asset, error) {
	return s.asset, nil
}
func (s *applicationStub) ListAssets(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, filter insights.AssetFilter) ([]insights.Asset, error) {
	s.assetFilter = filter
	return []insights.Asset{s.asset}, nil
}
func (s *applicationStub) GetAsset(context.Context, contract.ActorContext, contract.ProjectID, string) (insights.Asset, error) {
	return s.asset, nil
}
func (s *applicationStub) ListAssetLineage(context.Context, contract.ActorContext, contract.ProjectID, string) ([]insights.Asset, error) {
	return []insights.Asset{s.asset}, nil
}
func (s *applicationStub) IdentifyAssetType(context.Context, contract.ActorContext, contract.ProjectID, string, insights.IdentifyAssetTypeRequest) (insights.Asset, error) {
	return s.asset, nil
}
func (s *applicationStub) RegisterAssetMapping(context.Context, contract.ActorContext, contract.ProjectID, insights.RegisterAssetMappingRequest) (insights.AssetMapping, error) {
	return s.mapping, nil
}
func (s *applicationStub) ListAssetMappings(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, filter insights.AssetMappingFilter) ([]insights.AssetMapping, error) {
	s.mappingFilter = filter
	return []insights.AssetMapping{s.mapping}, nil
}
func (s *applicationStub) ResolveAssetMapping(context.Context, contract.ActorContext, contract.ProjectID, string, insights.ResolveAssetMappingRequest) (insights.AssetMapping, error) {
	return s.mapping, nil
}
func (s *applicationStub) ExtractFeatures(context.Context, contract.ActorContext, contract.ProjectID, string, insights.ExtractFeaturesRequest) ([]insights.AssetFeature, error) {
	return []insights.AssetFeature{s.feature}, nil
}
func (s *applicationStub) DeriveFeatures(context.Context, contract.ActorContext, contract.ProjectID, string, insights.DeriveFeaturesRequest) ([]insights.AssetFeature, error) {
	return []insights.AssetFeature{s.feature}, nil
}
func (s *applicationStub) PatchFeatures(context.Context, contract.ActorContext, contract.ProjectID, string, insights.PatchFeaturesRequest) ([]insights.AssetFeature, error) {
	return []insights.AssetFeature{s.feature}, nil
}
func (s *applicationStub) ListAssetFeatures(context.Context, contract.ActorContext, contract.ProjectID, string) ([]insights.AssetFeature, error) {
	return []insights.AssetFeature{s.feature}, nil
}
func (s *applicationStub) ConfirmAssetAnalysis(context.Context, contract.ActorContext, contract.ProjectID, string, insights.AssetTransitionRequest) (insights.Asset, error) {
	return s.asset, nil
}
func (s *applicationStub) RequestAssetReview(context.Context, contract.ActorContext, contract.ProjectID, string, insights.AssetTransitionRequest) (insights.Asset, error) {
	return s.asset, nil
}
func (s *applicationStub) RetireAsset(context.Context, contract.ActorContext, contract.ProjectID, string, insights.AssetTransitionRequest) (insights.Asset, error) {
	return s.asset, nil
}
func (s *applicationStub) AnalyzeAsset(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, assetID string, request insights.AnalyzeAssetRequest) (insights.AnalyzeAssetResult, error) {
	s.analyzedAssetID = assetID
	s.analyzeRequest = request
	return insights.AnalyzeAssetResult{Run: s.analysisRun, Features: []insights.AssetFeature{s.feature}}, nil
}
func (s *applicationStub) ListAnalysisRuns(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, filter insights.AnalysisRunFilter) ([]insights.AnalysisRun, error) {
	s.runFilter = filter
	return []insights.AnalysisRun{s.analysisRun}, nil
}
func (s *applicationStub) GetFeatureMatrix(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, assetIDs []string) (insights.FeatureMatrix, error) {
	s.matrixAssetIDs = assetIDs
	assets := make([]insights.Asset, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		assets = append(assets, insights.Asset{ID: assetID})
	}
	return insights.FeatureMatrix{Assets: assets, Disclosure: "仅比较各类型都有的共同特征。"}, nil
}
func (s *applicationStub) FindSimilarAssets(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, request insights.SimilarAssetRequest) (insights.SimilarAssetResult, error) {
	s.similarRequest = request
	return insights.SimilarAssetResult{
		Probe: []insights.SimilarityReason{{Key: "duration", Label: "时长", Value: "15s", Source: insights.SourceHuman}},
		Items: []insights.SimilarAsset{{AssetID: "insightasset_2", Overlap: 1, AdmissibleOverlap: 1}},
	}, nil
}
func (s *applicationStub) ImportExternalAsset(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, request insights.ImportExternalAssetRequest) (insights.ExternalAsset, error) {
	s.externalRequest = request
	return insights.ExternalAsset{
		ID: "externalasset_1", Title: request.Title, Purpose: request.Purpose,
		RetentionUntil: request.WindowEnd,
	}, nil
}
func (s *applicationStub) ListExternalAssets(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, limit int) ([]insights.ExternalAsset, error) {
	s.externalLimit = limit
	return []insights.ExternalAsset{{ID: "externalasset_1", Title: "同行的一条 15 秒竖版", Purpose: insights.PurposeBenchmark}}, nil
}
func (s *applicationStub) RegisterDataSource(context.Context, contract.ActorContext, contract.ProjectID, insights.RegisterDataSourceRequest) (insights.DataSource, error) {
	return s.dataSource, s.registerErr
}
func (s *applicationStub) ListDataSources(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, filter insights.DataSourceFilter) ([]insights.DataSource, error) {
	s.dataSourceFilter = filter
	return []insights.DataSource{s.dataSource}, nil
}
func (s *applicationStub) GetDataSource(context.Context, contract.ActorContext, contract.ProjectID, string) (insights.DataSource, error) {
	return s.dataSource, nil
}
func (s *applicationStub) UpdateDataSource(context.Context, contract.ActorContext, contract.ProjectID, string, insights.UpdateDataSourceRequest) (insights.DataSource, error) {
	return s.dataSource, nil
}
func (s *applicationStub) SetDataSourceQuality(context.Context, contract.ActorContext, contract.ProjectID, string, insights.SetDataSourceQualityRequest) (insights.DataSource, error) {
	return s.dataSource, nil
}
func (s *applicationStub) ImportMetrics(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, request insights.ImportMetricsRequest) (insights.ImportResult, error) {
	s.importedRows = len(request.Rows)
	return insights.ImportResult{Batch: s.importBatch}, nil
}
func (s *applicationStub) ListImportBatches(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, filter insights.ImportBatchFilter) ([]insights.ImportBatch, error) {
	s.importBatchFilter = filter
	return []insights.ImportBatch{s.importBatch}, nil
}
func (s *applicationStub) GetMetricOverview(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, window insights.MetricWindow) (insights.MetricOverview, error) {
	s.window = window
	return insights.MetricOverview{Window: window,
		Judgement: insights.NewJudgement(insights.ConfidenceLowSample, "窗口内样本不足，只能当作观察。")}, nil
}

func (s *applicationStub) GetPerformanceAnalysis(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, window insights.MetricWindow) (insights.PerformanceAnalysis, error) {
	s.window = window
	return insights.PerformanceAnalysis{Window: window, Comparable: true}, nil
}
func (s *applicationStub) GetDataQuality(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, window insights.MetricWindow) (insights.QualityReport, error) {
	s.window = window
	report := s.qualityReport
	report.Window = window
	return report, nil
}
func (s *applicationStub) GetCapabilityOperations(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, window insights.MetricWindow) (insights.CapabilityOperations, error) {
	s.window = window
	report := s.capabilityOperations
	report.Window = window
	return report, nil
}
func (s *applicationStub) GetInsightSettings(_ context.Context, _ contract.ActorContext, _ contract.ProjectID) (insights.InsightSettings, error) {
	return s.settings, nil
}
func (s *applicationStub) GetThresholds(_ context.Context, _ contract.ActorContext, _ contract.ProjectID) (insights.ResolvedThresholds, error) {
	return s.thresholds, nil
}
func (s *applicationStub) SaveThresholds(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, request insights.SaveThresholdsRequest) (insights.ResolvedThresholds, error) {
	s.thresholdRequest = request
	s.thresholds.Version++
	return s.thresholds, nil
}
func (s *applicationStub) ListThresholdHistory(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, _ int) ([]insights.ThresholdSet, error) {
	return s.thresholdHistory, nil
}
func (s *applicationStub) CreateExperiment(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, request insights.CreateExperimentRequest) (insights.Experiment, error) {
	s.experimentRequest = request
	return s.experiment, nil
}
func (s *applicationStub) ListExperiments(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, filter insights.ExperimentFilter) ([]insights.Experiment, error) {
	s.experimentFilter = filter
	return []insights.Experiment{s.experiment}, nil
}
func (s *applicationStub) GetExperiment(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, id string) (insights.ExperimentDetail, error) {
	s.experimentID = id
	return insights.ExperimentDetail{Experiment: s.experiment, Readout: s.readout}, nil
}
func (s *applicationStub) AttachExperimentAsset(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, experimentID, variantID string, request insights.AttachExperimentAssetRequest) (insights.AttachExperimentAssetResult, error) {
	s.experimentID, s.variantID, s.attachedAssetID = experimentID, variantID, request.AssetID
	return s.attachResult, nil
}
func (s *applicationStub) DetachExperimentAsset(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, experimentID, variantID, assetID string) (insights.ExperimentVariant, error) {
	s.experimentID, s.variantID, s.attachedAssetID = experimentID, variantID, assetID
	return s.attachResult.Variant, nil
}
func (s *applicationStub) StartExperiment(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, id string, _ int64) (insights.Experiment, error) {
	s.experimentID = id
	return s.experiment, nil
}
func (s *applicationStub) ConcludeExperiment(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, id string, request insights.ConcludeExperimentRequest) (insights.Experiment, error) {
	s.experimentID, s.interpretation = id, request.Interpretation
	return s.experiment, nil
}
func (s *applicationStub) ResolveQualityIssue(_ context.Context, _ contract.ActorContext, _ contract.ProjectID, request insights.ResolveQualityIssueRequest) (insights.QualityDisposition, error) {
	s.qualityRequest = request
	return insights.QualityDisposition{
		ID: "insightqualitydisposition_1", Fingerprint: request.Fingerprint,
		IssueKind: request.IssueKind, State: request.State, Note: request.Note, Version: 1,
	}, nil
}

func TestInsightsHTTPExposesDataIngestionSurface(t *testing.T) {
	t.Parallel()
	app := &applicationStub{
		dataSource:  insights.DataSource{ID: "insightsource_1", Version: 1},
		importBatch: insights.ImportBatch{ID: "insightbatch_1", Version: 1},
	}
	server := New(app)
	tests := []struct {
		method string
		path   string
		body   string
		status int
		want   string
	}{
		{http.MethodPost, "/api/insights/v1/projects/project_1/data-sources",
			`{"platform":"douyin","account_label":"主账户","account_ref":"adv_1","ingest_mode":"api","credential_ref":"vault://douyin","caliber":{"time_zone":"Asia/Shanghai","currency":"CNY","attribution_window":"click_7d","metric_schema_version":"v1"},"field_mapping":{"展示数":"impressions"}}`,
			201, "insightsource_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/data-sources", "", 200, "insightsource_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/data-sources/insightsource_1", "", 200, "insightsource_1"},
		{http.MethodPatch, "/api/insights/v1/projects/project_1/data-sources/insightsource_1",
			`{"expected_version":1,"status":"active","field_mapping":{"展示数":"impressions"}}`, 200, "insightsource_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/data-sources/insightsource_1:set-quality",
			`{"expected_version":2,"quality_status":"delayed","note":"平台回传延迟"}`, 200, "insightsource_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/data-sources/insightsource_1:unknown", `{}`, 404, ""},
		{http.MethodPost, "/api/insights/v1/projects/project_1/import-batches",
			`{"data_source_id":"insightsource_1","kind":"file","source_label":"7月.csv","content_hash":"hash_1","corrects_batch_id":"","register_objects":true,"rows":[{"platform_object_kind":"creative","platform_object_id":"c1","platform_object_name":"夏季前贴","stat_date":"2026-07-20","counts":{"impressions":1000,"clicks":20,"conversions":1,"video_views":0,"video_completions":0,"spend_cents":5000,"revenue_cents":0}}]}`,
			201, "insightbatch_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/import-batches", "", 200, "insightbatch_1"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/metric-overview?start=2026-07-01&end=2026-07-20", "", 200, "样本不足"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/metric-overview?start=不是日期", "", 400, "INVALID_REQUEST"},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(test.method, test.path, test.body))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
	if app.importedRows != 1 {
		t.Fatalf("导入的行应当原样传到服务层：%d", app.importedRows)
	}
}

// 业务层写在哨兵后面的那句中文要原样送到前端。
//
// 前端就是把 message 贴给用户看的。以前这里一律换成「请求参数无效」，人填错账户标识、
// 口径还是窗口，看到的都是同一句，只能靠猜。而内部错误那一支带的是数据库原文，
// 一个字都不能漏出去。
func TestErrorMessageCarriesTheHumanReadableReason(t *testing.T) {
	t.Parallel()
	app := &applicationStub{registerErr: fmt.Errorf("%w: 账户标识只能用英文字母、数字和 - _ . : / @", insights.ErrInvalidRequest)}
	server := New(app)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, authenticatedRequest(http.MethodPost, "/api/insights/v1/projects/project_1/data-sources",
		`{"platform":"douyin","account_ref":"品牌主账户","ingest_mode":"api"}`))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "账户标识只能用英文字母") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}

	app.registerErr = errors.New("Error 3988: Conversion from collation utf8mb4_general_ci into ascii_bin impossible")
	internal := httptest.NewRecorder()
	server.ServeHTTP(internal, authenticatedRequest(http.MethodPost, "/api/insights/v1/projects/project_1/data-sources",
		`{"platform":"douyin","account_ref":"品牌主账户","ingest_mode":"api"}`))
	if internal.Code != http.StatusInternalServerError || strings.Contains(internal.Body.String(), "ascii_bin") {
		t.Fatalf("内部错误的原文不能出现在响应里：status=%d body=%s", internal.Code, internal.Body.String())
	}
}

// 数据窗口必须来自 URL 并原样传下去（20 §4.1「必须显示数据窗口」），
// 缺省时给最近 30 天，而不是让服务层各自猜一个。
func TestConnectorQueriesForwardWindowAndFilters(t *testing.T) {
	t.Parallel()
	app := &applicationStub{}
	server := New(app)

	server.ServeHTTP(httptest.NewRecorder(), authenticatedRequest(http.MethodGet,
		"/api/insights/v1/projects/project_1/data-sources?status=active,draft&platform=douyin&limit=9", ""))
	if len(app.dataSourceFilter.Statuses) != 2 || app.dataSourceFilter.Statuses[0] != insights.DataSourceActive ||
		len(app.dataSourceFilter.Platforms) != 1 || app.dataSourceFilter.Platforms[0] != insights.PlatformDouyin ||
		app.dataSourceFilter.Limit != 9 {
		t.Fatalf("dataSourceFilter=%#v", app.dataSourceFilter)
	}

	server.ServeHTTP(httptest.NewRecorder(), authenticatedRequest(http.MethodGet,
		"/api/insights/v1/projects/project_1/import-batches?data_source_id=insightsource_1&status=failed,partial", ""))
	if app.importBatchFilter.DataSourceID != "insightsource_1" || len(app.importBatchFilter.Statuses) != 2 ||
		app.importBatchFilter.Statuses[0] != insights.ImportFailed {
		t.Fatalf("importBatchFilter=%#v", app.importBatchFilter)
	}

	server.ServeHTTP(httptest.NewRecorder(), authenticatedRequest(http.MethodGet,
		"/api/insights/v1/projects/project_1/metric-overview?start=2026-07-01&end=2026-07-20", ""))
	if app.window.Start.Format("2006-01-02") != "2026-07-01" || app.window.End.Format("2006-01-02") != "2026-07-20" {
		t.Fatalf("window=%#v", app.window)
	}

	server.ServeHTTP(httptest.NewRecorder(), authenticatedRequest(http.MethodGet,
		"/api/insights/v1/projects/project_1/metric-overview", ""))
	if app.window.Days() != 30 {
		t.Fatalf("缺省窗口应当是最近 30 天：%d 天", app.window.Days())
	}
}

func TestInsightsHTTPExposesDataQualitySurface(t *testing.T) {
	t.Parallel()
	app := &applicationStub{qualityReport: insights.QualityReport{
		Issues: []insights.QualityIssue{{
			Fingerprint: "freshness|lag|insightsource_1", Kind: insights.QualityFreshness,
			Severity: insights.SeverityBlocking, Title: "抖音 · 主账户 的数据滞后 6 天",
			State: insights.QualityOpen,
		}},
		ByKind: map[insights.QualityIssueKind]int{insights.QualityFreshness: 1},
		// 有阻断级问题时禁止强结论，前端靠这两个字段决定要不要收起结论区。
		StrongConclusionsAllowed: false, BlockedReason: "抖音 · 主账户 的数据滞后 6 天",
	}}
	server := New(app)
	tests := []struct {
		method string
		path   string
		body   string
		status int
		want   string
	}{
		{http.MethodGet, "/api/insights/v1/projects/project_1/data-quality?start=2026-07-01&end=2026-07-20",
			"", 200, "滞后 6 天"},
		{http.MethodGet, "/api/insights/v1/projects/project_1/data-quality?start=不是日期", "", 400, "INVALID_REQUEST"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/data-quality/dispositions",
			`{"fingerprint":"freshness|lag|insightsource_1","issue_kind":"freshness","state":"resolved","note":"已让平台侧重跑同步","observed_through":"2026-07-29T10:00:00Z"}`,
			201, "insightqualitydisposition_1"},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(test.method, test.path, test.body))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
	// observed_through 由前端回传它当时看到的那条问题的观测时间，服务端不能自己取 now，
	// 否则处置会连带盖掉处置之后才恶化的情况。
	if app.qualityRequest.ObservedThrough.Format(time.RFC3339) != "2026-07-29T10:00:00Z" {
		t.Fatalf("observed_through 没有原样传到服务层：%#v", app.qualityRequest)
	}
	if app.qualityRequest.State != insights.DispositionResolved {
		t.Fatalf("处置状态没有原样传到服务层：%#v", app.qualityRequest)
	}
}

func TestInsightsHTTPExposesCapabilityOperationsSurface(t *testing.T) {
	t.Parallel()
	app := &applicationStub{capabilityOperations: insights.CapabilityOperations{
		FeatureSystems: []insights.FeatureSystemHealth{{
			AssetType: insights.AssetTypePrerollAd, Label: "前贴片广告", AssetCount: 12,
			Fields: []insights.FeatureFieldUsage{{
				FeatureField: insights.FeatureField{Key: "opening_structure", Label: "开场结构"},
				AssetCount:   12, DistinctValues: 9,
				MergeCandidates: []string{"悬念式开场"},
			}},
		}},
		Evaluations: []insights.SkillEvaluation{{
			SkillID: "skill_preroll", SkillVersion: "v1", Reviewed: 4,
			Confidence: insights.ConfidenceLowSample,
			Note:       "样本不足 10 条，不给准确率",
		}},
	}}
	server := New(app)
	tests := []struct {
		path   string
		status int
		want   string
	}{
		{"/api/insights/v1/projects/project_1/capability-operations?start=2026-07-01&end=2026-07-20", 200, "悬念式开场"},
		// 样本不足这件事必须能从接口读出来，前端才知道该把准确率藏起来。
		{"/api/insights/v1/projects/project_1/capability-operations", 200, "low_sample"},
		{"/api/insights/v1/projects/project_1/capability-operations?start=不是日期", 400, "INVALID_REQUEST"},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(http.MethodGet, test.path, ""))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("GET %s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
	}
	if app.window.Days() != 30 {
		t.Fatalf("缺省窗口应当是最近 30 天：%d 天", app.window.Days())
	}
}

// 实验中心的路由要能和已有的 `{experiment_action}` 形式共存：
// `POST .../insight-experiments/exp_1:start` 和
// `POST .../insight-experiments/exp_1/variants/var_1/assets` 段数不同，互不遮挡。
func TestInsightsHTTPExposesExperimentSurface(t *testing.T) {
	t.Parallel()
	app := &applicationStub{
		experiment: insights.Experiment{
			ID: "insightexperiment_1", Title: "露脸开场是否提升点击",
			AssetType: insights.AssetTypePrerollAd, VariableKey: "preroll_hook_type",
			VariableLabel: "开场钩子类型", MinImpressions: 10000,
			Status: insights.ExperimentDraft, Version: 1,
			ControlledKeys: []string{}, Variants: []insights.ExperimentVariant{},
		},
		readout: insights.ExperimentReadout{
			Samples: []insights.VariantSample{{
				VariantID: "insightvariant_1", Name: "露脸", Impressions: 1200, Short: 8800,
			}},
			Comparisons: []insights.ExperimentComparison{{
				VariantID: "insightvariant_1", Blocked: true,
				Blocker: "「露脸」还差 8800 次展示才到事先定的门槛。",
			}},
			Notes: []string{},
		},
		attachResult: insights.AttachExperimentAssetResult{
			Variant:  insights.ExperimentVariant{ID: "insightvariant_1", AssetIDs: []string{"insightasset_1"}},
			Warnings: []string{"「CTA 类型」本来要求各组一致"},
		},
	}
	server := New(app)
	tests := []struct {
		method, path, body string
		status             int
		want               string
	}{
		{http.MethodGet, "/api/insights/v1/projects/project_1/insight-experiments?status=draft", "", 200, "露脸开场是否提升点击"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/insight-experiments", `{"title":"露脸开场是否提升点击","asset_type":"preroll_ad","variable_key":"preroll_hook_type","min_impressions":10000,"window_start":"2026-07-01","window_end":"2026-07-10","variants":[{"name":"露脸","variable_value":"露脸"},{"name":"不露脸","variable_value":"不露脸","is_baseline":true}]}`, 201, "insightexperiment_1"},
		// 样本不到门槛时接口里就不能有对比数字，前端才没得显示。
		{http.MethodGet, "/api/insights/v1/projects/project_1/insight-experiments/insightexperiment_1", "", 200, "还差 8800 次展示"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/insight-experiments/insightexperiment_1/variants/insightvariant_1/assets", `{"asset_id":"insightasset_1"}`, 200, "本来要求各组一致"},
		{http.MethodDelete, "/api/insights/v1/projects/project_1/insight-experiments/insightexperiment_1/variants/insightvariant_1/assets/insightasset_1", "", 200, "insightvariant_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/insight-experiments/insightexperiment_1:start", `{"expected_version":1}`, 200, "insightexperiment_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/insight-experiments/insightexperiment_1:conclude", `{"expected_version":2,"interpretation":"露脸开场确实更好"}`, 200, "insightexperiment_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/insight-experiments/insightexperiment_1:unknown", `{}`, 404, ""},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(test.method, test.path, test.body))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
	if app.experimentFilter.Status != insights.ExperimentDraft {
		t.Fatalf("状态筛选没有透传：%q", app.experimentFilter.Status)
	}
	if app.experimentID != "insightexperiment_1" || app.variantID != "insightvariant_1" {
		t.Fatalf("路径参数没有解析出来：experiment=%q variant=%q", app.experimentID, app.variantID)
	}
	// 判定不在入参里：能传判定，事先定的门槛就形同虚设。
	if app.interpretation != "露脸开场确实更好" {
		t.Fatalf("解读没有透传：%q", app.interpretation)
	}
}

// 阈值三条路由：读当前生效的一份、追加一版、看改动史。
//
// 保存用 PUT——从调用方看这是「把阈值设成这样」。POST 到同一路径会被 404，
// 而不是被当成「再追加一版」：只增版本是服务层的事，接口上不该有两种写法。
func TestThresholdRoutes(t *testing.T) {
	t.Parallel()
	app := &applicationStub{
		thresholds:       insights.ResolvedThresholds{Version: 2, SufficientImpressions: 10000},
		thresholdHistory: []insights.ThresholdSet{{ID: "thresholdset_1", Version: 1, Reason: "本项目曝光量普遍偏小"}},
	}
	server := New(app)
	tests := []struct {
		method string
		path   string
		body   string
		status int
		want   string
	}{
		{http.MethodGet, "/api/insights/v1/projects/project_1/thresholds", "", 200, `"version":2`},
		{http.MethodPut, "/api/insights/v1/projects/project_1/thresholds",
			`{"values":{"sufficient_impressions":2000},"reason":"本项目单条素材曝光量普遍在 3000 上下"}`, 200, `"version":3`},
		{http.MethodGet, "/api/insights/v1/projects/project_1/thresholds/history", "", 200, "thresholdset_1"},
		{http.MethodPost, "/api/insights/v1/projects/project_1/thresholds", `{}`, 405, ""},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, authenticatedRequest(test.method, test.path, test.body))
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
	}
	if app.thresholdRequest.Reason == "" {
		t.Error("理由没有传到服务层——它是这次改动唯一的说明")
	}
	if app.thresholdRequest.Values.SufficientImpressions == nil ||
		*app.thresholdRequest.Values.SufficientImpressions != 2000 {
		t.Error("要改的那一格没传到服务层")
	}
}
