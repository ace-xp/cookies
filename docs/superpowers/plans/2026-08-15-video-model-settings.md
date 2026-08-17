# 视频生成模型设置（写入数据库）实现计划

> **For agentic workers:** 本计划按任务顺序执行，每个任务自带测试与提交。步骤用复选框（`- [ ]`）跟踪。

**Goal:** 让使用者在「系统设置」页填服务地址、密钥、模型，保存前先探测连通性，通过后加密写入 MySQL，无需重启后端即可用真实模型生成视频。

**Architecture:** 复用既有的 provider 五张表和 `MySQLGatewayConfigStore`。新增一条写入路径（`gateway_config_write.go`）和一个零成本探测（`video_probe.go`），经三个 `/platform/v1/provider/video-configuration` 接口暴露。视频适配器同时持有库内路由解析器和 `.env` 兜底凭据，每次提交任务时现查路由，查不到再回落，因此「保存即生效」。

**Tech Stack:** Go 1.24 / MySQL 8 / net/http（无第三方 HTTP 框架）/ React + TypeScript（Vite，`src/` 为准）

## Global Constraints

- 所有面向使用者的文案用中文；代码注释与提交信息沿用仓库既有英文风格。
- 明文密钥不得出现在任何响应体、日志、审计记录、错误信息中。响应只回掩码（`****` + 末 4 位）。
- 写入五张 provider 表一律走修订式追加，不原地改历史行；旧凭据置 `retired`，不删。
- 单一能力：`capability='video.generate'`，`model_alias='cookies.video.standard'`，`connection_type='ark'`，`connection_code='ark-seedance'`。固定 ID：连接 `connection_ark_seedance`、路由 `route_ark_seedance_video`（与 `scripts/configure-ark-video.ps1` 一致，两条路可互相覆盖）。
- 优先级 **库内配置 > .env 直连 > 未配置**，判定发生在每次提交任务时，不在启动装配时。
- 组织维度：路由 `organization_id` 写 NULL（全局），不做多组织隔离。
- 每个任务结束时 `go build ./... && go test ./...` 必须通过；涉及前端的任务另跑 `npm run build`。

---

### Task 1: 本地主密钥默认值与写权限 scope

库内加密需要 `COOKIES_PROVIDER_MASTER_KEY`，现在是空的；写接口需要新 scope。这两项是后续所有任务的前置，先落地。

**Files:**
- Modify: `.env`（第 36 行 `COOKIES_LOCAL_SCOPES`、第 44 行 `COOKIES_PROVIDER_VIDEO_ADAPTER`、第 72 行 `COOKIES_PROVIDER_MASTER_KEY`）
- Modify: `.env.example`（同名三处）
- Modify: `docs/25-local-demo-runbook.md:101-116`（5.1 节）

**Interfaces:**
- Produces: scope 字符串 `provider.configuration.write`，Task 6 的中间件要用；本地默认主密钥 `Y29va2llcy1sb2NhbC1wcm92aWRlci1rZXktMzJiISE=`（解 base64 为 32 字节 `cookies-local-provider-key-32b!!`）。

- [ ] **Step 1: 改 `.env` 三处**

`COOKIES_LOCAL_SCOPES` 末尾追加 `,provider.configuration.write`。

`COOKIES_PROVIDER_VIDEO_ADAPTER` 从 `fake` 改为 `ark_video`，并把上方注释块替换为：

```
# Video generation is configured from the Settings page and stored encrypted in
# MySQL. Keep ark_video here so that saved configuration takes effect without a
# restart. COOKIES_ARK_VIDEO_* below is only the fallback used when MySQL holds
# no enabled video route; leaving it empty is fine.
COOKIES_PROVIDER_VIDEO_ADAPTER=ark_video
```

`COOKIES_PROVIDER_MASTER_KEY` 填入默认值：

```
COOKIES_PROVIDER_MASTER_KEY=Y29va2llcy1sb2NhbC1wcm92aWRlci1rZXktMzJiISE=
```

- [ ] **Step 2: 把同样三处改动同步到 `.env.example`**

`.env.example` 的主密钥值处补一行中文说明：`# 本地默认值，部署环境必须换成 openssl rand -base64 32 生成的值。`

- [ ] **Step 3: 改 runbook 5.1 节**

在 5.1 节开头补一段：

```markdown
`.env.example` 已带一个仅供本地使用的默认主密钥，新克隆无需任何操作即可启动。**部署环境必须换成自己生成的值**，并且在本地 MySQL 数据存在期间不要更换——换了以后已加密的凭据无法解密，需要在设置页重填一次。
```

- [ ] **Step 4: 确认服务能起来**

Run: `go build ./... && go run ./cmd/cookies-api`（起来后 Ctrl+C）
Expected: 不再出现 `COOKIES_PROVIDER_MASTER_KEY must be base64-encoded 32 bytes`，日志出现视频适配器就绪信息。

- [ ] **Step 5: Commit**

```bash
git add .env.example docs/25-local-demo-runbook.md
git commit -m "chore(provider): ship a local default master key and video config scope"
```

（`.env` 不入库，只改本机。）

---

### Task 2: 连通性探测

纯 HTTP，不碰数据库，可独立测。用一个不存在的任务 ID 调查询接口：密钥无效上游回 401/403，密钥有效但任务不存在回 404/400，都不产生费用。

**Files:**
- Create: `internal/platform/provider/video_probe.go`
- Test: `internal/platform/provider/video_probe_test.go`

**Interfaces:**
- Produces: `ProbeArkVideoCredential(ctx context.Context, client *http.Client, baseURL, apiKey string) VideoProbeResult`；类型 `VideoProbeResult{Outcome VideoProbeOutcome; Message string}`，方法 `OK() bool`；常量 `VideoProbeOK` / `VideoProbeUnauthorized` / `VideoProbeUnreachable` / `VideoProbeUpstreamError` / `VideoProbeInvalidInput`。Task 4 存结果，Task 6 回给界面。

- [ ] **Step 1: 写失败的测试**

创建 `internal/platform/provider/video_probe_test.go`：

```go
package provider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeArkVideoCredentialMapsUpstreamResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   VideoProbeOutcome
	}{
		{"unauthorized", http.StatusUnauthorized, VideoProbeUnauthorized},
		{"forbidden", http.StatusForbidden, VideoProbeUnauthorized},
		{"task not found means the key was accepted", http.StatusNotFound, VideoProbeOK},
		{"bad request also means the key was accepted", http.StatusBadRequest, VideoProbeOK},
		{"rate limited still proves the key works", http.StatusTooManyRequests, VideoProbeOK},
		{"upstream failure", http.StatusBadGateway, VideoProbeUpstreamError},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var gotPath, gotAuthorization string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotAuthorization = r.Header.Get("Authorization")
				w.WriteHeader(testCase.status)
			}))
			defer server.Close()

			result := ProbeArkVideoCredential(t.Context(), server.Client(), server.URL, "probe-key")
			if result.Outcome != testCase.want {
				t.Fatalf("outcome = %q, want %q", result.Outcome, testCase.want)
			}
			if result.Message == "" {
				t.Fatal("probe result must carry a user-facing message")
			}
			if strings.Contains(result.Message, "probe-key") {
				t.Fatal("probe message must not echo the API key")
			}
			if gotAuthorization != "Bearer probe-key" {
				t.Fatalf("authorization = %q", gotAuthorization)
			}
			if !strings.HasSuffix(gotPath, "/contents/generations/tasks/"+videoProbeTaskID) {
				t.Fatalf("probe path = %q", gotPath)
			}
		})
	}
}

func TestProbeArkVideoCredentialReportsUnreachableEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := server.Client()
	server.Close()

	result := ProbeArkVideoCredential(t.Context(), client, server.URL, "probe-key")
	if result.Outcome != VideoProbeUnreachable {
		t.Fatalf("outcome = %q, want %q", result.Outcome, VideoProbeUnreachable)
	}
}

func TestProbeArkVideoCredentialRejectsEmptyInput(t *testing.T) {
	result := ProbeArkVideoCredential(t.Context(), http.DefaultClient, "", "")
	if result.Outcome != VideoProbeInvalidInput {
		t.Fatalf("outcome = %q, want %q", result.Outcome, VideoProbeInvalidInput)
	}
	if result.OK() {
		t.Fatal("invalid input must not report success")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/platform/provider/ -run TestProbeArkVideo -v`
