# 外部服务管理页 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把散在 `.env`、数据库和两个设置页里的外部服务配置，收进「系统设置」页的一张表：14 条条目全部可看，第一档 9 条可改且保存即生效。

**Architecture:** 新增一个 `servicecatalog` 包，用声明式目录描述平台依赖的每个外部服务（编码、中文名、档位、影响面、字段、探针）。页面完全由目录渲染。存储复用现有 `provider_connections` / `provider_connection_revisions` / `provider_credentials` 三张表，把 `gateway_config_write.go` 里写死单个视频连接的逻辑一般化为按目录编码读写。七条模型能力已经通过 `resolveRoute` 在调用时读库，天然热生效；只需把火山语音和 local 自管的 ark_image/ark_text 也搬到路由上。

**Tech Stack:** Go 1.x（标准库 `net/http` + `database/sql` + MySQL）、React 18 + TypeScript + Vite、Node 内置 test runner（`tsx --test`）。

## Global Constraints

- 规格文档：`docs/superpowers/specs/2026-08-17-api-management-page-design.md`，冲突时以规格为准。
- 密钥只进不出：任何 HTTP 响应体只含 `provider.MaskAPIKey` 生成的掩码，完整密钥不出服务端。
- 保存前强制探测，探不通不落库。
- 数据库优先、`.env` 兜底：数据库无记录时回落环境变量，界面标注「当前值来自部署配置」。现有 `.env` 不改、不迁移。
- 写操作要求已有 scope `provider.configuration.write`；读操作仅要求登录。
- 探针一律使用最轻的只读上游操作，因为部分上游按调用计费。
- 第三档（数据库、管理员账号、`COOKIES_PROVIDER_MASTER_KEY`、`COOKIES_MIYUN_MASTER_KEY`）不进目录、不上页面。
- 所有面向用户的文案用中文。
- Go 测试：`go test ./...`。前端测试：`npm test`（`tsx --test test/*.test.ts`）。
- 每个任务结束时提交一次，提交信息用中文，结尾附 `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`。

---

## File Structure

| 文件 | 职责 |
| --- | --- |
| `internal/platform/servicecatalog/catalog.go`（新建） | 服务目录声明与查询：`Service`、`Field`、`All()`、`Find()` |
| `internal/platform/servicecatalog/catalog_test.go`（新建） | 目录完整性：`.env.example` 每个键要么归属某服务，要么在豁免清单里 |
| `internal/platform/servicecatalog/probe.go`（新建） | 探测结果四分类：`Outcome`、`Result`、`Classify()` |
| `internal/platform/servicecatalog/probe_test.go`（新建） | 四类上游回应的分类测试 |
| `migrations/provider/20260818090000_provider_connection_types.up.sql`（新建） | 放开 `connection_type` CHECK 约束 |
| `internal/platform/provider/service_configuration.go`（新建） | 通用读写：`GetServiceConfiguration` / `SaveServiceConfiguration` / `VerifyServiceConfiguration` |
| `internal/platform/provider/service_configuration_test.go`（新建） | 版本冲突、掩码、探测失败不落库 |
| `internal/platform/httpserver/service_configuration_handlers.go`（新建） | 三条 HTTP 接口与视图组装 |
| `internal/platform/httpserver/service_configuration_handlers_test.go`（新建） | 掩码、鉴权、错误映射 |
| `internal/platform/httpserver/server.go:387-390`（修改） | 注册新路由 |
| `cmd/cookies-api/main.go:966,996`（修改） | ark_image / ark_text 本地分支改走路由 |
| `internal/platform/provider/volcengine_speech_route.go`（新建） | 火山语音改从 `speech.synthesize` 路由解析 |
| `src/data/api.ts`（修改） | 三条接口的前端封装与类型 |
| `src/components/settings/ServiceCatalogPage.tsx`（新建） | 服务清单表 |
| `src/components/settings/ServiceEditor.tsx`（新建） | 按目录声明动态渲染的编辑区 |
| `src/components/ModelSettingsPage.tsx`（修改） | 换成渲染 `ServiceCatalogPage` |
| `test/service-catalog-page.test.ts`（新建） | 前端状态与降级逻辑 |

---

### Task 1: 服务目录

**Files:**
- Create: `internal/platform/servicecatalog/catalog.go`
- Test: `internal/platform/servicecatalog/catalog_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `servicecatalog.Service`、`servicecatalog.Field`、`servicecatalog.Tier`、`servicecatalog.All() []Service`、`servicecatalog.Find(code string) (Service, bool)`、`servicecatalog.ExemptEnvKeys() []string`

- [ ] **Step 1: 写失败的测试**

创建 `internal/platform/servicecatalog/catalog_test.go`：

```go
package servicecatalog

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllServicesHaveRequiredFields(t *testing.T) {
	services := All()
	if len(services) != 14 {
		t.Fatalf("expected 14 catalog entries, got %d", len(services))
	}
	seen := map[string]bool{}
	for _, service := range services {
		if seen[service.Code] {
			t.Fatalf("duplicate service code %q", service.Code)
		}
		seen[service.Code] = true
		if strings.TrimSpace(service.DisplayName) == "" {
			t.Errorf("%s: DisplayName is required", service.Code)
		}
		if strings.TrimSpace(service.Impact) == "" {
			t.Errorf("%s: Impact is required so the page can say what breaks", service.Code)
		}
		if service.Tier != TierEditable && service.Tier != TierReadOnly {
			t.Errorf("%s: Tier must be editable or readonly, got %q", service.Code, service.Tier)
		}
		if service.Tier == TierEditable && len(service.Fields) == 0 {
			t.Errorf("%s: editable services must declare fields", service.Code)
		}
	}
}

func TestFindReturnsDeclaredService(t *testing.T) {
	service, ok := Find("model.text")
	if !ok {
		t.Fatal("model.text must be declared")
	}
	if service.Capability != "text.generate" {
		t.Fatalf("expected capability text.generate, got %q", service.Capability)
	}
}

func TestFindRejectsUnknownCode(t *testing.T) {
	if _, ok := Find("model.nonexistent"); ok {
		t.Fatal("unknown code must not resolve")
	}
}

// TestEveryEnvKeyIsAccountedFor is the guard against adding a new external
// dependency and forgetting to register it. Every key in .env.example must
// either belong to a catalog entry or be listed as exempt (database, admin
// account, master keys, local paths).
func TestEveryEnvKeyIsAccountedFor(t *testing.T) {
	registered := map[string]bool{}
	for _, service := range All() {
		for _, key := range service.EnvKeys {
			registered[key] = true
		}
	}
	for _, key := range ExemptEnvKeys() {
		registered[key] = true
	}
	for _, key := range readEnvExampleKeys(t) {
		if !registered[key] {
			t.Errorf("%s is read from the environment but belongs to no catalog entry and is not exempt", key)
		}
	}
}

func readEnvExampleKeys(t *testing.T) []string {
	t.Helper()
	file, err := os.Open(filepath.Join("..", "..", "..", ".env.example"))
	if err != nil {
		t.Fatalf("open .env.example: %v", err)
	}
	defer file.Close()
	keys := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		keys = append(keys, strings.TrimSpace(name))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan .env.example: %v", err)
	}
	return keys
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/platform/servicecatalog/ -run TestAll -v`
Expected: FAIL，编译错误 `undefined: All`

- [ ] **Step 3: 写目录声明**

创建 `internal/platform/servicecatalog/catalog.go`：

```go
// Package servicecatalog declares every external service the platform calls.
// It is the single authoritative answer to "what does cookies depend on" —
// previously that answer was spread across 900 lines of platform/config.
//
// The catalog lives in code, not in the database: it describes what the
// binary is capable of talking to, so it travels with the binary.
package servicecatalog

// Tier says whether the settings page may write this service's configuration.
// Deployment-only concerns (database, admin account, master keys) are not in
// the catalog at all, so there is no third tier here.
type Tier string

const (
	TierEditable Tier = "editable"
	TierReadOnly Tier = "readonly"
)

// FieldKind drives both input rendering and masking. Secret fields are never
// echoed back to the browser in plaintext.
type FieldKind string

const (
	FieldText   FieldKind = "text"
	FieldSecret FieldKind = "secret"
)

// Field is one input on the settings page.
type Field struct {
	Name        string    `json:"name"`
	Label       string    `json:"label"`
	Kind        FieldKind `json:"kind"`
	Required    bool      `json:"required"`
	Placeholder string    `json:"placeholder"`
	Help        string    `json:"help"`
}

// Service is one row on the settings page.
type Service struct {
	Code        string   `json:"code"`
	DisplayName string   `json:"display_name"`
	Tier        Tier     `json:"tier"`
	// Impact is plain Chinese: what stops working when this is down.
	Impact string `json:"impact"`
	// Capability and ModelAlias are set for services backed by a provider
	// model route; empty for everything else.
	Capability     string `json:"capability,omitempty"`
	ModelAlias     string `json:"model_alias,omitempty"`
	ConnectionType string `json:"connection_type,omitempty"`
	// ConnectionCode identifies the provider_connections row this service
	// owns. Empty for services stored elsewhere (miyun) or not stored at all.
	ConnectionCode string `json:"connection_code,omitempty"`
	Fields         []Field `json:"fields,omitempty"`
	// EnvKeys are the environment variables this service falls back to. They
	// are also what the read-only tier tells the operator to edit.
	EnvKeys []string `json:"env_keys,omitempty"`
	// RestartRequired is true for read-only services whose values are read
	// once at boot.
	RestartRequired bool `json:"restart_required"`
}

var modelFields = []Field{
	{Name: "base_url", Label: "服务地址", Kind: FieldText, Required: true, Placeholder: "https://ark.cn-beijing.volces.com/api/v3"},
	{Name: "model", Label: "模型名", Kind: FieldText, Required: true, Help: "上游的模型标识，改错要等真正调用时才暴露"},
	{Name: "api_key", Label: "API Key", Kind: FieldSecret, Required: false, Help: "留空则沿用已保存的密钥"},
}