Expected: 编译失败，`undefined: ProbeArkVideoCredential`。

- [ ] **Step 3: 写实现**

创建 `internal/platform/provider/video_probe.go`：

```go
package provider

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// VideoProbeOutcome classifies a zero-cost connectivity check against the Ark
// video task API. The probe queries a task ID that cannot exist, so a rejected
// credential and an accepted one are distinguishable without submitting work.
type VideoProbeOutcome string

const (
	VideoProbeOK            VideoProbeOutcome = "ok"
	VideoProbeUnauthorized  VideoProbeOutcome = "unauthorized"
	VideoProbeUnreachable   VideoProbeOutcome = "unreachable"
	VideoProbeUpstreamError VideoProbeOutcome = "upstream_error"
	VideoProbeInvalidInput  VideoProbeOutcome = "invalid_input"
)

// videoProbeTaskID is deliberately not a valid Ark task ID.
const videoProbeTaskID = "cookies-connectivity-probe"

const videoProbeTimeout = 15 * time.Second

// VideoProbeResult carries a user-facing Chinese message. It never contains the
// credential under test.
type VideoProbeResult struct {
	Outcome VideoProbeOutcome
	Message string
}

func (r VideoProbeResult) OK() bool { return r.Outcome == VideoProbeOK }

// ProbeArkVideoCredential verifies that the base URL is reachable and the API
// key is accepted. It cannot verify the model name: that would require
// submitting a billable generation task.
func ProbeArkVideoCredential(ctx context.Context, client *http.Client, baseURL, apiKey string) VideoProbeResult {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" || strings.TrimSpace(apiKey) == "" {
		return VideoProbeResult{Outcome: VideoProbeInvalidInput, Message: "服务地址和密钥都不能为空"}
	}
	if client == nil {
		client = &http.Client{Timeout: videoProbeTimeout}
	}
	probeCtx, cancel := context.WithTimeout(ctx, videoProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, base+arkVideoTaskPath+"/"+videoProbeTaskID, nil)
	if err != nil {
		return VideoProbeResult{Outcome: VideoProbeInvalidInput, Message: "服务地址不是合法的 URL"}
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return VideoProbeResult{Outcome: VideoProbeUnreachable, Message: "服务地址连不上，请检查地址和网络"}
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return VideoProbeResult{Outcome: VideoProbeUnauthorized, Message: "密钥被拒绝，请确认填的是完整的 API Key"}
	case response.StatusCode >= 500:
		return VideoProbeResult{Outcome: VideoProbeUpstreamError, Message: "模型服务暂时不可用，请稍后重试"}
	default:
		// 4xx other than 401/403 means the credential was accepted and only the
		// probe task ID was rejected, which is exactly what we are looking for.
		return VideoProbeResult{Outcome: VideoProbeOK, Message: "连接正常"}
	}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/platform/provider/ -run TestProbeArkVideo -v`
Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/platform/provider/video_probe.go internal/platform/provider/video_probe_test.go
git commit -m "feat(provider): add a zero-cost Ark video connectivity probe"
```

---

### Task 3: 记录校验结果的迁移

界面要显示「最近校验时间 / 结果」，刷新后仍在，因此要落库。

**Files:**
- Create: `migrations/provider/20260815100000_provider_connection_verification.up.sql`
- Create: `migrations/provider/20260815100000_provider_connection_verification.down.sql`

**Interfaces:**
- Produces: `provider_connections` 新增三列 `last_verified_at DATETIME(6) NULL`、`last_verification_ok TINYINT(1) NULL`、`last_verification_message VARCHAR(512) NULL`。Task 4 读写它们。

- [ ] **Step 1: 写 up 迁移**

```sql
-- The Settings page shows when a video connection was last verified and what
-- the upstream said. Persisting it here keeps the answer after a page reload.
ALTER TABLE provider_connections
  ADD COLUMN last_verified_at DATETIME(6) NULL AFTER version,
  ADD COLUMN last_verification_ok TINYINT(1) NULL AFTER last_verified_at,
  ADD COLUMN last_verification_message VARCHAR(512) NULL AFTER last_verification_ok;
```

- [ ] **Step 2: 写 down 迁移**

```sql
ALTER TABLE provider_connections
  DROP COLUMN last_verification_message,
  DROP COLUMN last_verification_ok,
  DROP COLUMN last_verified_at;
```

- [ ] **Step 3: 跑迁移**

Run: `go run ./cmd/cookies-migrate up`（若仓库用别的入口，按 `migrations/provider/README.md` 的说明执行）
Expected: 无报错。随后验证：

```bash
docker exec deployments-mysql-1 mysql -ucookies -pcookies_local_development_only cookies -e "desc provider_connections;"
```

Expected: 输出里出现三个新列。

- [ ] **Step 4: Commit**

```bash
git add migrations/provider/20260815100000_provider_connection_verification.up.sql migrations/provider/20260815100000_provider_connection_verification.down.sql
git commit -m "feat(provider): record the last connection verification result"
```

---

### Task 4: 库内读写视频配置

写入路径今天完全不存在（只有 PowerShell 脚本直接拼 SQL）。这个任务把它变成 Go 代码。

**Files:**
- Create: `internal/platform/provider/gateway_config_write.go`
- Test: `internal/platform/provider/gateway_config_write_test.go`（纯函数部分）
- Test: `internal/platform/provider/mysql_store_integration_test.go:末尾追加`（读写往返，需 `COOKIES_TEST_MYSQL_DSN`，未设置时自动 skip）

**Interfaces:**
- Consumes: Task 2 的 `VideoProbeResult`；Task 3 的三列；既有 `CredentialCipher`（`Encrypt([]byte) (ciphertext, nonce []byte, keyVersion string, err error)`）、`MySQLGatewayConfigStore{DB, Cipher, AllowInsecureHTTP, VideoConnectionType}`、`ErrGatewayRouteNotFound`。
- Produces:
  - `type VideoConfiguration struct { Configured bool; BaseURL, UpstreamModel, MaskedAPIKey string; CredentialReadable bool; UpdatedAt time.Time; LastVerifiedAt *time.Time; LastVerificationOK *bool; LastVerificationMessage string; Version int64 }`
  - `type VideoConfigurationInput struct { BaseURL, Model, APIKey string; Verification VideoProbeResult; ExpectedVersion *int64 }`
  - `(MySQLGatewayConfigStore) GetVideoConfiguration(ctx, organizationID contract.OrganizationID) (VideoConfiguration, error)`
  - `(MySQLGatewayConfigStore) SaveVideoConfiguration(ctx, organizationID contract.OrganizationID, input VideoConfigurationInput) (VideoConfiguration, error)`
  - `(MySQLGatewayConfigStore) ResolveVideoAPIKey(ctx, organizationID contract.OrganizationID) (string, error)` —— 只给「探测时沿用已存密钥」用，返回明文，绝不出接口。
  - `MaskAPIKey(value string) string`
  - `var ErrVideoConfigurationConflict = errors.New("video configuration version conflict")`
  - `var ErrVideoConfigurationCredentialMissing = errors.New("video configuration has no stored credential")`

- [ ] **Step 1: 写纯函数的失败测试**

创建 `internal/platform/provider/gateway_config_write_test.go`：

```go
package provider

import "testing"

func TestMaskAPIKeyKeepsOnlyTheTail(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"abcd":                 "****",
		"abcdefgh":             "****efgh",
		"sk-1234567890abcdef":  "****cdef",
	}
	for input, want := range cases {
		if got := MaskAPIKey(input); got != want {
			t.Fatalf("MaskAPIKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeVideoConfigurationInputRejectsBadValues(t *testing.T) {
	if _, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: "", Model: "m"}, false); err == nil {
		t.Fatal("empty base URL must be rejected")
	}
	if _, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: "https://ark.example/api/v3", Model: ""}, false); err == nil {
		t.Fatal("empty model must be rejected")
	}
	if _, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: "http://ark.example/api/v3", Model: "m"}, false); err == nil {
		t.Fatal("plain HTTP must be rejected unless explicitly allowed")
	}
	normalized, err := normalizeVideoConfigurationInput(VideoConfigurationInput{BaseURL: " https://ark.example/api/v3/ ", Model: " seedance "}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if normalized.BaseURL != "https://ark.example/api/v3" || normalized.Model != "seedance" {
		t.Fatalf("normalized = %+v", normalized)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/platform/provider/ -run "TestMaskAPIKey|TestNormalizeVideoConfiguration" -v`
Expected: `undefined: MaskAPIKey`。

- [ ] **Step 3: 写实现**

创建 `internal/platform/provider/gateway_config_write.go`：

```go
package provider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// The Settings page owns exactly one video connection and one video route.
// These identifiers match scripts/configure-ark-video.ps1 so that either path
// can overwrite the other's configuration instead of creating a duplicate.
const (
	VideoConnectionID   = "connection_ark_seedance"
	VideoConnectionCode = "ark-seedance"
	VideoRouteID        = "route_ark_seedance_video"
	VideoModelAlias     = "cookies.video.standard"
	videoCapability     = "video.generate"

	videoConnectionTimeoutSeconds = 900
	videoConnectionMaxBytes       = 209715200
)

// videoRouteConstraints mirrors the generation allowlist the PowerShell script
// writes. The Settings page does not expose these; they exist so a stored route
// still rejects unsupported durations and resolutions.
const videoRouteConstraints = `{"duration_seconds_min":4,"duration_seconds_max":15,` +
	`"aspect_ratios":["9:16","16:9","1:1"],"resolutions":["480p","720p"],` +
	`"video_input_modes":["text_only","reference_image","first_last_frame"],` +
	`"video_audio_policies":["silent","generated_audio"]}`

var (
	ErrVideoConfigurationConflict         = errors.New("video configuration version conflict")
	ErrVideoConfigurationCredentialMissing = errors.New("video configuration has no stored credential")
)

// VideoConfiguration is what the Settings page reads back. MaskedAPIKey never
// contains the credential itself.
type VideoConfiguration struct {
	Configured              bool
	BaseURL                 string
	UpstreamModel           string
	MaskedAPIKey            string
	CredentialReadable      bool
	Version                 int64
	UpdatedAt               time.Time
	LastVerifiedAt          *time.Time
	LastVerificationOK      *bool
	LastVerificationMessage string
}

// VideoConfigurationInput is what the Settings page writes. An empty APIKey
// means "keep the stored one", so changing only the base URL does not force the
// operator to paste the credential again.
type VideoConfigurationInput struct {
	BaseURL         string
	Model           string
	APIKey          string
	Verification    VideoProbeResult
	ExpectedVersion *int64
}

// MaskAPIKey renders a credential for display. Short values are fully masked.
func MaskAPIKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 4 {
		return "****"
	}
	return "****" + trimmed[len(trimmed)-4:]
}

func normalizeVideoConfigurationInput(input VideoConfigurationInput, allowInsecureHTTP bool) (VideoConfigurationInput, error) {
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.Model = strings.TrimSpace(input.Model)
	input.APIKey = strings.TrimSpace(input.APIKey)
	if input.BaseURL == "" {
		return VideoConfigurationInput{}, fmt.Errorf("服务地址不能为空")
	}
	if input.Model == "" {
		return VideoConfigurationInput{}, fmt.Errorf("模型名不能为空")
	}
	parsed, err := url.Parse(input.BaseURL)
	if err != nil || parsed.Host == "" {
		return VideoConfigurationInput{}, fmt.Errorf("服务地址不是合法的 URL")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && allowInsecureHTTP) {
		return VideoConfigurationInput{}, fmt.Errorf("服务地址必须是 HTTPS")
	}
	return input, nil
}

// GetVideoConfiguration reads the stored configuration. A stored credential
// that cannot be decrypted (master key rotated) reports CredentialReadable
// false rather than failing, so the page can ask for a re-entry.
func (s MySQLGatewayConfigStore) GetVideoConfiguration(ctx context.Context, organizationID contract.OrganizationID) (VideoConfiguration, error) {
	if s.DB == nil {
		return VideoConfiguration{}, fmt.Errorf("provider database is required")
	}
	var (
		config       VideoConfiguration
		ciphertext   []byte
		nonce        []byte
		keyVersion   string
		verifiedAt   sql.NullTime
		verifiedOK   sql.NullBool
		verifiedText sql.NullString
	)
	err := s.DB.QueryRowContext(ctx, `SELECT cr.base_url, rr.upstream_model, c.version, r.updated_at,
			c.last_verified_at, c.last_verification_ok, c.last_verification_message,
			pc.ciphertext, pc.nonce, pc.key_version
		FROM provider_model_routes r
		JOIN provider_model_route_revisions rr ON rr.id = r.current_revision_id
		JOIN provider_connections c ON c.id = rr.connection_id
		JOIN provider_connection_revisions cr ON cr.id = rr.connection_revision_id
		LEFT JOIN provider_credentials pc ON pc.connection_id = c.id AND pc.status = 'active'
			AND pc.active_from <= UTC_TIMESTAMP(6)
			AND (pc.active_until IS NULL OR pc.active_until > UTC_TIMESTAMP(6))
		WHERE r.id = ? AND r.status = 'enabled' AND c.status = 'enabled'
		ORDER BY pc.credential_version DESC
		LIMIT 1`, VideoRouteID).Scan(
		&config.BaseURL, &config.UpstreamModel, &config.Version, &config.UpdatedAt,
		&verifiedAt, &verifiedOK, &verifiedText, &ciphertext, &nonce, &keyVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return VideoConfiguration{}, nil
	}
	if err != nil {
		return VideoConfiguration{}, err
	}
	config.Configured = true
	if verifiedAt.Valid {
		instant := verifiedAt.Time
		config.LastVerifiedAt = &instant
	}
	if verifiedOK.Valid {
		outcome := verifiedOK.Bool
		config.LastVerificationOK = &outcome
	}
	config.LastVerificationMessage = verifiedText.String
	if len(ciphertext) > 0 && s.Cipher != nil {
		plaintext, decryptErr := s.Cipher.Decrypt(ciphertext, nonce, keyVersion)
		if decryptErr == nil {
			config.CredentialReadable = true
			config.MaskedAPIKey = MaskAPIKey(string(plaintext))
		}
	}
	return config, nil
}

// ResolveVideoAPIKey returns the stored plaintext credential. It exists only so
// a save that omits the API key can still be probed; the value must never leave
// the process.
func (s MySQLGatewayConfigStore) ResolveVideoAPIKey(ctx context.Context, organizationID contract.OrganizationID) (string, error) {
	if s.DB == nil {
		return "", fmt.Errorf("provider database is required")
	}
	if s.Cipher == nil {
		return "", fmt.Errorf("provider credential cipher is required")
	}
	var (
		ciphertext []byte
		nonce      []byte
		keyVersion string
	)
	err := s.DB.QueryRowContext(ctx, `SELECT ciphertext, nonce, key_version
		FROM provider_credentials
		WHERE connection_id = ? AND status = 'active'
			AND active_from <= UTC_TIMESTAMP(6)
			AND (active_until IS NULL OR active_until > UTC_TIMESTAMP(6))
		ORDER BY credential_version DESC LIMIT 1`, VideoConnectionID).Scan(&ciphertext, &nonce, &keyVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrVideoConfigurationCredentialMissing
	}
	if err != nil {
		return "", err
	}
	plaintext, err := s.Cipher.Decrypt(ciphertext, nonce, keyVersion)
	if err != nil {
		return "", ErrVideoConfigurationCredentialMissing
	}
	return string(plaintext), nil
}

// SaveVideoConfiguration writes one revision of everything the video route
// needs. Every table is append-only: revisions accumulate and replaced
// credentials are retired rather than deleted.
func (s MySQLGatewayConfigStore) SaveVideoConfiguration(ctx context.Context, organizationID contract.OrganizationID, input VideoConfigurationInput) (VideoConfiguration, error) {
	if s.DB == nil {
		return VideoConfiguration{}, fmt.Errorf("provider database is required")
	}
	if s.Cipher == nil {
		return VideoConfiguration{}, fmt.Errorf("provider credential cipher is required")
	}
	normalized, err := normalizeVideoConfigurationInput(input, s.AllowInsecureHTTP)
	if err != nil {
		return VideoConfiguration{}, err
	}
	transaction, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return VideoConfiguration{}, err
	}
	defer func() { _ = transaction.Rollback() }()

	var currentVersion int64
	err = transaction.QueryRowContext(ctx, `SELECT version FROM provider_connections WHERE id = ? FOR UPDATE`, VideoConnectionID).Scan(&currentVersion)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_connections (id, connection_code, connection_type, current_revision_id, status, version)
			VALUES (?, ?, 'ark', NULL, 'enabled', 1)`, VideoConnectionID, VideoConnectionCode); err != nil {
			return VideoConfiguration{}, err
		}
		currentVersion = 1
	case err != nil:
		return VideoConfiguration{}, err
	default:
		if normalized.ExpectedVersion != nil && *normalized.ExpectedVersion != currentVersion {
			return VideoConfiguration{}, ErrVideoConfigurationConflict
		}
	}

	connectionRevisionID, err := nextVideoRevision(ctx, transaction,
		`SELECT COALESCE(MAX(revision_number), 0) FROM provider_connection_revisions WHERE connection_id = ?`, VideoConnectionID, "connection_ark_seedance_r")
	if err != nil {
		return VideoConfiguration{}, err
	}
	if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_connection_revisions
		(id, connection_id, revision_number, base_url, timeout_seconds, max_response_bytes)
		VALUES (?, ?, ?, ?, ?, ?)`,
		connectionRevisionID.id, VideoConnectionID, connectionRevisionID.number, normalized.BaseURL,
		videoConnectionTimeoutSeconds, videoConnectionMaxBytes); err != nil {
		return VideoConfiguration{}, err
	}

	verificationOK := normalized.Verification.OK()
	if _, err = transaction.ExecContext(ctx, `UPDATE provider_connections
		SET current_revision_id = ?, status = 'enabled', version = version + 1,
			last_verified_at = UTC_TIMESTAMP(6), last_verification_ok = ?, last_verification_message = ?
		WHERE id = ?`, connectionRevisionID.id, verificationOK, normalized.Verification.Message, VideoConnectionID); err != nil {
		return VideoConfiguration{}, err
	}

	if normalized.APIKey != "" {
		ciphertext, nonce, keyVersion, encryptErr := s.Cipher.Encrypt([]byte(normalized.APIKey))
		if encryptErr != nil {
			return VideoConfiguration{}, encryptErr
		}
		if _, err = transaction.ExecContext(ctx, `UPDATE provider_credentials
			SET status = 'retired', active_until = UTC_TIMESTAMP(6)
			WHERE connection_id = ? AND status = 'active'`, VideoConnectionID); err != nil {
			return VideoConfiguration{}, err
		}
		credential, credentialErr := nextVideoRevision(ctx, transaction,
			`SELECT COALESCE(MAX(credential_version), 0) FROM provider_credentials WHERE connection_id = ?`, VideoConnectionID, "credential_ark_seedance_v")
		if credentialErr != nil {
			return VideoConfiguration{}, credentialErr
		}
		if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_credentials
			(id, connection_id, credential_version, ciphertext, nonce, key_version, status, active_from)
			VALUES (?, ?, ?, ?, ?, ?, 'active', UTC_TIMESTAMP(6))`,
			credential.id, VideoConnectionID, credential.number, ciphertext, nonce, keyVersion); err != nil {
			return VideoConfiguration{}, err
		}
	}

	if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_model_routes
		(id, organization_id, capability, model_alias, current_revision_id, status)
		VALUES (?, NULL, ?, ?, NULL, 'enabled')
		ON DUPLICATE KEY UPDATE status = 'enabled', version = version + 1`,
		VideoRouteID, videoCapability, VideoModelAlias); err != nil {
		return VideoConfiguration{}, err
	}
	routeRevision, err := nextVideoRevision(ctx, transaction,
		`SELECT COALESCE(MAX(revision_number), 0) FROM provider_model_route_revisions WHERE route_id = ?`, VideoRouteID, "route_ark_seedance_video_r")
	if err != nil {
		return VideoConfiguration{}, err
	}
	if _, err = transaction.ExecContext(ctx, `INSERT INTO provider_model_route_revisions
		(id, route_id, revision_number, connection_id, connection_revision_id, upstream_model, constraints_json)
		VALUES (?, ?, ?, ?, ?, ?, CAST(? AS JSON))`,
		routeRevision.id, VideoRouteID, routeRevision.number, VideoConnectionID,
		connectionRevisionID.id, normalized.Model, videoRouteConstraints); err != nil {
		return VideoConfiguration{}, err
	}
	if _, err = transaction.ExecContext(ctx, `UPDATE provider_model_routes SET current_revision_id = ? WHERE id = ?`,
		routeRevision.id, VideoRouteID); err != nil {
		return VideoConfiguration{}, err
	}
	if err = transaction.Commit(); err != nil {
		return VideoConfiguration{}, err
	}
	return s.GetVideoConfiguration(ctx, organizationID)
}