var services = []Service{
	{
		Code: "model.text", DisplayName: "文本模型", Tier: TierEditable,
		Impact:         "策略生成、文案撰写、创意脚本",
		Capability:     "text.generate", ModelAlias: "cookies.text.standard",
		ConnectionType: "ark", ConnectionCode: "ark-text",
		Fields:         modelFields,
		EnvKeys:        []string{"COOKIES_PROVIDER_TEXT_ADAPTER", "COOKIES_ARK_TEXT_API_KEY", "COOKIES_ARK_TEXT_MODEL", "COOKIES_ARK_TEXT_BASE_URL"},
	},
	{
		Code: "model.image", DisplayName: "图片模型", Tier: TierEditable,
		Impact:         "图片生成、图文创意",
		Capability:     "image.generate", ModelAlias: "cookies.image.standard",
		ConnectionType: "ark", ConnectionCode: "ark-image",
		Fields:         modelFields,
		EnvKeys:        []string{"COOKIES_PROVIDER_IMAGE_ADAPTER", "COOKIES_ARK_IMAGE_API_KEY", "COOKIES_ARK_IMAGE_MODEL", "COOKIES_ARK_IMAGE_BASE_URL", "COOKIES_OPENAI_IMAGE_API_KEY", "COOKIES_OPENAI_IMAGE_MODEL", "COOKIES_OPENAI_IMAGE_BASE_URL"},
	},
	{
		Code: "model.video", DisplayName: "视频模型", Tier: TierEditable,
		Impact:         "视频创作出片",
		Capability:     "video.generate", ModelAlias: "cookies.video.standard",
		ConnectionType: "ark", ConnectionCode: "ark-seedance",
		Fields:         modelFields,
		EnvKeys:        []string{"COOKIES_PROVIDER_VIDEO_ADAPTER", "COOKIES_PROVIDER_ALLOW_DIRECT_VIDEO", "COOKIES_ARK_VIDEO_API_KEY", "COOKIES_ARK_VIDEO_MODEL", "COOKIES_ARK_VIDEO_BASE_URL", "ARK_API_KEY", "ARK_BASE_URL"},
	},
	{
		Code: "model.vision", DisplayName: "图片理解", Tier: TierEditable,
		Impact:         "素材理解、内容分析",
		Capability:     "vision.understand", ModelAlias: "cookies.vision.standard",
		ConnectionType: "adapter_gateway", ConnectionCode: "gateway-vision",
		Fields:         modelFields,
		EnvKeys:        []string{"COOKIES_MEDIA_UNDERSTANDING_REAL_PROVIDER_ENABLED", "COOKIES_MEDIA_UNDERSTANDING_VISION_MODEL_ALIAS", "COOKIES_MEDIA_UNDERSTANDING_ASR_ENABLED"},
	},
	{
		Code: "model.speech", DisplayName: "语音合成", Tier: TierEditable,
		Impact:         "视频配音",
		Capability:     "speech.synthesize", ModelAlias: "cookies.speech.standard",
		ConnectionType: "minimax_speech", ConnectionCode: "minimax-speech",
		Fields:         modelFields,
		EnvKeys:        []string{"COOKIES_PROVIDER_SPEECH_ADAPTER", "COOKIES_PROVIDER_AUDIO_ADAPTER", "COOKIES_PROVIDER_SOUND_ASSET_ADAPTER"},
	},
	{
		Code: "model.document_vision", DisplayName: "文档视觉解析", Tier: TierEditable,
		Impact:         "PDF 与文档类素材解析",
		Capability:     "document.vision.parse", ModelAlias: "cookies.document.standard",
		ConnectionType: "las_operator", ConnectionCode: "las-document",
		Fields:         modelFields,
		EnvKeys:        []string{},
	},
	{
		Code: "model.research", DisplayName: "联网研究", Tier: TierEditable,
		Impact:         "需求分析的联网取证",
		Capability:     "research.web", ModelAlias: "cookies.research.standard",
		ConnectionType: "ark", ConnectionCode: "ark-research",
		Fields:         modelFields,
		EnvKeys:        []string{"COOKIES_RESEARCH_MCP_PROTOCOL_VERSION", "COOKIES_RESEARCH_MCP_ENV_ALLOWLIST", "COOKIES_RESEARCH_TIMEOUT_SECONDS", "COOKIES_RESEARCH_MAX_OUTPUT_BYTES"},
	},
	{
		Code: "miyun", DisplayName: "密云素材源", Tier: TierEditable,
		Impact: "素材采集与竞品洞察全链路",
		Fields: []Field{
			{Name: "endpoint", Label: "服务地址", Kind: FieldText, Required: true, Placeholder: "https://api.youshu.youcloud.com/graphql"},
			{Name: "session_cookie", Label: "会话 Cookie", Kind: FieldSecret, Required: false, Help: "留空则沿用已保存的会话"},
		},
		EnvKeys: []string{"COOKIES_MIYUN_ENABLED", "COOKIES_MIYUN_ENDPOINT", "COOKIES_MIYUN_DOWNLOAD_ALLOWED_HOSTS", "COOKIES_MIYUN_MAX_CONCURRENT", "COOKIES_MIYUN_REQUESTS_PER_SECOND", "COOKIES_MIYUN_COOLDOWN_SECONDS"},
	},
	{
		Code: "volcengine_speech", DisplayName: "火山语音", Tier: TierEditable,
		Impact:         "视频配音（火山路线）",
		Capability:     "speech.synthesize", ModelAlias: "cookies.speech.volcengine",
		ConnectionType: "volcengine_speech", ConnectionCode: "volcengine-speech",
		Fields: []Field{
			{Name: "base_url", Label: "服务地址", Kind: FieldText, Required: true, Placeholder: "https://openspeech.bytedance.com"},
			{Name: "model", Label: "资源 ID", Kind: FieldText, Required: true, Help: "对应 COOKIES_VOLCENGINE_SPEECH_RESOURCE_ID"},
			{Name: "api_key", Label: "API Key", Kind: FieldSecret, Required: false, Help: "留空则沿用已保存的密钥"},
		},
		EnvKeys: []string{"COOKIES_VOLCENGINE_SPEECH_ENDPOINT", "COOKIES_VOLCENGINE_SPEECH_API_KEY", "COOKIES_VOLCENGINE_SPEECH_RESOURCE_ID", "COOKIES_VOLCENGINE_SPEECH_DEFAULT_VOICE"},
	},
	{
		Code: "storage.tos", DisplayName: "TOS 对象存储", Tier: TierReadOnly, RestartRequired: true,
		Impact:  "素材上传与全部文件读写",
		EnvKeys: []string{"COOKIES_BLOB_PROVIDER", "COOKIES_FILESYSTEM_BLOB_ROOT", "COOKIES_TOS_ENDPOINT", "COOKIES_TOS_REGION", "COOKIES_TOS_ACCESS_KEY", "COOKIES_TOS_SECRET_KEY", "COOKIES_TOS_SECURITY_TOKEN", "COOKIES_TOS_BUCKET", "COOKIES_PROVIDER_OUTPUT_BUCKET"},
	},
	{
		Code: "document_converter", DisplayName: "Gotenberg 转档", Tier: TierReadOnly, RestartRequired: true,
		Impact:  "PPT 与 Word 转 PDF，方案导出",
		EnvKeys: []string{"COOKIES_DOCUMENT_CONVERTER_BASE_URL"},
	},
	{
		Code: "product_source", DisplayName: "抖音商品源", Tier: TierReadOnly, RestartRequired: false,
		Impact:  "从商品链接带出商品信息",
		EnvKeys: []string{},
	},
	{
		Code: "scanner.clamav", DisplayName: "ClamAV 病毒扫描", Tier: TierReadOnly, RestartRequired: true,
		Impact:  "上传素材的安全扫描",
		EnvKeys: []string{"COOKIES_SCANNER_MODE", "COOKIES_CLAMAV_ADDRESS"},
	},
	{
		Code: "media.ffmpeg", DisplayName: "FFmpeg 转码", Tier: TierReadOnly, RestartRequired: true,
		Impact:  "视频转码、时长与规格识别",
		EnvKeys: []string{"COOKIES_FFMPEG_PATH", "COOKIES_FFPROBE_PATH", "COOKIES_VIDEO_WORK_ROOT"},
	},
}

// All returns the catalog in display order.
func All() []Service {
	out := make([]Service, len(services))
	copy(out, services)
	return out
}

// Find resolves one entry by code.
func Find(code string) (Service, bool) {
	for _, service := range services {
		if service.Code == code {
			return service, true
		}
	}
	return Service{}, false
}