type videoRevisionID struct {
	id     string
	number int64
}

func nextVideoRevision(ctx context.Context, transaction *sql.Tx, query, parent, prefix string) (videoRevisionID, error) {
	var current int64
	if err := transaction.QueryRowContext(ctx, query, parent).Scan(&current); err != nil {
		return videoRevisionID{}, err
	}
	next := current + 1
	return videoRevisionID{id: fmt.Sprintf("%s%d", prefix, next), number: next}, nil
}
```

- [ ] **Step 4: 跑纯函数测试确认通过**

Run: `go test ./internal/platform/provider/ -run "TestMaskAPIKey|TestNormalizeVideoConfiguration" -v`
Expected: PASS。

- [ ] **Step 5: 写读写往返的集成测试**

在 `internal/platform/provider/mysql_store_integration_test.go` 末尾追加：

```go
func TestVideoConfigurationRoundTrip(t *testing.T) {
	dsn := os.Getenv("COOKIES_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COOKIES_TEST_MYSQL_DSN is not configured")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	defer db.Close()
	cipher, err := NewAESGCMCredentialCipher("Y29va2llcy1sb2NhbC1wcm92aWRlci1rZXktMzJiISE=", "test-v1")
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}
	store := MySQLGatewayConfigStore{DB: db, Cipher: cipher, VideoConnectionType: "ark"}

	saved, err := store.SaveVideoConfiguration(t.Context(), "org_local", VideoConfigurationInput{
		BaseURL:      "https://ark.cn-beijing.volces.com/api/v3",
		Model:        "doubao-seedance-1-0-lite-t2v-250428",
		APIKey:       "first-secret-key",
		Verification: VideoProbeResult{Outcome: VideoProbeOK, Message: "连接正常"},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !saved.Configured || saved.MaskedAPIKey != "****-key" {
		t.Fatalf("saved = %+v", saved)
	}
	if saved.LastVerificationOK == nil || !*saved.LastVerificationOK {
		t.Fatal("verification result was not persisted")
	}

	// Changing only the model must keep the stored credential usable.
	if _, err = store.SaveVideoConfiguration(t.Context(), "org_local", VideoConfigurationInput{
		BaseURL:      "https://ark.cn-beijing.volces.com/api/v3",
		Model:        "doubao-seedance-1-0-pro-250528",
		Verification: VideoProbeResult{Outcome: VideoProbeOK, Message: "连接正常"},
	}); err != nil {
		t.Fatalf("save without key: %v", err)
	}
	key, err := store.ResolveVideoAPIKey(t.Context(), "org_local")
	if err != nil || key != "first-secret-key" {
		t.Fatalf("ResolveVideoAPIKey = %q, %v", key, err)
	}

	// Replacing the key retires the old credential instead of deleting it.
	if _, err = store.SaveVideoConfiguration(t.Context(), "org_local", VideoConfigurationInput{
		BaseURL:      "https://ark.cn-beijing.volces.com/api/v3",
		Model:        "doubao-seedance-1-0-pro-250528",
		APIKey:       "second-secret-key",
		Verification: VideoProbeResult{Outcome: VideoProbeOK, Message: "连接正常"},
	}); err != nil {
		t.Fatalf("replace key: %v", err)
	}
	var retired int
	if err = db.QueryRow(`SELECT COUNT(*) FROM provider_credentials WHERE connection_id = ? AND status = 'retired'`, VideoConnectionID).Scan(&retired); err != nil {
		t.Fatalf("count retired: %v", err)
	}
	if retired == 0 {
		t.Fatal("the replaced credential must be retired, not deleted")
	}

	// The saved route must be resolvable by the adapter path.
	snapshot, err := store.ResolveVideoRoute(t.Context(), "org_local", VideoModelAlias)
	if err != nil {
		t.Fatalf("ResolveVideoRoute: %v", err)
	}
	if snapshot.UpstreamModel != "doubao-seedance-1-0-pro-250528" {
		t.Fatalf("snapshot model = %q", snapshot.UpstreamModel)
	}
}
```

- [ ] **Step 6: 跑集成测试**

Run:
```bash
COOKIES_TEST_MYSQL_DSN='cookies:cookies_local_development_only@tcp(127.0.0.1:3307)/cookies?parseTime=true&multiStatements=true' go test ./internal/platform/provider/ -run TestVideoConfigurationRoundTrip -v
```
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add internal/platform/provider/gateway_config_write.go internal/platform/provider/gateway_config_write_test.go internal/platform/provider/mysql_store_integration_test.go
git commit -m "feat(provider): write video connection, credential, and route from Go"
```

---

### Task 5: 库内路由缺失时回落到 .env

要做到「保存即生效」，服务必须始终装配库内路由解析器；库里没配置时才用 `.env` 的凭据。

**Files:**
- Modify: `internal/platform/provider/service.go:217`（结构体加字段）
- Modify: `internal/platform/provider/video.go:232-239`（解析逻辑）
- Modify: `internal/platform/provider/ark_video_adapter.go:54`（新增混合构造函数）
- Test: `internal/platform/provider/video_test.go`（追加）

**Interfaces:**
- Consumes: Task 4 的 `ErrGatewayRouteNotFound` 路径。
- Produces: `Service.VideoRouteOptional bool`；`NewArkVideoAdapterWithRoutes(config ArkVideoConfig, credentials GatewayCredentialResolver, handles OutputHandleStore) (*ArkVideoAdapter, error)`。Task 7 装配时用。

- [ ] **Step 1: 写失败的测试**

在 `internal/platform/provider/video_test.go` 末尾追加：

```go
type missingVideoRouteResolver struct{}

func (missingVideoRouteResolver) ResolveVideoRoute(context.Context, contract.OrganizationID, string) (VideoRouteSnapshot, error) {
	return VideoRouteSnapshot{}, fmt.Errorf("%w: none configured", ErrGatewayRouteNotFound)
}

func TestCreateVideoJobFallsBackToEnvironmentCredentialWhenRouteIsOptional(t *testing.T) {
	service := newTestVideoService(t)
	service.VideoRoutes = missingVideoRouteResolver{}
	service.VideoRouteOptional = true

	job, _, err := service.CreateVideoJob(t.Context(), validVideoJobRequest())
	if err != nil {
		t.Fatalf("CreateVideoJob: %v", err)
	}
	if job.ID == "" {
		t.Fatal("expected a job to be created without a stored route")
	}
}

func TestCreateVideoJobRejectsMissingRouteWithoutEnvironmentFallback(t *testing.T) {
	service := newTestVideoService(t)
	service.VideoRoutes = missingVideoRouteResolver{}
	service.VideoRouteOptional = false

	if _, _, err := service.CreateVideoJob(t.Context(), validVideoJobRequest()); err == nil {
		t.Fatal("expected an error when no route and no environment fallback exist")
	}
}
```

> 实施说明：`newTestVideoService(t)` 和 `validVideoJobRequest()` 是本文件里已有的测试辅助。若名字不同，照现有 `TestCreateVideoJob*` 用例的构造方式复制一份，不要新造 helper。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/platform/provider/ -run TestCreateVideoJob -v`
Expected: `service.VideoRouteOptional undefined`。

- [ ] **Step 3: 加字段并改解析逻辑**

`internal/platform/provider/service.go:217` 之后加一行：

```go
	VideoRoutes   VideoRouteResolver
	// VideoRouteOptional lets video generation fall back to the adapter's
	// environment credential when MySQL holds no enabled route. It is set when
	// COOKIES_ARK_VIDEO_API_KEY is present, so an operator who has not visited
	// the Settings page yet still gets a working pipeline.
	VideoRouteOptional bool
```

`internal/platform/provider/video.go:233-239` 改为：

```go
	var route *VideoRouteSnapshot
	if s.VideoRoutes != nil {
		resolved, resolveErr := s.VideoRoutes.ResolveVideoRoute(ctx, request.Actor.OrganizationID, request.ModelAlias)
		switch {
		case resolveErr == nil:
			route = &resolved
		case errors.Is(resolveErr, ErrGatewayRouteNotFound) && s.VideoRouteOptional:
			// Nothing stored yet: leave the route nil so the adapter uses the
			// credential it was built with.
			route = nil
		default:
			return contract.ProviderJob{}, false, fmt.Errorf("resolve provider video route: %w", resolveErr)
		}
	}
```

若 `video.go` 尚未 import `errors`，补上。

- [ ] **Step 4: 加混合构造函数**

在 `internal/platform/provider/ark_video_adapter.go` 的 `NewArkVideoAdapter` 之后追加：

```go
// NewArkVideoAdapterWithRoutes builds an adapter that prefers a stored route and
// falls back to environment credentials when the caller passes no route. Both
// halves are optional: an empty config means "stored route required", and a nil
// resolver means "environment only".
func NewArkVideoAdapterWithRoutes(config ArkVideoConfig, credentials GatewayCredentialResolver, handles OutputHandleStore) (*ArkVideoAdapter, error) {
	if handles == nil {
		return nil, fmt.Errorf("Ark video output handle store is required")
	}
	if credentials == nil && (strings.TrimSpace(config.APIKey) == "" || strings.TrimSpace(config.Model) == "") {
		return nil, fmt.Errorf("Ark video needs either a credential resolver or an API key and model")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" && strings.TrimSpace(config.APIKey) != "" {
		baseURL = arkVideoDefaultBaseURL
	}
	return &ArkVideoAdapter{
		apiKey: strings.TrimSpace(config.APIKey), model: strings.TrimSpace(config.Model), baseURL: baseURL,
		client: &http.Client{Timeout: 3 * time.Minute}, handles: handles, credentials: credentials, now: time.Now,
	}, nil
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/platform/provider/ -v`
Expected: 全部 PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/platform/provider/service.go internal/platform/provider/video.go internal/platform/provider/ark_video_adapter.go internal/platform/provider/video_test.go
git commit -m "feat(provider): prefer the stored video route and fall back to env credentials"
```

---

### Task 6: 三个 HTTP 接口

照米云连接设置（`internal/systems/insights/httpapi/miyun.go:20-22`）的 GET / PUT / `:verify` 形状。

**Files:**
- Modify: `internal/platform/httpserver/server.go:319`（接口定义）、`:359` 附近（路由注册）
- Modify: `internal/platform/httpserver/handlers.go:403` 之后（三个 handler）
- Test: `internal/platform/httpserver/handlers_test.go`（追加）

**Interfaces:**
- Consumes: Task 4 的 `GetVideoConfiguration` / `SaveVideoConfiguration` / `ResolveVideoAPIKey` / `ErrVideoConfigurationConflict` / `ErrVideoConfigurationCredentialMissing`；Task 2 的 `ProbeArkVideoCredential`。
- Produces: 三条路由与响应体；接口 `ProviderVideoConfigurationStore`，Task 7 装配时把 `MySQLGatewayConfigStore` 塞进去。

- [ ] **Step 1: 写失败的测试**

在 `internal/platform/httpserver/handlers_test.go` 末尾追加：

```go
type stubVideoConfigurationStore struct {
	config    provider.VideoConfiguration
	saved     provider.VideoConfigurationInput
	storedKey string
}

func (s *stubVideoConfigurationStore) GetVideoConfiguration(context.Context, contract.OrganizationID) (provider.VideoConfiguration, error) {
	return s.config, nil
}

func (s *stubVideoConfigurationStore) SaveVideoConfiguration(_ context.Context, _ contract.OrganizationID, input provider.VideoConfigurationInput) (provider.VideoConfiguration, error) {
	s.saved = input
	s.config = provider.VideoConfiguration{
		Configured: true, BaseURL: input.BaseURL, UpstreamModel: input.Model,
		MaskedAPIKey: provider.MaskAPIKey(input.APIKey), CredentialReadable: true,
	}
	return s.config, nil
}

func (s *stubVideoConfigurationStore) ResolveVideoAPIKey(context.Context, contract.OrganizationID) (string, error) {
	if s.storedKey == "" {
		return "", provider.ErrVideoConfigurationCredentialMissing
	}
	return s.storedKey, nil
}

func TestVideoConfigurationEndpointsNeverEchoTheAPIKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()

	store := &stubVideoConfigurationStore{}
	server := newTestServer(t, func(dependencies *Dependencies) {
		dependencies.ProviderVideoConfiguration = store
		dependencies.ProviderAllowInsecureHTTP = true
	})

	body := `{"base_url":"` + upstream.URL + `","model":"doubao-seedance-1-0-lite-t2v-250428","api_key":"super-secret-value"}`
	response := doAuthenticatedRequest(t, server, http.MethodPut, "/platform/v1/provider/video-configuration", body)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "super-secret-value") {
		t.Fatal("the response must never contain the plaintext API key")
	}
	if !strings.Contains(response.Body.String(), `"masked_api_key":"****alue"`) {
		t.Fatalf("expected a masked key, got %s", response.Body.String())
	}
	if store.saved.APIKey != "super-secret-value" {
		t.Fatalf("the store did not receive the key: %+v", store.saved)
	}
}

func TestVideoConfigurationSaveIsRejectedWhenTheProbeFails(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()

	store := &stubVideoConfigurationStore{}
	server := newTestServer(t, func(dependencies *Dependencies) {
		dependencies.ProviderVideoConfiguration = store
		dependencies.ProviderAllowInsecureHTTP = true
	})

	body := `{"base_url":"` + upstream.URL + `","model":"m","api_key":"bad-key"}`
	response := doAuthenticatedRequest(t, server, http.MethodPut, "/platform/v1/provider/video-configuration", body)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.saved.BaseURL != "" {
		t.Fatal("a failed probe must not write to the database")
	}
}

func TestVideoConfigurationVerifyReusesTheStoredKey(t *testing.T) {
	var gotAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer upstream.Close()

	store := &stubVideoConfigurationStore{storedKey: "stored-key"}
	server := newTestServer(t, func(dependencies *Dependencies) {
		dependencies.ProviderVideoConfiguration = store
		dependencies.ProviderAllowInsecureHTTP = true
	})

	body := `{"base_url":"` + upstream.URL + `","model":"m"}`
	response := doAuthenticatedRequest(t, server, http.MethodPost, "/platform/v1/provider/video-configuration:verify", body)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if gotAuthorization != "Bearer stored-key" {
		t.Fatalf("authorization = %q", gotAuthorization)
	}
}
```

> 实施说明：`newTestServer` 与 `doAuthenticatedRequest` 按本文件既有辅助的实际签名调整；不要新造测试脚手架。若既有辅助不支持回调式改依赖，就照相邻用例的写法直接构造 `Dependencies`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/platform/httpserver/ -run TestVideoConfiguration -v`
Expected: `dependencies.ProviderVideoConfiguration undefined`。

- [ ] **Step 3: 定义依赖接口并注册路由**

`internal/platform/httpserver/server.go:319` 的 `ProviderConfigurationReader` 保持不动（只读能力清单仍在用），另加：

```go
// ProviderVideoConfigurationStore is the read-write seam the Settings page uses
// to configure video generation. Plaintext credentials enter through Save and
// never come back out.
type ProviderVideoConfigurationStore interface {
	GetVideoConfiguration(ctx context.Context, organizationID contract.OrganizationID) (provider.VideoConfiguration, error)
	SaveVideoConfiguration(ctx context.Context, organizationID contract.OrganizationID, input provider.VideoConfigurationInput) (provider.VideoConfiguration, error)
	ResolveVideoAPIKey(ctx context.Context, organizationID contract.OrganizationID) (string, error)
}
```

在 `Dependencies` 结构体里加两个字段：

```go
	ProviderVideoConfiguration ProviderVideoConfigurationStore
	ProviderAllowInsecureHTTP  bool
	// ProviderVideoEnvironmentConfigured reports whether COOKIES_ARK_VIDEO_API_KEY
	// is set, so the page can say which configuration is actually in effect.
	ProviderVideoEnvironmentConfigured bool
	ProviderVideoEnvironmentModel      string
	ProviderVideoEnvironmentBaseURL    string
```

在 `server.go:359` 的 `provider/capabilities` 注册之后追加：

```go
	server.mux.Handle("GET /platform/v1/provider/video-configuration", server.requireAuthentication(http.HandlerFunc(server.readVideoConfiguration)))
	server.mux.Handle("PUT /platform/v1/provider/video-configuration", server.requireAuthentication(server.requireScope("provider.configuration.write", http.HandlerFunc(server.saveVideoConfiguration))))
	server.mux.Handle("POST /platform/v1/provider/video-configuration:verify", server.requireAuthentication(server.requireScope("provider.configuration.write", http.HandlerFunc(server.verifyVideoConfiguration))))
```

- [ ] **Step 4: 写 handler**

在 `internal/platform/httpserver/handlers.go` 的 `providerCapabilities` 之后追加：

```go
type videoConfigurationRequest struct {
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	APIKey          string `json:"api_key"`
	ExpectedVersion *int64 `json:"expected_version"`
}

type videoConfigurationVerification struct {
	OK      bool   `json:"ok"`
	Outcome string `json:"outcome"`
	Message string `json:"message"`
}

type videoConfigurationResponse struct {
	Capability       string                          `json:"capability"`
	Status           string                          `json:"status"`
	Source           string                          `json:"source"`
	BaseURL          string                          `json:"base_url"`
	Model            string                          `json:"model"`
	MaskedAPIKey     string                          `json:"masked_api_key"`
	NeedsReentry     bool                            `json:"needs_reentry"`
	Version          int64                           `json:"version"`
	UpdatedAt        *time.Time                      `json:"updated_at,omitempty"`
	LastVerifiedAt   *time.Time                      `json:"last_verified_at,omitempty"`
	LastVerification *videoConfigurationVerification `json:"last_verification,omitempty"`
}

func (s *Server) videoConfigurationView(config provider.VideoConfiguration) videoConfigurationResponse {
	view := videoConfigurationResponse{
		Capability: "video.generate",
		Status:     "not_configured",
		Source:     "none",
		Version:    config.Version,
	}
	switch {
	case config.Configured:
		view.Status = "configured"
		view.Source = "workspace"
		view.BaseURL = config.BaseURL
		view.Model = config.UpstreamModel
		view.MaskedAPIKey = config.MaskedAPIKey
		view.NeedsReentry = !config.CredentialReadable
		if view.NeedsReentry {
			// A stored credential that no longer decrypts is a configuration
			// problem, not an outage. Ask for a re-entry instead of a retry.
			view.Status = "needs_reentry"
		}
		updatedAt := config.UpdatedAt
		view.UpdatedAt = &updatedAt
		view.LastVerifiedAt = config.LastVerifiedAt
		if config.LastVerificationOK != nil {
			view.LastVerification = &videoConfigurationVerification{OK: *config.LastVerificationOK, Message: config.LastVerificationMessage}
		}
	case s.dependencies.ProviderVideoEnvironmentConfigured:
		view.Status = "configured"
		view.Source = "environment"
		view.BaseURL = s.dependencies.ProviderVideoEnvironmentBaseURL
		view.Model = s.dependencies.ProviderVideoEnvironmentModel
	}
	return view
}

func (s *Server) readVideoConfiguration(w http.ResponseWriter, r *http.Request) {
	identity, ok := identityFromContext(r.Context())
	if !ok {
		writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "认证信息缺失")
		return
	}
	if s.dependencies.ProviderVideoConfiguration == nil {
		writeJSON(w, http.StatusOK, s.videoConfigurationView(provider.VideoConfiguration{}))
		return
	}
	config, err := s.dependencies.ProviderVideoConfiguration.GetVideoConfiguration(r.Context(), identity.OrganizationID)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "provider_configuration_unavailable", "读取模型配置失败")
		return
	}
	writeJSON(w, http.StatusOK, s.videoConfigurationView(config))
}