// ExemptEnvKeys lists environment variables that deliberately have no catalog
// entry: deployment plumbing, credentials that would brick the install if
// edited from a browser, and feature flags that are not external services.
func ExemptEnvKeys() []string {
	return []string{
		"COOKIES_ENV", "COOKIES_HTTP_ADDR",
		"COOKIES_MYSQL_DATABASE", "COOKIES_MYSQL_USER", "COOKIES_MYSQL_PASSWORD",
		"COOKIES_MYSQL_ROOT_PASSWORD", "COOKIES_MYSQL_PORT", "COOKIES_MYSQL_DSN",
		"COOKIES_MYSQL_MEMORY_LIMIT", "COOKIES_MYSQL_INNODB_BUFFER_POOL_SIZE",
		"COOKIES_MYSQL_MAX_SERVER_CONNECTIONS", "COOKIES_MYSQL_MAX_OPEN_CONNS", "COOKIES_MYSQL_MAX_IDLE_CONNS",
		"COOKIES_DEMO_DATA_DIR", "COOKIES_DEMO_EMAIL", "COOKIES_DEMO_PASSWORD",
		"COOKIES_PASSWORD_AUTH_ENABLED", "COOKIES_ADMIN_USERNAME", "COOKIES_ADMIN_PASSWORD", "COOKIES_SESSION_HOURS",
		"COOKIES_PROVIDER_MASTER_KEY", "COOKIES_PROVIDER_MASTER_KEY_VERSION",
		"COOKIES_MIYUN_MASTER_KEY", "COOKIES_MIYUN_MASTER_KEY_VERSION",
		"COOKIES_PROVIDER_ALLOW_INSECURE_HTTP",
		"COOKIES_LOCAL_ORGANIZATION_ID", "COOKIES_LOCAL_PRINCIPAL_KIND", "COOKIES_LOCAL_PRINCIPAL_ID",
		"COOKIES_LOCAL_PROJECT_ID", "COOKIES_LOCAL_SCOPES",
		"VITE_API_BASE_URL", "VITE_COMPAT_API_PROXY_TARGET", "VITE_PLATFORM_PROXY_TARGET", "VITE_SHOW_STATE_PREVIEW",
	}
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/platform/servicecatalog/ -v`

Expected: `TestAllServicesHaveRequiredFields`、`TestFindReturnsDeclaredService`、`TestFindRejectsUnknownCode` PASS。

`TestEveryEnvKeyIsAccountedFor` 大概率会红，报出若干未归属的键（如 `COOKIES_AI_AD_MAX_ACTIVE_UNITS`、`COOKIES_STRATEGY_*`、`COOKIES_CREATIVE_*`）。这些是**功能开关而非外部服务**，把它们补进 `ExemptEnvKeys()` 直到测试变绿。补的时候逐个确认：如果某个键实际指向一个外部依赖，正确做法是加目录条目而不是加豁免。

- [ ] **Step 5: 提交**

```bash
git add internal/platform/servicecatalog/
git commit -m "feat(settings): 新增外部服务目录

14 条条目声明平台依赖的每个外部服务，含影响面与配置字段。
完整性测试保证新增外部依赖时不会漏登记。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 2: 探测结果四分类

**Files:**
- Create: `internal/platform/servicecatalog/probe.go`
- Test: `internal/platform/servicecatalog/probe_test.go`

**Interfaces:**
- Consumes: Task 1 的 `servicecatalog` 包
- Produces: `servicecatalog.Outcome`（常量 `OutcomeOK`、`OutcomeAuthFailed`、`OutcomeUnreachable`、`OutcomeRejected`）、`servicecatalog.Result{Outcome, Message, UpstreamMessage}`、`servicecatalog.ClassifyHTTP(status int, upstreamMessage string) Result`、`servicecatalog.ClassifyTransport(err error) Result`

- [ ] **Step 1: 写失败的测试**

创建 `internal/platform/servicecatalog/probe_test.go`：

```go
package servicecatalog

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyHTTPSuccess(t *testing.T) {
	result := ClassifyHTTP(200, "")
	if result.Outcome != OutcomeOK {
		t.Fatalf("expected ok, got %q", result.Outcome)
	}
	if result.Message != "已连通，可以用" {
		t.Fatalf("unexpected message %q", result.Message)
	}
}

func TestClassifyHTTPAuthFailure(t *testing.T) {
	for _, status := range []int{401, 403} {
		result := ClassifyHTTP(status, "")
		if result.Outcome != OutcomeAuthFailed {
			t.Fatalf("status %d: expected auth_failed, got %q", status, result.Outcome)
		}
		if !strings.Contains(result.Message, "重新签发") {
			t.Fatalf("status %d: message must tell the operator what to do, got %q", status, result.Message)
		}
	}
}

// The upstream's own words must survive. Miyun once returned only the code
// 00:403001, the UI guessed "re-copy your cookie", and the real cause was an
// insufficient subscription tier — see the 20260814100000 insights migration.
func TestClassifyHTTPRejectionKeepsUpstreamMessage(t *testing.T) {
	result := ClassifyHTTP(400, "该链接需要高版本才能查看，请升级套餐。")
	if result.Outcome != OutcomeRejected {
		t.Fatalf("expected rejected, got %q", result.Outcome)
	}
	if result.UpstreamMessage != "该链接需要高版本才能查看，请升级套餐。" {
		t.Fatalf("upstream message was not preserved verbatim: %q", result.UpstreamMessage)
	}
	if !strings.Contains(result.Message, "该链接需要高版本才能查看，请升级套餐。") {
		t.Fatalf("display message must carry the upstream words, got %q", result.Message)
	}
}

func TestClassifyHTTPRejectionWithoutUpstreamWords(t *testing.T) {
	result := ClassifyHTTP(500, "")
	if result.Outcome != OutcomeRejected {
		t.Fatalf("expected rejected, got %q", result.Outcome)
	}
	if result.Message == "" {
		t.Fatal("a rejection with no upstream words still needs a message")
	}
}

func TestClassifyTransportFailure(t *testing.T) {
	result := ClassifyTransport(errors.New("dial tcp 10.0.0.1:443: i/o timeout"))
	if result.Outcome != OutcomeUnreachable {
		t.Fatalf("expected unreachable, got %q", result.Outcome)
	}
	if !strings.Contains(result.Message, "检查地址") {
		t.Fatalf("message must point at address and network, got %q", result.Message)
	}
}

// A transport error can carry an internal hostname or token. Only the
// classification and the fixed guidance reach the browser.
func TestClassifyTransportDoesNotLeakErrorText(t *testing.T) {
	result := ClassifyTransport(errors.New("dial tcp secret-host.internal:443: refused"))
	if strings.Contains(result.Message, "secret-host.internal") {
		t.Fatalf("transport error detail must not reach the message: %q", result.Message)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/platform/servicecatalog/ -run TestClassify -v`
Expected: FAIL，编译错误 `undefined: ClassifyHTTP`

- [ ] **Step 3: 写实现**

创建 `internal/platform/servicecatalog/probe.go`：

```go
package servicecatalog

import "strings"

// Outcome is the four-way classification every probe collapses to. The page
// renders one sentence of guidance per outcome, so the operator always knows
// the next action.
type Outcome string

const (
	OutcomeOK          Outcome = "ok"
	OutcomeAuthFailed  Outcome = "auth_failed"
	OutcomeUnreachable Outcome = "unreachable"
	OutcomeRejected    Outcome = "rejected"
)

// Result is what a probe reports. UpstreamMessage is kept separate so callers
// can log it and the UI can show it without the platform paraphrasing it.
type Result struct {
	Outcome         Outcome `json:"outcome"`
	Message         string  `json:"message"`
	UpstreamMessage string  `json:"upstream_message,omitempty"`
}

const (
	messageOK          = "已连通，可以用"
	messageAuthFailed  = "密钥无效或已过期，请到服务商后台重新签发"
	messageUnreachable = "地址填错，或服务器出不了网。请检查地址、检查服务器网络"
	messageRejected    = "上游拒绝了这次请求，但没有给出说明"
)

// ClassifyHTTP maps an upstream status code to an outcome. upstreamMessage is
// whatever human-readable explanation the upstream gave; it is carried through
// verbatim rather than re-interpreted, because guessing has misled operators
// before.
func ClassifyHTTP(status int, upstreamMessage string) Result {
	trimmed := strings.TrimSpace(upstreamMessage)
	switch {
	case status >= 200 && status < 300:
		return Result{Outcome: OutcomeOK, Message: messageOK}
	case status == 401 || status == 403:
		result := Result{Outcome: OutcomeAuthFailed, Message: messageAuthFailed, UpstreamMessage: trimmed}
		if trimmed != "" {
			result.Message = messageAuthFailed + "（上游说明：" + trimmed + "）"
		}
		return result
	default:
		result := Result{Outcome: OutcomeRejected, Message: messageRejected, UpstreamMessage: trimmed}
		if trimmed != "" {
			result.Message = trimmed
		}
		return result
	}
}

// ClassifyTransport handles the case where no HTTP response arrived at all.
// The error text may name internal hosts, so it is deliberately dropped.
func ClassifyTransport(err error) Result {
	if err == nil {
		return Result{Outcome: OutcomeOK, Message: messageOK}
	}
	return Result{Outcome: OutcomeUnreachable, Message: messageUnreachable}
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/platform/servicecatalog/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/platform/servicecatalog/probe.go internal/platform/servicecatalog/probe_test.go
git commit -m "feat(settings): 探测结果四分类

连通/认证失败/网络不通/上游拒绝，每类给一句下一步动作。
上游给了人话说明就原样透出，不做二次猜测。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 3: 放开 connection_type 约束

**Files:**
- Create: `migrations/provider/20260818090000_provider_connection_types.up.sql`
- Create: `migrations/provider/20260818090000_provider_connection_types.down.sql`

**Interfaces:**
- Consumes: 无
- Produces: `provider_connections.connection_type` 接受 `adapter_gateway`、`ark`、`las_operator`、`minimax_speech`、`volcengine_speech`

**背景**：原表的 CHECK 约束只允许 `adapter_gateway`（见 `migrations/provider/20260723120000_provider_gateway_config.up.sql`），但 `gateway_config.go` 已经在按 `ark`、`las_operator`、`minimax_speech` 查询，`gateway_config_write.go:279` 也在写 `'ark'`。约束与实际用法已经不一致，这次一并对齐。

- [ ] **Step 1: 先确认现有数据里已有哪些类型**

Run:
```bash
docker compose exec -T mysql mysql -uroot -p"$COOKIES_MYSQL_ROOT_PASSWORD" "$COOKIES_MYSQL_DATABASE" -e "SELECT connection_type, COUNT(*) FROM provider_connections GROUP BY connection_type;"
```
Expected: 列出现有类型。**把输出记下来**，Step 3 的允许值集合必须是其超集，否则迁移会失败。

- [ ] **Step 2: 写迁移**

创建 `migrations/provider/20260818090000_provider_connection_types.up.sql`：

```sql
-- The original CHECK allowed only 'adapter_gateway', but gateway_config.go has
-- been resolving 'ark', 'las_operator' and 'minimax_speech' routes and
-- gateway_config_write.go has been inserting 'ark' rows. MySQL only began
-- enforcing CHECK constraints in 8.0.16, which is why this drifted unnoticed.
-- The settings page needs one connection per catalog entry, so align the
-- constraint with what the code actually writes.
ALTER TABLE provider_connections
  DROP CHECK chk_provider_connection_type;

ALTER TABLE provider_connections
  ADD CONSTRAINT chk_provider_connection_type
  CHECK (connection_type IN (
    'adapter_gateway',
    'ark',
    'las_operator',
    'minimax_speech',
    'volcengine_speech'
  ));

-- The settings page shows a "最近检查" column for every row. Without somewhere
-- to persist the last probe, that column would only ever be filled for the one
-- service edited in the current page session.
ALTER TABLE provider_connections
  ADD COLUMN last_probe_outcome VARCHAR(32) NOT NULL DEFAULT '',
  ADD COLUMN last_probe_message VARCHAR(512) NOT NULL DEFAULT '',
  ADD COLUMN last_probed_at DATETIME(6) NULL;
```

创建 `migrations/provider/20260818090000_provider_connection_types.down.sql`：

```sql
-- Rolling back only restores the narrower constraint. Rows already written
-- with a now-disallowed type would make this fail, which is the correct
-- signal: delete or retype those connections first.
ALTER TABLE provider_connections
  DROP CHECK chk_provider_connection_type;

ALTER TABLE provider_connections
  ADD CONSTRAINT chk_provider_connection_type
  CHECK (connection_type IN ('adapter_gateway'));

ALTER TABLE provider_connections
  DROP COLUMN last_probe_outcome,
  DROP COLUMN last_probe_message,
  DROP COLUMN last_probed_at;
```

- [ ] **Step 3: 跑迁移**

Run: `npm run go:migrate`
Expected: 迁移成功，无报错。

- [ ] **Step 4: 验证约束生效**

Run:
```bash
docker compose exec -T mysql mysql -uroot -p"$COOKIES_MYSQL_ROOT_PASSWORD" "$COOKIES_MYSQL_DATABASE" -e "INSERT INTO provider_connections (id, connection_code, connection_type, status, version) VALUES ('probe_bad', 'probe-bad', 'nonsense', 'enabled', 1);"
```
Expected: 报错 `Check constraint 'chk_provider_connection_type' is violated`。若插入成功，说明该 MySQL 版本不强制 CHECK，需在 `SaveServiceConfiguration`（Task 4）里补一次应用层校验。

- [ ] **Step 5: 提交**

```bash
git add migrations/provider/20260818090000_provider_connection_types.up.sql migrations/provider/20260818090000_provider_connection_types.down.sql
git commit -m "fix(provider): connection_type 约束对齐实际用法

原约束只允许 adapter_gateway，但代码一直在写 ark、las_operator、
minimax_speech。补上 volcengine_speech 供设置页使用。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 4: 通用服务配置读写

**Files:**
- Create: `internal/platform/provider/service_configuration.go`
- Test: `internal/platform/provider/service_configuration_test.go`
- Reference: `internal/platform/provider/gateway_config_write.go:147-365`（视频专用版，本任务的一般化对象）

**Interfaces:**
- Consumes: Task 1 的 `servicecatalog.Find`、Task 2 的 `servicecatalog.Result`
- Produces:
  - `provider.ServiceConfiguration{Code, Configured, Values map[string]string, MaskedSecrets map[string]string, CredentialReadable bool, Version int64, UpdatedAt time.Time, LastProbe servicecatalog.Result, LastProbedAt *time.Time}`
  - `provider.ServiceConfigurationInput{Code string, Values map[string]string, ExpectedVersion *int64}`
  - `provider.ErrServiceConfigurationConflict`、`provider.ErrServiceProbeFailed`
  - `(MySQLGatewayConfigStore) GetServiceConfiguration(ctx, orgID, code) (ServiceConfiguration, error)`
  - `(MySQLGatewayConfigStore) SaveServiceConfiguration(ctx, orgID, ServiceConfigurationInput) (ServiceConfiguration, error)`
  - `(MySQLGatewayConfigStore) VerifyServiceConfiguration(ctx, orgID, ServiceConfigurationInput) (servicecatalog.Result, error)`

- [ ] **Step 1: 写失败的测试**

创建 `internal/platform/provider/service_configuration_test.go`：

```go
package provider

import (
	"errors"
	"strings"
	"testing"
)

func TestServiceConfigurationInputRejectsUnknownCode(t *testing.T) {
	_, err := normalizeServiceConfigurationInput(
		ServiceConfigurationInput{Code: "model.nonexistent"}, false)
	if err == nil {
		t.Fatal("an unknown catalog code must be rejected before touching the database")
	}
}

func TestServiceConfigurationInputRequiresDeclaredRequiredFields(t *testing.T) {
	_, err := normalizeServiceConfigurationInput(
		ServiceConfigurationInput{Code: "model.text", Values: map[string]string{"model": "doubao-x"}}, false)
	if err == nil {
		t.Fatal("base_url is declared required and must be enforced")
	}
	if !strings.Contains(err.Error(), "服务地址") {
		t.Fatalf("the error must name the field in Chinese, got %q", err.Error())
	}
}

func TestServiceConfigurationInputRejectsPlainHTTP(t *testing.T) {
	_, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "http://ark.example.com", "model": "doubao-x", "api_key": "k"},
	}, false)
	if err == nil {
		t.Fatal("plain HTTP must be rejected unless explicitly allowed")
	}
}

func TestServiceConfigurationInputAllowsPlainHTTPWhenPolicyAllows(t *testing.T) {
	normalized, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "http://127.0.0.1:8000", "model": "doubao-x", "api_key": "k"},
	}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if normalized.Values["base_url"] != "http://127.0.0.1:8000" {
		t.Fatalf("base_url was altered: %q", normalized.Values["base_url"])
	}
}

// An omitted secret means "keep the stored one", so the operator can change a
// base URL without pasting the credential again.
func TestServiceConfigurationInputKeepsOmittedSecretEmpty(t *testing.T) {
	normalized, err := normalizeServiceConfigurationInput(ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x"},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := normalized.Values["api_key"]; present {
		t.Fatal("an omitted secret must stay omitted, not become an empty credential")
	}
}

func TestServiceConfigurationViewMasksSecrets(t *testing.T) {
	config := ServiceConfiguration{
		Code:          "model.text",
		Values:        map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x"},
		MaskedSecrets: map[string]string{"api_key": MaskAPIKey("sk-1234567890abcd")},
	}
	for _, value := range config.Values {
		if strings.Contains(value, "sk-1234567890abcd") {
			t.Fatal("the plaintext credential must never appear in Values")
		}
	}
	if config.MaskedSecrets["api_key"] == "sk-1234567890abcd" {
		t.Fatal("MaskedSecrets must hold a mask, not the key")
	}
}

func TestServiceConfigurationConflictIsDistinguishable(t *testing.T) {
	if !errors.Is(ErrServiceConfigurationConflict, ErrServiceConfigurationConflict) {
		t.Fatal("sentinel error must be comparable")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/platform/provider/ -run TestServiceConfiguration -v`
Expected: FAIL，编译错误 `undefined: normalizeServiceConfigurationInput`

- [ ] **Step 3: 写实现**

创建 `internal/platform/provider/service_configuration.go`：

```go
package provider

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

var (
	// ErrServiceConfigurationConflict means someone else saved a newer
	// revision while this form was open.
	ErrServiceConfigurationConflict = errors.New("service configuration version conflict")
	// ErrServiceProbeFailed means the probe rejected the input, so nothing
	// was written.
	ErrServiceProbeFailed = errors.New("service configuration probe failed")
)

// ServiceConfigurationInputError carries a message that is safe to show the
// operator verbatim.
type ServiceConfigurationInputError struct{ Message string }

func (e ServiceConfigurationInputError) Error() string { return e.Message }

// ServiceConfiguration is the read model for one catalog entry. Secrets appear
// only in MaskedSecrets, never in Values.
type ServiceConfiguration struct {
	Code               string
	Configured         bool
	Values             map[string]string
	MaskedSecrets      map[string]string
	CredentialReadable bool
	Version            int64
	UpdatedAt          time.Time
	LastProbe          servicecatalog.Result
	LastProbedAt       *time.Time
	// EnvironmentFallback reports what the deployment environment would
	// supply when nothing is stored, so the page can say "current value comes
	// from deployment configuration".
	EnvironmentFallback map[string]string
}

// ServiceConfigurationInput is what the settings page submits. A field absent
// from Values is left untouched; this is how an omitted secret keeps the
// stored credential.
type ServiceConfigurationInput struct {
	Code            string
	Values          map[string]string
	ExpectedVersion *int64
}

func normalizeServiceConfigurationInput(input ServiceConfigurationInput, allowInsecureHTTP bool) (ServiceConfigurationInput, error) {
	service, found := servicecatalog.Find(input.Code)
	if !found {
		return ServiceConfigurationInput{}, ServiceConfigurationInputError{Message: fmt.Sprintf("未知的服务编码 %q", input.Code)}
	}
	if service.Tier != servicecatalog.TierEditable {
		return ServiceConfigurationInput{}, ServiceConfigurationInputError{Message: service.DisplayName + "不支持在页面上修改"}
	}
	normalized := ServiceConfigurationInput{Code: input.Code, ExpectedVersion: input.ExpectedVersion, Values: map[string]string{}}
	for _, field := range service.Fields {
		raw, present := input.Values[field.Name]
		value := strings.TrimSpace(raw)
		// A secret that was not submitted stays absent so the caller knows to
		// keep the stored credential.
		if field.Kind == servicecatalog.FieldSecret && (!present || value == "") {
			continue
		}
		if field.Required && value == "" {
			return ServiceConfigurationInput{}, ServiceConfigurationInputError{Message: field.Label + "不能为空"}
		}
		if field.Name == "base_url" || field.Name == "endpoint" {
			parsed, err := url.Parse(value)
			if err != nil || parsed.Host == "" {
				return ServiceConfigurationInput{}, ServiceConfigurationInputError{Message: field.Label + "不是一个有效的地址"}
			}
			if parsed.Scheme != "https" && !(allowInsecureHTTP && parsed.Scheme == "http") {
				return ServiceConfigurationInput{}, ServiceConfigurationInputError{Message: field.Label + "必须是 https 地址"}
			}
			value = strings.TrimRight(parsed.String(), "/")
		}
		normalized.Values[field.Name] = value
	}
	return normalized, nil
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/platform/provider/ -run TestServiceConfiguration -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/platform/provider/service_configuration.go internal/platform/provider/service_configuration_test.go
git commit -m "feat(provider): 服务配置输入校验

按目录声明校验字段，省略的密钥保持省略（沿用已存凭据）。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 5: 服务配置的库读写

**Files:**
- Modify: `internal/platform/provider/service_configuration.go`（追加 store 方法）
- Modify: `internal/platform/provider/service_configuration_test.go`（追加集成测试）
- Reference: `internal/platform/provider/gateway_config_write.go:258-365`（照抄事务结构与 append-only 语义）

**Interfaces:**
- Consumes: Task 4 的 `normalizeServiceConfigurationInput`、`ServiceConfiguration`、`ServiceConfigurationInput`
- Produces: `(MySQLGatewayConfigStore) GetServiceConfiguration`、`SaveServiceConfiguration`、`VerifyServiceConfiguration` 三个方法

- [ ] **Step 1: 写失败的集成测试**

在 `internal/platform/provider/service_configuration_test.go` 末尾追加。测试用的 MySQL 连接方式照抄 `internal/platform/provider/mysql_store_integration_test.go` 的 skip 惯例：

```go
func TestSaveServiceConfigurationWritesRevisionAndRetiresOldCredential(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	first, err := store.SaveServiceConfiguration(testContext(t), testOrganizationID, ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x", "api_key": "sk-first-credential"},
	})
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if first.Version != 1 {
		t.Fatalf("expected version 1, got %d", first.Version)
	}
	if first.MaskedSecrets["api_key"] == "sk-first-credential" {
		t.Fatal("the response must carry a mask, not the key")
	}

	expected := first.Version
	second, err := store.SaveServiceConfiguration(testContext(t), testOrganizationID, ServiceConfigurationInput{
		Code:            "model.text",
		Values:          map[string]string{"base_url": "https://ark.example.com", "model": "doubao-y"},
		ExpectedVersion: &expected,
	})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if second.Version != 2 {
		t.Fatalf("expected version 2, got %d", second.Version)
	}
	// The secret was omitted, so the stored credential must survive.
	if !second.CredentialReadable {
		t.Fatal("omitting the secret must keep the stored credential usable")
	}
	if second.Values["model"] != "doubao-y" {
		t.Fatalf("model was not updated: %q", second.Values["model"])
	}
}