// probeVideoConfiguration resolves the credential to test (the submitted one,
// or the stored one when the field was left blank) and checks connectivity.
func (s *Server) probeVideoConfiguration(r *http.Request, organizationID contract.OrganizationID, request videoConfigurationRequest) (provider.VideoProbeResult, string, error) {
	apiKey := strings.TrimSpace(request.APIKey)
	if apiKey == "" && s.dependencies.ProviderVideoConfiguration != nil {
		stored, err := s.dependencies.ProviderVideoConfiguration.ResolveVideoAPIKey(r.Context(), organizationID)
		if err != nil {
			return provider.VideoProbeResult{}, "", err
		}
		apiKey = stored
	}
	if apiKey == "" {
		return provider.VideoProbeResult{}, "", provider.ErrVideoConfigurationCredentialMissing
	}
	return provider.ProbeArkVideoCredential(r.Context(), nil, request.BaseURL, apiKey), apiKey, nil
}

func (s *Server) verifyVideoConfiguration(w http.ResponseWriter, r *http.Request) {
	identity, ok := identityFromContext(r.Context())
	if !ok {
		writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "认证信息缺失")
		return
	}
	var request videoConfigurationRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		return
	}
	result, _, err := s.probeVideoConfiguration(r, identity.OrganizationID, request)
	if errors.Is(err, provider.ErrVideoConfigurationCredentialMissing) {
		writeProblem(w, r, http.StatusBadRequest, "provider_credential_required", "还没有保存过密钥，请填写密钥后再测试")
		return
	}
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "provider_configuration_unavailable", "读取模型配置失败")
		return
	}
	writeJSON(w, http.StatusOK, videoConfigurationVerification{OK: result.OK(), Outcome: string(result.Outcome), Message: result.Message})
}