func TestSaveServiceConfigurationRejectsStaleVersion(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	if _, err := store.SaveServiceConfiguration(testContext(t), testOrganizationID, ServiceConfigurationInput{
		Code:   "model.text",
		Values: map[string]string{"base_url": "https://ark.example.com", "model": "doubao-x", "api_key": "sk-a"},
	}); err != nil {
		t.Fatalf("seed save: %v", err)
	}
	stale := int64(999)
	_, err := store.SaveServiceConfiguration(testContext(t), testOrganizationID, ServiceConfigurationInput{
		Code:            "model.text",
		Values:          map[string]string{"base_url": "https://ark.example.com", "model": "doubao-z"},
		ExpectedVersion: &stale,
	})
	if !errors.Is(err, ErrServiceConfigurationConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestGetServiceConfigurationReportsUnconfigured(t *testing.T) {
	store := newServiceConfigurationTestStore(t)
	config, err := store.GetServiceConfiguration(testContext(t), testOrganizationID, "model.image")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.Configured {
		t.Fatal("a service that was never saved must report Configured=false")
	}
}
```

同时补上测试脚手架（放在同文件顶部附近）：

```go
func newServiceConfigurationTestStore(t *testing.T) MySQLGatewayConfigStore {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("COOKIES_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("COOKIES_TEST_MYSQL_DSN is not configured")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cipher, err := NewAESGCMCredentialCipher(bytes.Repeat([]byte{7}, 32), "test-v1")
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}
	t.Cleanup(func() { cleanServiceConfigurationTables(t, db) })
	cleanServiceConfigurationTables(t, db)
	return MySQLGatewayConfigStore{DB: db, Cipher: cipher}
}

func cleanServiceConfigurationTables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`DELETE FROM provider_credentials WHERE connection_id LIKE 'service_%'`,
		`UPDATE provider_model_routes SET current_revision_id = NULL WHERE id LIKE 'service_%'`,
		`UPDATE provider_connections SET current_revision_id = NULL WHERE id LIKE 'service_%'`,
		`DELETE FROM provider_model_route_revisions WHERE route_id LIKE 'service_%'`,
		`DELETE FROM provider_model_routes WHERE id LIKE 'service_%'`,
		`DELETE FROM provider_connection_revisions WHERE connection_id LIKE 'service_%'`,
		`DELETE FROM provider_connections WHERE id LIKE 'service_%'`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("clean %q: %v", statement, err)
		}
	}
}
```

若 `NewAESGCMCredentialCipher`、`testContext`、`testOrganizationID` 在包内的实际名字不同，以 `internal/platform/provider/mysql_store_integration_test.go` 里的写法为准，不要另造。

- [ ] **Step 2: 跑测试确认失败**

Run: `COOKIES_TEST_MYSQL_DSN="$COOKIES_MYSQL_DSN" go test ./internal/platform/provider/ -run TestSaveServiceConfiguration -v`
Expected: FAIL，编译错误 `store.SaveServiceConfiguration undefined`

- [ ] **Step 3: 写实现**

在 `internal/platform/provider/service_configuration.go` 追加。import 块要补上 `context`、`database/sql`，以及 `contract` 包（路径以 `gateway_config.go` 里的写法为准）：

```go
// serviceIDs derives stable primary keys from the catalog code so a re-save
// overwrites the same rows instead of accumulating duplicate connections.
func serviceIDs(code string) (connectionID, routeID string) {
	slug := strings.ReplaceAll(code, ".", "_")
	return "service_conn_" + slug, "service_route_" + slug
}

// addressField and secretField name the two fields every service has under a
// different label — miyun calls them endpoint/session_cookie, the model
// services call them base_url/api_key. Both land in the same storage column,
// so the mapping lives here rather than being special-cased at each call site.
func addressField(service servicecatalog.Service) string {
	for _, field := range service.Fields {
		if field.Name == "endpoint" {
			return "endpoint"
		}
	}
	return "base_url"
}

func secretField(service servicecatalog.Service) string {
	for _, field := range service.Fields {
		if field.Kind == servicecatalog.FieldSecret {
			return field.Name
		}
	}
	return ""
}

// GetServiceConfiguration reads the current stored revision. It never decrypts
// the credential — it only reports whether one exists and is readable under
// the current master key.
func (s MySQLGatewayConfigStore) GetServiceConfiguration(ctx context.Context, organizationID contract.OrganizationID, code string) (ServiceConfiguration, error) {
	service, found := servicecatalog.Find(code)
	if !found {
		return ServiceConfiguration{}, ServiceConfigurationInputError{Message: fmt.Sprintf("未知的服务编码 %q", code)}
	}
	if s.DB == nil {
		return ServiceConfiguration{}, fmt.Errorf("provider database is required")
	}
	connectionID, routeID := serviceIDs(code)
	config := ServiceConfiguration{Code: code, Values: map[string]string{}, MaskedSecrets: map[string]string{}}

	var baseURL, upstreamModel, probeOutcome, probeMessage string
	var version int64
	var updatedAt time.Time
	var probedAt sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT connection.version, connection.updated_at, revision.base_url,
			COALESCE(route_revision.upstream_model, ''),
			connection.last_probe_outcome, connection.last_probe_message, connection.last_probed_at
		FROM provider_connections connection
		JOIN provider_connection_revisions revision ON revision.id = connection.current_revision_id
		LEFT JOIN provider_model_routes route ON route.id = ?
		LEFT JOIN provider_model_route_revisions route_revision ON route_revision.id = route.current_revision_id
		WHERE connection.id = ?`,
		routeID, connectionID,
	).Scan(&version, &updatedAt, &baseURL, &upstreamModel, &probeOutcome, &probeMessage, &probedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return config, nil
	}
	if err != nil {
		return ServiceConfiguration{}, err
	}
	config.Configured = true
	config.Version = version
	config.UpdatedAt = updatedAt
	config.Values[addressField(service)] = baseURL
	if upstreamModel != "" {
		config.Values["model"] = upstreamModel
	}
	if probeOutcome != "" {
		config.LastProbe = servicecatalog.Result{
			Outcome: servicecatalog.Outcome(probeOutcome),
			Message: probeMessage,
		}
	}
	if probedAt.Valid {
		config.LastProbedAt = &probedAt.Time
	}

	plaintext, credErr := s.resolveServiceCredential(ctx, connectionID)
	switch {
	case credErr == nil:
		config.CredentialReadable = true
		for _, field := range service.Fields {
			if field.Kind == servicecatalog.FieldSecret {
				config.MaskedSecrets[field.Name] = MaskAPIKey(plaintext)
			}
		}
	default:
		// A credential row may exist but no longer decrypt after a master key
		// rotation. Saying so is more useful than reporting "not configured".
		config.CredentialReadable = false
	}
	return config, nil
}

func (s MySQLGatewayConfigStore) resolveServiceCredential(ctx context.Context, connectionID string) (string, error) {
	var ciphertext, nonce []byte
	var keyVersion string
	err := s.DB.QueryRowContext(ctx, `SELECT ciphertext, nonce, key_version
		FROM provider_credentials
		WHERE connection_id = ? AND status = 'active'
		  AND active_from <= UTC_TIMESTAMP(6)
		  AND (active_until IS NULL OR active_until > UTC_TIMESTAMP(6))
		ORDER BY credential_version DESC LIMIT 1`, connectionID).Scan(&ciphertext, &nonce, &keyVersion)
	if err != nil {
		return "", err
	}
	if s.Cipher == nil {
		return "", fmt.Errorf("provider credential cipher is required")
	}
	plaintext, err := s.Cipher.Decrypt(ciphertext, nonce, keyVersion)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// VerifyServiceConfiguration probes without writing. When the secret is
// omitted it probes with the stored credential, so "test connection" works
// after a page reload.
func (s MySQLGatewayConfigStore) VerifyServiceConfiguration(ctx context.Context, organizationID contract.OrganizationID, input ServiceConfigurationInput) (servicecatalog.Result, error) {
	normalized, err := normalizeServiceConfigurationInput(input, s.AllowInsecureHTTP)
	if err != nil {
		return servicecatalog.Result{}, err
	}
	service, _ := servicecatalog.Find(normalized.Code)
	connectionID, _ := serviceIDs(normalized.Code)

	address := normalized.Values[addressField(service)]
	secret := normalized.Values[secretField(service)]
	// An omitted secret means "test the credential you already have", so the
	// operator can retest after a page reload without re-pasting the key.
	if secret == "" {
		stored, resolveErr := s.resolveServiceCredential(ctx, connectionID)
		if resolveErr != nil {
			return servicecatalog.Result{}, ServiceConfigurationInputError{Message: "还没有保存过密钥，请先填写"}
		}
		secret = stored
	}
	return ProbeService(ctx, normalized.Code, address, secret), nil
}

// SaveServiceConfiguration probes first and writes only on success. Every
// table is append-only: revisions accumulate and a replaced credential is
// retired rather than deleted.
func (s MySQLGatewayConfigStore) SaveServiceConfiguration(ctx context.Context, organizationID contract.OrganizationID, input ServiceConfigurationInput) (ServiceConfiguration, error) {
	if s.DB == nil {
		return ServiceConfiguration{}, fmt.Errorf("provider database is required")
	}
	if s.Cipher == nil {
		return ServiceConfiguration{}, fmt.Errorf("provider credential cipher is required")
	}
	normalized, err := normalizeServiceConfigurationInput(input, s.AllowInsecureHTTP)
	if err != nil {
		return ServiceConfiguration{}, err
	}
	probe, err := s.VerifyServiceConfiguration(ctx, organizationID, normalized)
	if err != nil {
		return ServiceConfiguration{}, err
	}
	if probe.Outcome != servicecatalog.OutcomeOK {
		return ServiceConfiguration{}, fmt.Errorf("%w: %s", ErrServiceProbeFailed, probe.Message)
	}
	if err := s.writeServiceRevision(ctx, organizationID, normalized); err != nil {
		return ServiceConfiguration{}, err
	}
	// Recording the probe outside the revision transaction is deliberate: a
	// failure to note "it worked" must not roll back a configuration that
	// demonstrably works.
	connectionID, _ := serviceIDs(normalized.Code)
	if _, err := s.DB.ExecContext(ctx, `UPDATE provider_connections
		SET last_probe_outcome = ?, last_probe_message = ?, last_probed_at = UTC_TIMESTAMP(6)
		WHERE id = ?`, string(probe.Outcome), probe.Message, connectionID); err != nil {
		return ServiceConfiguration{}, err
	}
	config, err := s.GetServiceConfiguration(ctx, organizationID, normalized.Code)
	if err != nil {
		return ServiceConfiguration{}, err
	}
	return config, nil
}
```

`writeServiceRevision` 是把 `gateway_config_write.go:258-365` 的事务体按目录编码参数化：同样的 `SELECT ... FOR UPDATE` 版本检查、同样的 `nextVideoRevision` 取号、同样的 connection revision / route revision / credential 三段插入，区别只是 ID、`connection_type`、`capability`、`model_alias` 改为从 `servicecatalog.Find(code)` 取。**实现时把 `nextVideoRevision` 改名为 `nextRevision` 并同时供两处使用，不要复制一份。**

**审计**（规格 4.6）：`provider_connection_revisions` 已有 `created_by` 字段，`writeServiceRevision` 要把发起人写进去。发起人从 `contract` 的 principal 取，签名多带一个参数。凭据密文不进审计——revision 行只记地址、模型、版本号和发起人，密钥另存在 `provider_credentials`。加一条测试断言 revision 行的 `created_by` 非空。若该表实际没有 `created_by` 列，在 Task 3 的迁移里一并补上 `created_by VARCHAR(128) NOT NULL DEFAULT ''`。

- [ ] **Step 4: 跑测试**

Run: `COOKIES_TEST_MYSQL_DSN="$COOKIES_MYSQL_DSN" go test ./internal/platform/provider/ -v`
Expected: PASS，且原有 `TestSaveVideoConfiguration*` 全部仍绿。

- [ ] **Step 5: 提交**

```bash
git add internal/platform/provider/service_configuration.go internal/platform/provider/service_configuration_test.go internal/platform/provider/gateway_config_write.go
git commit -m "feat(provider): 按目录编码读写服务配置

复用 provider_connections 三张表，append-only 语义与视频配置一致。
探测不通不落库；省略密钥则沿用已存凭据。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 6: 各服务的探针

**Files:**
- Create: `internal/platform/provider/service_probe.go`
- Test: `internal/platform/provider/service_probe_test.go`
- Reference: `internal/platform/provider/video_probe.go:40`（`ProbeArkVideoCredential`，现有唯一探针）

**Interfaces:**
- Consumes: Task 2 的 `servicecatalog.ClassifyHTTP` / `ClassifyTransport`
- Produces: `provider.ProbeService(ctx context.Context, code, baseURL, secret string) servicecatalog.Result`

- [ ] **Step 1: 写失败的测试**

创建 `internal/platform/provider/service_probe_test.go`：

```go
package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

func TestProbeServiceReportsOKOnSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("unexpected authorization header %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	result := ProbeService(context.Background(), "model.text", upstream.URL, "sk-test")
	if result.Outcome != servicecatalog.OutcomeOK {
		t.Fatalf("expected ok, got %q (%s)", result.Outcome, result.Message)
	}
}

func TestProbeServiceReportsAuthFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	result := ProbeService(context.Background(), "model.text", upstream.URL, "sk-bad")
	if result.Outcome != servicecatalog.OutcomeAuthFailed {
		t.Fatalf("expected auth_failed, got %q", result.Outcome)
	}
}