func (s *Server) saveVideoConfiguration(w http.ResponseWriter, r *http.Request) {
	identity, ok := identityFromContext(r.Context())
	if !ok {
		writeProblem(w, r, http.StatusUnauthorized, "unauthenticated", "认证信息缺失")
		return
	}
	if s.dependencies.ProviderVideoConfiguration == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "provider_configuration_unavailable", "当前部署没有开启模型配置写入")
		return
	}
	var request videoConfigurationRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		return
	}
	result, apiKey, err := s.probeVideoConfiguration(r, identity.OrganizationID, request)
	if errors.Is(err, provider.ErrVideoConfigurationCredentialMissing) {
		writeProblem(w, r, http.StatusBadRequest, "provider_credential_required", "第一次保存必须填写密钥")
		return
	}
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "provider_configuration_unavailable", "读取模型配置失败")
		return
	}
	if !result.OK() {
		// Nothing is written when the probe fails, so a bad paste cannot break a
		// working configuration.
		writeProblem(w, r, http.StatusBadGateway, "provider_verification_failed", result.Message)
		return
	}
	saved, err := s.dependencies.ProviderVideoConfiguration.SaveVideoConfiguration(r.Context(), identity.OrganizationID, provider.VideoConfigurationInput{
		BaseURL:         request.BaseURL,
		Model:           request.Model,
		APIKey:          apiKey,
		Verification:    result,
		ExpectedVersion: request.ExpectedVersion,
	})
	if errors.Is(err, provider.ErrVideoConfigurationConflict) {
		writeProblem(w, r, http.StatusConflict, "provider_configuration_conflict", "配置已被其他人修改，请刷新后重试")
		return
	}
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "provider_configuration_invalid", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.videoConfigurationView(saved))
}
```

> 实施说明：`writeProblem` / `writeJSON` / `decodeJSONBody` / `identityFromContext` 的确切名字与签名以 `handlers.go` 里相邻 handler 为准，照抄现有用法。`s.dependencies` 的字段访问方式同理。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/platform/httpserver/ -run TestVideoConfiguration -v`
Expected: 三条全 PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/platform/httpserver/server.go internal/platform/httpserver/handlers.go internal/platform/httpserver/handlers_test.go
git commit -m "feat(api): expose video model configuration read, save, and verify"
```

---

### Task 7: 装配

把 Task 4~6 接到 `main.go`：视频适配器同时拿库内解析器和 env 兜底，HTTP 依赖注入写入 store。

**Files:**
- Modify: `cmd/cookies-api/main.go:752-767`（VideoRoutes 装配）、`:888-905`（buildVideoAdapter）、依赖注入处

**Interfaces:**
- Consumes: Task 5 的 `NewArkVideoAdapterWithRoutes` 与 `VideoRouteOptional`；Task 6 的 `Dependencies` 新字段。

- [ ] **Step 1: 改 `buildVideoAdapter` 的 `ark_video` 分支**

```go
	case "ark_video":
		// The Settings page can configure this at runtime, so the adapter always
		// carries a route resolver. Environment credentials remain as a fallback
		// for deployments that never open the page.
		cipher, err := provider.NewAESGCMCredentialCipher(cfg.Provider.MasterKey, cfg.Provider.MasterKeyVersion)
		if err != nil {
			return nil, err
		}
		store := provider.MySQLGatewayConfigStore{DB: db, Cipher: cipher, AllowInsecureHTTP: cfg.Provider.AllowInsecureHTTP, VideoConnectionType: "ark"}
		return provider.NewArkVideoAdapterWithRoutes(provider.ArkVideoConfig{
			APIKey:  cfg.Provider.ArkVideo.APIKey,
			Model:   cfg.Provider.ArkVideo.Model,
			BaseURL: cfg.Provider.ArkVideo.BaseURL,
		}, store, handles)