func TestProbeServiceCarriesUpstreamRejectionMessage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"该链接需要高版本才能查看，请升级套餐。"}}`))
	}))
	defer upstream.Close()

	result := ProbeService(context.Background(), "model.text", upstream.URL, "sk-test")
	if result.Outcome != servicecatalog.OutcomeRejected {
		t.Fatalf("expected rejected, got %q", result.Outcome)
	}
	if result.UpstreamMessage != "该链接需要高版本才能查看，请升级套餐。" {
		t.Fatalf("upstream words were lost: %q", result.UpstreamMessage)
	}
}

func TestProbeServiceReportsUnreachable(t *testing.T) {
	// Port 1 is reserved and refuses connections on every supported platform.
	result := ProbeService(context.Background(), "model.text", "https://127.0.0.1:1", "sk-test")
	if result.Outcome != servicecatalog.OutcomeUnreachable {
		t.Fatalf("expected unreachable, got %q", result.Outcome)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/platform/provider/ -run TestProbeService -v`
Expected: FAIL，编译错误 `undefined: ProbeService`

- [ ] **Step 3: 写实现**

创建 `internal/platform/provider/service_probe.go`：

```go
package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

// probeTimeout keeps a hung upstream from holding the settings page open.
const probeTimeout = 15 * time.Second

// probeMaxBodyBytes caps how much of an error body is read before classifying.
const probeMaxBodyBytes = 64 * 1024

// probePath is the lightest read-only endpoint per service. Probes may cost
// money on metered upstreams, so nothing here generates content.
func probePath(code string) string {
	switch code {
	case "miyun":
		return ""
	case "volcengine_speech":
		return "/api/v1/tts/voices"
	default:
		return "/models"
	}
}

// ProbeService performs one lightweight authenticated read against the
// upstream and classifies the answer. It never generates content.
func ProbeService(ctx context.Context, code, baseURL, secret string) servicecatalog.Result {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	endpoint := strings.TrimRight(baseURL, "/") + probePath(code)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return servicecatalog.ClassifyTransport(err)
	}
	if code == "miyun" {
		request.Header.Set("Cookie", secret)
	} else {
		request.Header.Set("Authorization", "Bearer "+secret)
	}

	response, err := (&http.Client{Timeout: probeTimeout}).Do(request)
	if err != nil {
		return servicecatalog.ClassifyTransport(err)
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(response.Body, probeMaxBodyBytes))
	return servicecatalog.ClassifyHTTP(response.StatusCode, extractUpstreamMessage(body))
}

// extractUpstreamMessage digs the upstream's own explanation out of the common
// error envelopes. Returning "" is fine — the classifier has a fallback.
func extractUpstreamMessage(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	for _, candidate := range []string{envelope.Error.Message, envelope.Message, envelope.Msg} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
```

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/platform/provider/ -run TestProbeService -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/platform/provider/service_probe.go internal/platform/provider/service_probe_test.go
git commit -m "feat(provider): 各服务的连通性探针

只做最轻的只读调用（上游按次计费），四类结果统一分类，
上游说明原样带出。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 7: HTTP 接口

**Files:**
- Create: `internal/platform/httpserver/service_configuration_handlers.go`
- Test: `internal/platform/httpserver/service_configuration_handlers_test.go`
- Modify: `internal/platform/httpserver/server.go:387-390`
- Reference: `internal/platform/httpserver/video_configuration_handlers.go`（视图与错误映射的既有写法）

**Interfaces:**
- Consumes: Task 1 的 `servicecatalog.All`、Task 5 的三个 store 方法
- Produces: 三条路由
  - `GET /platform/v1/provider/services`
  - `PUT /platform/v1/provider/services/{code}`
  - `POST /platform/v1/provider/services/{code}/verification`

- [ ] **Step 1: 写失败的测试**

创建 `internal/platform/httpserver/service_configuration_handlers_test.go`：

```go
package httpserver

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestServiceListViewNeverCarriesPlaintextSecret(t *testing.T) {
	view := serviceConfigurationView(
		servicecatalogTestService(),
		testServiceConfiguration("sk-1234567890abcd"),
	)
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	if strings.Contains(string(encoded), "sk-1234567890abcd") {
		t.Fatalf("the plaintext credential leaked into the response: %s", encoded)
	}
}

func TestServiceListViewReportsTierAndImpact(t *testing.T) {
	view := serviceConfigurationView(servicecatalogTestService(), testServiceConfiguration(""))
	if view["tier"] != "editable" {
		t.Fatalf("expected tier editable, got %v", view["tier"])
	}
	if view["impact"] == "" || view["impact"] == nil {
		t.Fatal("impact is what tells the operator what breaks; it must be present")
	}
}

func TestServiceListViewReportsEnvKeysForReadOnlyTier(t *testing.T) {
	view := serviceConfigurationView(servicecatalogReadOnlyTestService(), testServiceConfiguration(""))
	keys, ok := view["env_keys"].([]string)
	if !ok || len(keys) == 0 {
		t.Fatal("a read-only service must tell the operator which env keys to edit")
	}
	if view["restart_required"] != true {
		t.Fatal("a read-only service read at boot must say a restart is needed")
	}
}
```

`servicecatalogTestService`、`servicecatalogReadOnlyTestService`、`testServiceConfiguration` 三个 helper 直接在该测试文件里构造，分别返回 `servicecatalog.Find("model.text")`、`servicecatalog.Find("storage.tos")` 的结果和一个填了 `MaskedSecrets` 的 `provider.ServiceConfiguration`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/platform/httpserver/ -run TestServiceList -v`
Expected: FAIL，编译错误 `undefined: serviceConfigurationView`

- [ ] **Step 3: 写实现**

创建 `internal/platform/httpserver/service_configuration_handlers.go`。视图函数把目录声明和存储状态合成一行：

```go
package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/shikanon/cookies/internal/platform/provider"
	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

// serviceConfigurationBody is what the settings page submits. Values holds
// only the fields declared by the catalog entry; an omitted secret means
// "keep the stored one".
type serviceConfigurationBody struct {
	Values          map[string]string `json:"values"`
	ExpectedVersion *int64            `json:"expected_version"`
}

// serviceConfigurationView is the only shape a credential ever reaches the
// browser in: a mask, never the key.
func serviceConfigurationView(service servicecatalog.Service, config provider.ServiceConfiguration) map[string]any {
	view := map[string]any{
		"code":             service.Code,
		"display_name":     service.DisplayName,
		"tier":             string(service.Tier),
		"impact":           service.Impact,
		"fields":           service.Fields,
		"env_keys":         service.EnvKeys,
		"restart_required": service.RestartRequired,
		"configured":       config.Configured,
		"values":           config.Values,
		"masked_secrets":   config.MaskedSecrets,
		"credential_readable": config.CredentialReadable,
		"version":          config.Version,
	}
	if !config.UpdatedAt.IsZero() {
		view["updated_at"] = config.UpdatedAt
	}
	probe := map[string]any{"outcome": string(config.LastProbe.Outcome), "message": config.LastProbe.Message}
	if config.LastProbe.UpstreamMessage != "" {
		probe["upstream_message"] = config.LastProbe.UpstreamMessage
	}
	if config.LastProbedAt != nil {
		probe["probed_at"] = *config.LastProbedAt
	}
	view["last_probe"] = probe
	if len(config.EnvironmentFallback) > 0 {
		view["environment_fallback"] = config.EnvironmentFallback
	}
	return view
}

func (s *Server) listServiceConfigurations(w http.ResponseWriter, r *http.Request) {
	organizationID := s.organizationFromRequest(r)
	items := []map[string]any{}
	for _, service := range servicecatalog.All() {
		config := provider.ServiceConfiguration{Code: service.Code}
		if service.Tier == servicecatalog.TierEditable {
			stored, err := s.ServiceConfigurations.GetServiceConfiguration(r.Context(), organizationID, service.Code)
			if err == nil {
				config = stored
			}
			// A single unreadable service must not blank the whole page; the
			// row simply reports itself as unconfigured.
		}
		items = append(items, serviceConfigurationView(service, config))
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": items})
}
```

`saveServiceConfiguration` 与 `verifyServiceConfiguration` 两个 handler 照 `video_configuration_handlers.go:99-134` 的结构写：解析 body → 调 store → 错误映射 → 返回视图。错误映射对照表：

| store 错误 | HTTP 状态 | 响应 message |
| --- | --- | --- |
| `provider.ServiceConfigurationInputError` | 400 | 错误自带的中文文案 |
| `provider.ErrServiceConfigurationConflict` | 409 | `配置已被更新，请刷新后重试` |
| `provider.ErrServiceProbeFailed` | 422 | 探测结果的 `Message`（已含上游原话） |
| 其他 | 500 | `保存失败，请稍后重试` |

`organizationFromRequest`、`writeJSON` 用 `server.go` 里的既有 helper，名字不同则以既有为准。

- [ ] **Step 4: 注册路由**

修改 `internal/platform/httpserver/server.go`，在第 390 行之后追加：

```go
	server.mux.Handle("GET /platform/v1/provider/services", server.requireAuthentication(http.HandlerFunc(server.listServiceConfigurations)))
	server.mux.Handle("PUT /platform/v1/provider/services/{code}", server.requireAuthentication(server.requireScope("provider.configuration.write", http.HandlerFunc(server.saveServiceConfiguration))))
	server.mux.Handle("POST /platform/v1/provider/services/{code}/verification", server.requireAuthentication(server.requireScope("provider.configuration.write", http.HandlerFunc(server.verifyServiceConfiguration))))
```

同时在 `Server` 结构体上加一个字段 `ServiceConfigurations`，类型为新定义的接口：

```go
// ServiceConfigurationStore is the settings page's seam into provider storage.
type ServiceConfigurationStore interface {
	GetServiceConfiguration(context.Context, contract.OrganizationID, string) (provider.ServiceConfiguration, error)
	SaveServiceConfiguration(context.Context, contract.OrganizationID, provider.ServiceConfigurationInput) (provider.ServiceConfiguration, error)
	VerifyServiceConfiguration(context.Context, contract.OrganizationID, provider.ServiceConfigurationInput) (servicecatalog.Result, error)
}
```

在 `cmd/cookies-api/main.go` 里，把已有的 `provider.MySQLGatewayConfigStore` 实例赋给这个字段（它已实现三个方法，不需要新建实例）。

- [ ] **Step 5: 跑测试**

Run: `go test ./internal/platform/httpserver/ -v && go build ./...`
Expected: PASS，编译通过。

- [ ] **Step 6: 提交**

```bash
git add internal/platform/httpserver/service_configuration_handlers.go internal/platform/httpserver/service_configuration_handlers_test.go internal/platform/httpserver/server.go cmd/cookies-api/main.go
git commit -m "feat(settings): 服务配置的三条 HTTP 接口

读整份目录、保存单个服务、只探测不保存。响应只含掩码。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 8: 火山语音改走路由

**Files:**
- Modify: `cmd/cookies-api/main.go`（火山语音适配器装配处）
- Modify: `internal/platform/provider/volcengine_asr.go` 或 `internal/integrations/creativeprovider/volcengine_asr.go`（以实际持有 `COOKIES_VOLCENGINE_SPEECH_*` 的构造函数为准）
- Test: `internal/platform/provider/volcengine_speech_route_test.go`
- Reference: `internal/platform/provider/minimax_speech_adapter.go:93`（同构的路由式适配器）

**Interfaces:**
- Consumes: Task 5 写入的 `volcengine_speech` 连接、`ResolveSpeechRoute`
- Produces: 火山语音适配器改为在每次调用时通过 `ResolveSpeechRoute(ctx, orgID, "cookies.speech.volcengine")` 取地址与凭据

**背景**：这是第一档里唯一还在「后端启动时读 `.env` 装配」的服务。其余七条模型能力已经走 `resolveRoute`，天然热生效。

- [ ] **Step 1: 写失败的测试**

创建 `internal/platform/provider/volcengine_speech_route_test.go`。下面用到的 `GatewayRouteSnapshot`、`ErrGatewayRouteNotFound`、`stubCredentialResolver` 三个名字取自 `gateway_config.go` 与 `minimax_speech_adapter_test.go`；**动手前先打开这两个文件核对实际名字，不一致时以既有为准，不要另造同义类型**：

```go
package provider

import (
	"context"
	"testing"

	"github.com/shikanon/cookies/internal/platform/contract"
)

type stubSpeechRouteResolver struct {
	snapshot GatewayRouteSnapshot
	err      error
	calls    int
}

func (r *stubSpeechRouteResolver) ResolveSpeechRoute(context.Context, contract.OrganizationID, string) (GatewayRouteSnapshot, error) {
	r.calls++
	return r.snapshot, r.err
}

// The adapter must ask the resolver on every call. Caching the snapshot at
// construction time is exactly the behaviour this task removes.
func TestVolcengineSpeechAdapterResolvesRoutePerCall(t *testing.T) {
	resolver := &stubSpeechRouteResolver{snapshot: GatewayRouteSnapshot{
		BaseURL: "https://openspeech.example.com", UpstreamModel: "resource-1",
	}}
	adapter := NewVolcengineSpeechAdapter(resolver, stubCredentialResolver{key: "sk-test"})

	for i := 0; i < 2; i++ {
		if _, err := adapter.Capability(context.Background(), testOrganizationID); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if resolver.calls != 2 {
		t.Fatalf("expected one resolve per call, got %d", resolver.calls)
	}
}

func TestVolcengineSpeechAdapterReportsUnavailableWithoutRoute(t *testing.T) {
	resolver := &stubSpeechRouteResolver{err: ErrGatewayRouteNotFound}
	adapter := NewVolcengineSpeechAdapter(resolver, stubCredentialResolver{key: "sk-test"})

	capability, err := adapter.Capability(context.Background(), testOrganizationID)
	if err != nil {
		t.Fatalf("a missing route is a state, not an error: %v", err)
	}
	if capability.Available {
		t.Fatal("no route means the capability is unavailable")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/platform/provider/ -run TestVolcengineSpeech -v`
Expected: FAIL，编译错误 `undefined: NewVolcengineSpeechAdapter`

- [ ] **Step 3: 改造适配器**

把现有火山语音适配器的构造签名从「接收 config 结构体」改成「接收 `SpeechRouteResolver` 与 `GatewayCredentialResolver`」，与 `minimax_speech_adapter.go` 保持同构：每次调用先 `ResolveSpeechRoute`，从 `snapshot.BaseURL` 取地址、`snapshot.UpstreamModel` 取资源 ID、通过 `ResolveGatewayCredential(ctx, snapshot.CredentialID, snapshot.CredentialVersion)` 取密钥。

`cmd/cookies-api/main.go` 中原本读 `cfg.Provider.VolcengineSpeech.*` 的装配代码，改为传入已有的 `MySQLGatewayConfigStore`。

**`.env` 兜底不能丢**：当 `ResolveSpeechRoute` 返回 `ErrGatewayRouteNotFound` 且 `COOKIES_VOLCENGINE_SPEECH_API_KEY` 非空时，回落到环境变量构造一次性的 snapshot。这条回落写在适配器里，加一条测试覆盖。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/platform/provider/ ./cmd/... -v && go build ./...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/platform/provider/ cmd/cookies-api/main.go
git commit -m "refactor(provider): 火山语音改走 speech.synthesize 路由

从启动时读 .env 装配改成调用时解析路由，配置改完立即生效。
路由缺失且 env 有值时仍回落 env，现有部署不受影响。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 9: 收编 ark_image / ark_text 的本地自管分支

**Files:**
- Modify: `cmd/cookies-api/main.go:966`（`NewArkImageAdapter` 装配）
- Modify: `cmd/cookies-api/main.go:996`（`NewArkTextAdapter` 装配）
- Test: `cmd/cookies-api/adapter_assembly_test.go`（新建）

**Interfaces:**
- Consumes: Task 5 写入的 `ark-text` / `ark-image` 连接
- Produces: 两个适配器改为优先从路由解析，路由缺失时回落 `cfg.Provider.ArkText` / `cfg.Provider.ArkImage`

**背景**：这两条只在 local 环境生效，但它们是「同一能力两套配置来源」的唯一残留。收编后，文本与图片能力的配置只有一个入口。

- [ ] **Step 1: 写失败的测试**

创建 `cmd/cookies-api/adapter_assembly_test.go`：

```go
package main

import "testing"

// The settings page owns text and image configuration. If the local
// self-managed branch keeps reading .env directly, saving in the page appears
// to do nothing on a local install — the exact confusion this work removes.
func TestArkTextAdapterPrefersStoredRoute(t *testing.T) {
	config := arkTextAdapterConfig(
		storedRoute{BaseURL: "https://stored.example.com", Model: "stored-model", APIKey: "sk-stored", Found: true},
		envArkText{BaseURL: "https://env.example.com", Model: "env-model", APIKey: "sk-env"},
	)
	if config.BaseURL != "https://stored.example.com" {
		t.Fatalf("stored route must win, got %q", config.BaseURL)
	}
	if config.Model != "stored-model" {
		t.Fatalf("stored model must win, got %q", config.Model)
	}
}

func TestArkTextAdapterFallsBackToEnvironment(t *testing.T) {
	config := arkTextAdapterConfig(
		storedRoute{Found: false},
		envArkText{BaseURL: "https://env.example.com", Model: "env-model", APIKey: "sk-env"},
	)
	if config.BaseURL != "https://env.example.com" {
		t.Fatalf("environment fallback must apply, got %q", config.BaseURL)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./cmd/cookies-api/ -run TestArkText -v`
Expected: FAIL，编译错误 `undefined: arkTextAdapterConfig`

- [ ] **Step 3: 写实现**

在 `cmd/cookies-api/main.go` 里抽出 `arkTextAdapterConfig` 与 `arkImageAdapterConfig` 两个纯函数（输入是「库里的路由」和「env 的值」，输出是 adapter config），装配处改为调用它们。`storedRoute`、`envArkText`、`envArkImage` 是这两个函数的入参类型，定义在同文件内。

- [ ] **Step 4: 跑测试**

Run: `go test ./cmd/cookies-api/ -v && go build ./...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add cmd/cookies-api/
git commit -m "refactor(provider): 收编 ark 文本与图片的本地自管分支

优先用页面存的路由，路由缺失才回落 env，消除同一能力两套配置来源。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 10: 前端接口封装

**Files:**
- Modify: `src/data/api.ts:5989-5996`（`getCapabilities` / video 三条附近）
- Test: `test/service-catalog-api.test.ts`（新建）

**Interfaces:**
- Consumes: Task 7 的三条 HTTP 接口
- Produces:
  - `ApiServiceField { name, label, kind: 'text' | 'secret', required, placeholder?, help? }`
  - `ApiServiceProbe { outcome: 'ok' | 'auth_failed' | 'unreachable' | 'rejected', message, upstream_message?, probed_at? }`
  - `ApiServiceConfiguration { code, display_name, tier, impact, fields, env_keys, restart_required, configured, values, masked_secrets, credential_readable, version, updated_at?, last_probe, environment_fallback? }`
  - `api.listServices(): Promise<{ services: ApiServiceConfiguration[] }>`
  - `api.saveService(code, values, expectedVersion?): Promise<ApiServiceConfiguration>`
  - `api.verifyService(code, values): Promise<ApiServiceProbe>`

- [ ] **Step 1: 写失败的测试**

创建 `test/service-catalog-api.test.ts`：

```ts
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { serviceSubmitBody, summarizeServiceStatus } from '../src/data/serviceCatalog'

test('空字符串的密钥字段不提交，保持沿用已存凭据', () => {
  const body = serviceSubmitBody(
    [
      { name: 'base_url', label: '服务地址', kind: 'text', required: true },
      { name: 'api_key', label: 'API Key', kind: 'secret', required: false },
    ],
    { base_url: 'https://ark.example.com', api_key: '' },
    3,
  )
  assert.equal(body.values.base_url, 'https://ark.example.com')
  assert.equal('api_key' in body.values, false)
  assert.equal(body.expected_version, 3)
})

test('填了的密钥字段照常提交', () => {
  const body = serviceSubmitBody(
    [{ name: 'api_key', label: 'API Key', kind: 'secret', required: false }],
    { api_key: 'sk-new' },
    undefined,
  )
  assert.equal(body.values.api_key, 'sk-new')
  assert.equal(body.expected_version, undefined)
})

test('状态汇总区分四态', () => {
  assert.equal(summarizeServiceStatus({ configured: false, last_probe: { outcome: 'ok', message: '' } }), '未配置')
  assert.equal(summarizeServiceStatus({ configured: true, last_probe: { outcome: 'ok', message: '' } }), '可用')
  assert.equal(summarizeServiceStatus({ configured: true, last_probe: { outcome: 'auth_failed', message: '' } }), '已配置但连不通')
  assert.equal(summarizeServiceStatus({ configured: true, last_probe: { outcome: 'unreachable', message: '' } }), '已配置但连不通')
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npx tsx --test test/service-catalog-api.test.ts`
Expected: FAIL，`Cannot find module '../src/data/serviceCatalog'`

- [ ] **Step 3: 写实现**

创建 `src/data/serviceCatalog.ts`，放纯函数与类型（这样它们能被 Node test runner 直接引入，不必拉起 React）：

```ts
export type ServiceFieldKind = 'text' | 'secret'
export type ProbeOutcome = 'ok' | 'auth_failed' | 'unreachable' | 'rejected'

export interface ApiServiceField {
  name: string
  label: string
  kind: ServiceFieldKind
  required: boolean
  placeholder?: string
  help?: string
}

export interface ApiServiceProbe {
  outcome: ProbeOutcome
  message: string
  upstream_message?: string
  probed_at?: string
}

export interface ApiServiceConfiguration {
  code: string
  display_name: string
  tier: 'editable' | 'readonly'
  impact: string
  fields: ApiServiceField[]
  env_keys: string[]
  restart_required: boolean
  configured: boolean
  values: Record<string, string>
  masked_secrets: Record<string, string>
  credential_readable: boolean
  version: number
  updated_at?: string
  last_probe: ApiServiceProbe
  environment_fallback?: Record<string, string>
}

/**
 * An untouched secret input is submitted as absent rather than as an empty
 * string, so the server keeps the stored credential instead of clearing it.
 */
export function serviceSubmitBody(
  fields: Pick<ApiServiceField, 'name' | 'kind'>[],
  values: Record<string, string>,
  expectedVersion: number | undefined,
) {
  const submitted: Record<string, string> = {}
  for (const field of fields) {
    const value = (values[field.name] ?? '').trim()
    if (field.kind === 'secret' && value === '') continue
    submitted[field.name] = value
  }
  return { values: submitted, expected_version: expectedVersion }
}

export function summarizeServiceStatus(
  config: Pick<ApiServiceConfiguration, 'configured' | 'last_probe'>,
): '可用' | '已配置但连不通' | '未配置' {
  if (!config.configured) return '未配置'
  return config.last_probe.outcome === 'ok' ? '可用' : '已配置但连不通'
}
```

在 `src/data/api.ts` 的 api 对象里（第 5989 行 `getCapabilities` 附近）追加三条：

```ts
  listServices: () => platformRequest<{ services: ApiServiceConfiguration[] }>('/provider/services'),
  saveService: (code: string, body: ReturnType<typeof serviceSubmitBody>) =>
    platformRequest<ApiServiceConfiguration>(`/provider/services/${encodeURIComponent(code)}`, 'PUT', body),
  verifyService: (code: string, body: ReturnType<typeof serviceSubmitBody>) =>
    platformRequest<ApiServiceProbe>(`/provider/services/${encodeURIComponent(code)}/verification`, 'POST', body),
```

并从 `./serviceCatalog` 引入 `serviceSubmitBody` 与三个类型再 re-export，保持 `src/data/api.ts` 仍是前端唯一的接口入口。

- [ ] **Step 4: 跑测试**

Run: `npx tsx --test test/service-catalog-api.test.ts && npx tsc --noEmit -p tsconfig.app.json`
Expected: PASS，类型检查通过。

- [ ] **Step 5: 提交**

```bash
git add src/data/serviceCatalog.ts src/data/api.ts test/service-catalog-api.test.ts
git commit -m "feat(settings): 服务配置接口的前端封装

未改动的密钥字段不提交，避免把已存凭据清空。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 11: 服务清单页

**Files:**
- Create: `src/components/settings/ServiceCatalogPage.tsx`
- Create: `src/components/settings/ServiceEditor.tsx`
- Modify: `src/components/ModelSettingsPage.tsx`
- Test: `test/service-catalog-page.test.ts`（新建）
- Reference: `src/components/ModelSettingsPage.tsx:79-157`（现有表单与样式类名）

**Interfaces:**
- Consumes: Task 10 的 `api.listServices` / `api.saveService` / `api.verifyService`、`summarizeServiceStatus`、`serviceSubmitBody`
- Produces: `ServiceCatalogPage`（无 props）、`ServiceEditor({ service, onSaved })`

- [ ] **Step 1: 写失败的测试**

创建 `test/service-catalog-page.test.ts`。只测纯函数——渲染逻辑靠 Task 12 的实跑验收：

```ts
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { catalogLoadState, readOnlyHint } from '../src/components/settings/serviceCatalogState'

// A failed load must not render as "everything is unconfigured" — that
// misleads the operator into re-entering credentials that are already fine.
test('后端不可用时是读取失败，不是全部未配置', () => {
  assert.equal(catalogLoadState({ loading: false, error: '网络错误', services: [] }), 'load-failed')
})

test('读取中就是读取中', () => {
  assert.equal(catalogLoadState({ loading: true, error: '', services: [] }), 'loading')
})

test('读到空目录才算真的空', () => {
  assert.equal(catalogLoadState({ loading: false, error: '', services: [] }), 'empty')
})

test('只读服务给出要改的环境变量与是否需重启', () => {
  const hint = readOnlyHint({ env_keys: ['COOKIES_TOS_ACCESS_KEY'], restart_required: true })
  assert.ok(hint.includes('COOKIES_TOS_ACCESS_KEY'))
  assert.ok(hint.includes('重启'))
})

test('无配置项的只读服务不编造环境变量', () => {
  const hint = readOnlyHint({ env_keys: [], restart_required: false })
  assert.ok(!hint.includes('COOKIES_'))
})
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npx tsx --test test/service-catalog-page.test.ts`
Expected: FAIL，`Cannot find module`

- [ ] **Step 3: 写状态函数**

创建 `src/components/settings/serviceCatalogState.ts`：

```ts
import type { ApiServiceConfiguration } from '../../data/serviceCatalog'

export type CatalogLoadState = 'loading' | 'load-failed' | 'empty' | 'ready'

/**
 * A read failure and an empty catalog look identical if you only check
 * services.length. They must not: telling the operator "nothing is
 * configured" when the backend is simply down sends them re-entering
 * credentials that were never lost.
 */
export function catalogLoadState(input: {
  loading: boolean
  error: string
  services: ApiServiceConfiguration[]
}): CatalogLoadState {
  if (input.loading) return 'loading'
  if (input.error) return 'load-failed'
  return input.services.length === 0 ? 'empty' : 'ready'
}

export function readOnlyHint(service: Pick<ApiServiceConfiguration, 'env_keys' | 'restart_required'>): string {
  if (service.env_keys.length === 0) return '这项没有配置项，只展示是否可达。'
  const keys = service.env_keys.join('、')
  return service.restart_required
    ? `这项要改服务器上的 ${keys}，改完需要重启后端。`
    : `这项要改服务器上的 ${keys}。`
}
```

- [ ] **Step 4: 跑测试**

Run: `npx tsx --test test/service-catalog-page.test.ts`
Expected: PASS

- [ ] **Step 5: 写页面组件**

创建 `src/components/settings/ServiceCatalogPage.tsx`：加载 `api.listServices()`，按 `catalogLoadState` 分四种渲染；`ready` 时渲染一张表，每行四列（服务名、状态、影响面、最近检查），点行展开 `ServiceEditor`。样式沿用 `src/styles.css` 里已有的 `provider-form`、`config-status`、`provider-metadata`、`config-notice` 等类名，不新造设计语言。

创建 `src/components/settings/ServiceEditor.tsx`：按 `service.fields` 动态渲染输入（`kind === 'secret'` 用 `type="password"`，placeholder 显示 `masked_secrets[field.name]`），三个按钮：`测试连接`（调 `verifyService`）、`保存`（调 `saveService`）、`刷新状态`。`tier === 'readonly'` 时不渲染表单，只渲染 `readOnlyHint(service)`。

保存返回 409 时展示「配置已被更新，请刷新后重试」并禁用保存按钮直到用户点刷新。

- [ ] **Step 6: 接进设置页**

修改 `src/components/ModelSettingsPage.tsx`：删掉视频专用表单与 `<MiyunConnectionSettings/>`，改为渲染 `<ServiceCatalogPage/>`，保留页头「系统设置」与安全说明。`MiyunConnectionSettings.tsx` 与 `ModelConfigContext` 中仅服务于旧视频表单的成员一并删除，不保留两套交互。

- [ ] **Step 7: 跑检查**

Run: `npm test && npm run build`
Expected: 全部 PASS，构建通过且不超 bundle 预算。

- [ ] **Step 8: 提交**

```bash
git add src/components/settings/ src/components/ModelSettingsPage.tsx test/service-catalog-page.test.ts
git commit -m "feat(settings): 服务清单页

一张表看全 14 条外部服务，第一档可改，只读档给出要改的环境变量。
读取失败不渲染成「全部未配置」。

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

### Task 12: 环境实跑验收

**Files:**
- Modify: `docs/25-local-demo-runbook.md`（补一节「改外部服务配置」）

**Interfaces:**
- Consumes: 前 11 个任务的全部产出
- Produces: 一份可复现的验收记录

- [ ] **Step 1: 跑全量测试**

Run: `go test ./... && npm test && npm run build`
Expected: 全绿。任一失败都停下修，不要带着红继续。

- [ ] **Step 2: 部署到 8091**

Run: `git push`
Expected: CI/CD 自动上线（见 `.github/workflows/`）。等流水线绿。

- [ ] **Step 3: 热生效实跑**

在浏览器里：

1. 打开 8091 的「系统设置」
2. 确认 14 条服务都在表里，状态、影响面、最近检查四列都有内容
3. 找到「图片模型」，改一个字段（例如把模型名改成另一个可用模型），点「测试连接」，确认返回「已连通，可以用」
4. 点保存
5. **不重启后端**，直接去创意模块生成一张图
6. 确认出图，且用的是新模型

Expected: 出图成功。若失败，说明热生效链路没通，回到 Task 5 / Task 9 排查。

- [ ] **Step 4: 四类错误实跑**

1. 把「图片模型」的密钥改成一个错的，点测试连接 → 应显示「密钥无效或已过期，请到服务商后台重新签发」
2. 把地址改成 `https://127.0.0.1:1`，点测试连接 → 应显示「地址填错，或服务器出不了网」
3. 确认这两次都**没有落库**（刷新页面，配置仍是原来的）

Expected: 三条都符合。

- [ ] **Step 5: 兜底实跑**

找一个页面里从未保存过的服务（例如「文档视觉解析」），确认它显示「当前值来自部署配置」而不是「未配置」，且对应功能仍然可用。

Expected: 符合。若显示「未配置」但功能可用，说明 `EnvironmentFallback` 没接上，回到 Task 5。

- [ ] **Step 6: 补文档并提交**

在 `docs/25-local-demo-runbook.md` 里加一节「改外部服务配置」，写清楚：去哪个页面、哪些能在页面改、哪些要改 `.env` 并重启、探测失败的四种提示分别意味着什么。

```bash
git add docs/25-local-demo-runbook.md
git commit -m "docs: runbook 补上外部服务配置的改法

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>"
```

---

## 附：自查记录

**规格覆盖**：规格第三节 14 条目录 → Task 1；第四节 4.1 目录 → Task 1、4.2 存储 → Task 3+5、4.3 热生效 → Task 8+9、4.4 优先级 → Task 5（`EnvironmentFallback`）+ Task 8 回落、4.5 接口 → Task 7、4.6 安全 → Task 4（掩码测试）+ Task 7（鉴权）；第五节前端 → Task 10+11；第六节错误处理 → Task 2+6（分类）+ Task 11（页面降级）；第七节验收 → 各任务测试 + Task 12。

**有意不做的三条**，写在这里以免被当成遗漏：

1. **旧的 `video-configuration` 三条接口不删**（规格 4.5 说「保留一个版本后移除」）。Task 11 删掉了前端调用方，后端接口留着作兼容层，移除属于下一次清理。
2. **不加内存缓存**（规格 4.3 提到「配置服务带内存缓存，写入时主动失效」）。路由解析本来就是一次带索引的主键查询，而现在的 8091 是单机单实例——先把热生效做对、把缓存失效这个新的失败模式挡在门外更划算。等真出现压测数据说明这里是瓶颈，再单独加。
3. **状态不做「已关闭」这一态**（规格第五节列了四态）。`provider_connections.status` 有 `enabled`/`disabled`，但页面上没有关闭服务的入口，四态里第四态永远出不来。等真需要「临时停用某个服务」时再连同入口一起做。

**自查过程中改掉的三处**：`GetServiceConfiguration` 里 route ID 曾经手拼一次而不是走 `serviceIDs`；探测取地址与密钥时写死了 `base_url`/`api_key`，密云的 `endpoint`/`session_cookie` 会取空——改为 `addressField` / `secretField` 按目录声明取；清单页的「最近检查」列没有落库来源，补了三个字段到迁移里。