```

- [ ] **Step 2: 改 `main.go:752-767` 的 VideoRoutes 装配**

```go
		// ark_video always resolves through MySQL so that configuration saved in
		// the Settings page takes effect without a restart. VideoRouteOptional
		// keeps the environment credential usable until that first save.
		if cfg.Provider.VideoAdapter == "adapter_gateway" || cfg.Provider.VideoAdapter == "ark_video" {
			cipher, cipherErr := provider.NewAESGCMCredentialCipher(cfg.Provider.MasterKey, cfg.Provider.MasterKeyVersion)
			if cipherErr != nil {
				log.Fatalf("configure Provider video credential encryption: %v", cipherErr)
			}
			connectionType := "ark"
			if cfg.Provider.VideoAdapter == "adapter_gateway" {
				connectionType = "adapter_gateway"
			}
			videoConfigStore := provider.MySQLGatewayConfigStore{
				DB: db, Cipher: cipher, AllowInsecureHTTP: cfg.Provider.AllowInsecureHTTP,
				VideoConnectionType: connectionType,
			}
			providerService.VideoRoutes = videoConfigStore
			providerService.VideoRouteOptional = cfg.Provider.ArkVideoDirect()
			if cfg.Provider.VideoAdapter == "ark_video" {
				dependencies.ProviderVideoConfiguration = videoConfigStore
			}
		}
		dependencies.ProviderAllowInsecureHTTP = cfg.Provider.AllowInsecureHTTP
		dependencies.ProviderVideoEnvironmentConfigured = cfg.Provider.ArkVideoDirect()
		dependencies.ProviderVideoEnvironmentModel = cfg.Provider.ArkVideo.Model
		dependencies.ProviderVideoEnvironmentBaseURL = cfg.Provider.ArkVideo.BaseURL
```

> 实施说明：`dependencies` 变量在这一段是否已在作用域内需现场确认；若这段在 `dependencies` 构造之前，就把四行赋值挪到构造之后、并把 `videoConfigStore` 提升到外层变量。

- [ ] **Step 3: 编译并起服务**

Run: `go build ./... && go vet ./...`
Expected: 无报错。

Run: `go run ./cmd/cookies-api`
Expected: 启动成功，无 `log.Fatalf`。

- [ ] **Step 4: 手工验证三个接口**

先登录拿 cookie：

```bash
curl -s -c .tmp-cookies.txt -X POST http://127.0.0.1:8080/platform/v1/auth/login -H 'Content-Type: application/json' -d '{"username":"Admin","password":"123456"}' -o /dev/null -w '%{http_code}\n'
```

读配置（应为 `not_configured` 或 `environment`）：

```bash
curl -s -b .tmp-cookies.txt http://127.0.0.1:8080/platform/v1/provider/video-configuration
```

Expected: 200，JSON 含 `"capability":"video.generate"`。

- [ ] **Step 5: Commit**

```bash
git add cmd/cookies-api/main.go
git commit -m "feat(api): wire the video configuration store into the HTTP surface"
```

---

### Task 8: 前端设置页

现在 `src/data/api.ts:5780-5784` 的三个方法打向 `/api/provider/configuration`，后端从未实现，必然 404。改成打真接口。注意 `/platform/v1` 路径要用 `apiRequest`（`src/backend/platform.ts:211`），不是 `request`（会加 `/api` 前缀）。

**Files:**
- Modify: `src/data/api.ts:5780-5784`
- Modify: `src/context/ModelConfigContext.tsx`
- Modify: `src/components/ModelSettingsPage.tsx`

**Interfaces:**
- Consumes: Task 6 的三条路由与响应字段（`status` / `source` / `base_url` / `model` / `masked_api_key` / `needs_reentry` / `version` / `last_verified_at` / `last_verification`）。

- [ ] **Step 1: 换掉 api.ts 里的三个方法**

删除指向 `/provider/configuration` 的三个方法，替换为：

```ts
export interface VideoModelConfiguration {
  capability: string;
  status: 'configured' | 'not_configured' | 'needs_reentry';
  source: 'workspace' | 'environment' | 'none';
  base_url: string;
  model: string;
  masked_api_key: string;
  needs_reentry: boolean;
  version: number;
  updated_at?: string;
  last_verified_at?: string;
  last_verification?: { ok: boolean; outcome?: string; message: string };
}

export interface VideoModelConfigurationInput {
  base_url: string;
  model: string;
  api_key?: string;
  expected_version?: number;
}

export async function getVideoModelConfiguration(): Promise<VideoModelConfiguration> {
  return apiRequest<VideoModelConfiguration>('/platform/v1/provider/video-configuration');
}

export async function saveVideoModelConfiguration(input: VideoModelConfigurationInput): Promise<VideoModelConfiguration> {
  return apiRequest<VideoModelConfiguration>('/platform/v1/provider/video-configuration', {
    method: 'PUT',
    body: JSON.stringify(input),
  });
}

export async function verifyVideoModelConfiguration(
  input: VideoModelConfigurationInput,
): Promise<{ ok: boolean; outcome: string; message: string }> {
  return apiRequest<{ ok: boolean; outcome: string; message: string }>(
    '/platform/v1/provider/video-configuration:verify',
    { method: 'POST', body: JSON.stringify(input) },
  );
}
```

若 `api.ts` 尚未 import `apiRequest`，从 `../backend/platform` 引入（路径以文件内既有 import 为准）。

- [ ] **Step 2: 改 `ModelConfigContext.tsx`**

把 `saveProvider` 改为调 `saveVideoModelConfiguration`，`refresh` 改为同时读 `getVideoModelConfiguration` 与既有的 `getCapabilities`，并新增 `verifyProvider` 走 `verifyVideoModelConfiguration`。context 值里补 `videoConfiguration: VideoModelConfiguration | null`。

> 实施说明：保留现有 context 对外暴露的其他字段不动，只加不改，避免其他页面报错。

- [ ] **Step 3: 改 `ModelSettingsPage.tsx`**

把现有「模型服务密钥 / 服务地址」表单改成设计文档第三节的视频生成卡片：

- 三个输入：服务地址（默认填 `https://ark.cn-beijing.volces.com/api/v3`）、密钥（`type="password"`，已配置时 placeholder 为 `已保存，留空则不改动`，且**不是必填**）、模型（`<input list=...>` 可输入下拉，预置 `doubao-seedance-1-0-lite-t2v-250428`、`doubao-seedance-1-0-pro-250528`）。
- 头部状态徽标：`source === 'workspace'` 显示「已配置 · 服务端加密保存」；`'environment'` 显示「使用部署环境配置」；`'none'` 显示「未配置」；`status === 'needs_reentry'` 显示「配置需要重新填写」并高亮密钥框。
- 底部两个按钮：「测试连接」调 `verifyProvider`，把返回的 `message` 显示出来但不写库；「保存并生效」调 `saveProvider`，失败时直接展示后端返回的 `detail`。
- 「最近校验」行显示 `last_verified_at` 与 `last_verification.message`。
- 表单下方固定一行灰字提示：`测试连接只验证地址和密钥；模型名是否可用要到第一次生成视频时才知道。`

- [ ] **Step 4: 前端构建**

Run: `npm run build`
Expected: 构建通过，无 TypeScript 报错。

- [ ] **Step 5: 界面验证**

启动 `npm run dev` 与 `go run ./cmd/cookies-api`，打开系统设置页：

1. 首次进入显示「未配置」或「使用部署环境配置」。
2. 填一个错的密钥点「测试连接」→ 显示「密钥被拒绝…」。
3. 填正确的地址 + 密钥 + 模型点「保存并生效」→ 状态变为「已配置 · 服务端加密保存」，最近校验显示当前时间。
4. 刷新页面 → 配置仍在，密钥显示为掩码。
5. 只改模型、密钥框留空，再点保存 → 成功，说明旧密钥被沿用。

- [ ] **Step 6: Commit**

```bash
git add src/data/api.ts src/context/ModelConfigContext.tsx src/components/ModelSettingsPage.tsx
git commit -m "feat(web): configure video generation from the Settings page"
```

---

### Task 9: 文档与端到端验证

**Files:**
- Modify: `docs/25-local-demo-runbook.md:117-141`（5.2 节）

- [ ] **Step 1: 改 runbook 5.2 节**

把「例外：视频（Ark Seedance）可以不走脚本」整段替换为：

```markdown
**视频（Ark Seedance）不需要脚本，也不需要改 `.env`。** 启动后端后打开前端的「系统设置」页，在「视频生成」卡片里填服务地址、API Key、模型名，点「保存并生效」即可。保存前系统会用一个不存在的任务 ID 探一次上游：密钥无效当场报错、不写库；通过才加密写入 MySQL，无需重启。

`.env` 里的 `COOKIES_ARK_VIDEO_*` 仍然有效，作为「界面还没配过」时的兜底。优先级是**界面配置 > .env > 未配置**。

`scripts/configure-ark-video.ps1` 仍可用，它写的是同一条连接和路由，两条路可以互相覆盖。

注意：连通性探测验不出模型名对不对——验模型名必须真提交一次生成任务，那要花钱。模型名填错会在第一次生成视频时以上游原话报出来。
```

- [ ] **Step 2: 跑全量验证**

Run:
```bash
go build ./... && go vet ./... && go test ./... && npm run build && git diff --check
```
Expected: 全部通过。

- [ ] **Step 3: 端到端实跑一次视频生成**

在设置页保存好真实凭据后，从创意页提交一次视频生成，确认任务提交成功并进入轮询（不必等出片）。若报 `MODEL_INPUT_UNSUPPORTED`，检查请求里是否带了该模型不支持的字段（`seedance-1-0-lite` 不接受显式 `audio_policy`），这是模型侧限制而非本次改动的问题。

- [ ] **Step 4: Commit**

```bash
git add docs/25-local-demo-runbook.md
git commit -m "docs: configure video generation from the Settings page"
```

---

## 自查

**Spec 覆盖：** 第一节问题 → Task 8 修好坏表单；第二节范围 → 全程只做 `video.generate`；第三节界面 → Task 8 Step 3；第四节探测 → Task 2 + Task 6；第五节数据落点 → Task 3 + Task 4；第六节 env 关系 → Task 5 + Task 7；第七节主密钥 → Task 1；第八节接口 → Task 6；第九节分层 → 各 Task 的 Files 段；第十节测试 → Task 2/4/6 的测试步骤（写入、探测五种响应、密钥不外泄、回落、解密失败经 `CredentialReadable`→`needs_reentry`、界面行为在 Task 8 Step 5）；第十一节不做 → 计划里未出现多组织隔离、参数白名单界面、轮换排期、模型名校验。

**已知需现场确认的三处**（计划里已就地标注，不是占位符）：`video_test.go` 的既有 helper 名、`handlers.go` 的 `writeProblem`/`writeJSON`/`decodeJSONBody` 实际签名、`main.go` 里 `dependencies` 变量的作用域位置。这三处都要照相邻既有代码的写法对齐，不新造脚手架。
