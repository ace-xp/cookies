# 素材缩略图 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让素材台账和素材清单上的每一条视频都有一张能看的首帧图，而不是一行标题加一个类型图标。

**Architecture:** 平台早就把派生物的脚手架搭好了 —— `DerivativeProfile` 里有 `poster_v1`，`asset_derivatives` / `asset_processing_jobs` 两张表建了，`EnsureDerivative` / `RetryDerivative` 也实现了。缺的是三样：一个真的会抽帧的实现、一个把任务跑起来的 worker、一个把结果换成可访问 URL 的端点。本计划补这三样，一行新表都不建。抽帧走 ffmpeg（镜像里已经装了），worker 挂在现成的 `jobruntime` 共享 runtime 上，取用端点由洞察侧通过 `internal/integrations/` 的适配器读到派生物。

**Tech Stack:** Go 1.22+（`internal/platform/assets`、`internal/systems/insights`、`internal/integrations`）、ffmpeg、MySQL 8、Vite + React 19 + TypeScript 5.9（`src/`）

**执行顺序：** 本计划在 `docs/superpowers/plans/2026-08-13-insight-asset-management.md`（素材台账基建）之后执行。Task 5 的触发点复用那份计划 Task 8 建立的挂钩位置；Task 6 的前端落点是那份计划 Task 12 建的台账视图。

## Global Constraints

- 所有新增注释、错误文案、界面文案一律中文。
- 分层：`internal/platform/*` **不得** import `internal/systems/*`。
- 后端全量验证：`go build ./...` 与 `go test ./internal/platform/assets/... ./internal/systems/insights/...`。
- 前端全量验证：`npx tsc --noEmit -p tsconfig.json` 与 `npm test`。
- ffmpeg 路径来自现有配置（`cfg.Media.FFmpegPath` 或 `main.go` 里已解析出来的那个变量，见 Task 5 Step 3）。**路径为空时整条链路静默不启用** —— 本地开发机没有 ffmpeg 也要能跑起来。
- 抽帧失败不影响素材本身。派生物有自己的 `failed` 状态和重试通道，素材照常可用。

## 文件结构

| 文件 | 责任 |
|---|---|
| `internal/platform/assets/poster.go`（新建） | `PosterExtractor` 接口 + `FFmpegPosterExtractor` 抽帧实现 |
| `internal/platform/assets/derivatives.go` | 仓储接口加 `GetDerivative` / `CompleteDerivative`；新增 kind 常量、调度器、handler |
| `internal/platform/assets/derivatives_mysql.go` | 上面两个方法的 SQL 实现 |
| `cmd/cookies-api/main.go` | 装抽帧器、调度器、handler；把 poster 端口接给洞察 |
| `internal/systems/insights/assets.go` | `PosterReader` 端口 + `ReadAssetPoster` 服务方法 |
| `internal/integrations/insightsposter/reader.go`（新建） | 把 `assets.DerivativeService` 的查询翻成洞察要的签名 URL |
| `internal/systems/insights/httpapi/assets.go` | `GET .../assets/{asset_id}/poster` |
| `src/data/api.ts` | `insightAssetPosterUrl` |
| `src/components/insight/assets/LedgerView.tsx` | 每条记录左侧显示缩略图，取不到就退回类型图标 |

---

### Task 1: 抽帧

**Files:**
- Create: `internal/platform/assets/poster.go`
- Test: `internal/platform/assets/poster_test.go`

**Interfaces:**
- Produces:
  - `type PosterExtractor interface { ExtractPoster(ctx context.Context, contents []byte) ([]byte, error) }`
  - `type FFmpegPosterExtractor struct { Path string; WorkRoot string; SeekSeconds float64 }`
  - `func (p FFmpegPosterExtractor) ExtractPoster(ctx context.Context, contents []byte) ([]byte, error)`
  - `const PosterMIMEType = "image/jpeg"`

- [ ] **Step 1: 写失败的测试**

创建 `internal/platform/assets/poster_test.go`：

```go
package assets

import (
	"context"
	"errors"
	"testing"
)

func TestFFmpegPosterExtractorRequiresPathAndContent(t *testing.T) {
	// 没配 ffmpeg 路径时要明确报错，不能返回空字节当成「抽出来了一张空图」——
	// 那会让一张 0 字节的图落进素材库，前端显示成裂图，而没人知道为什么。
	extractor := FFmpegPosterExtractor{}
	if _, err := extractor.ExtractPoster(context.Background(), []byte("fake")); err == nil {
		t.Fatal("没有 ffmpeg 路径时应当报错")
	}
	withPath := FFmpegPosterExtractor{Path: "ffmpeg"}
	if _, err := withPath.ExtractPoster(context.Background(), nil); err == nil {
		t.Fatal("没有视频内容时应当报错")
	}
}

func TestFFmpegPosterExtractorReportsMissingBinary(t *testing.T) {
	extractor := FFmpegPosterExtractor{Path: "definitely-not-a-real-ffmpeg-binary"}
	_, err := extractor.ExtractPoster(context.Background(), []byte("fake video bytes"))
	if err == nil {
		t.Fatal("ffmpeg 不存在时应当报错")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("错误应当说清是 ffmpeg 的问题，得到 %v", err)
	}
}

func TestPosterMIMETypeIsAllowedForDerivedImages(t *testing.T) {
	// IngestDerivedImage 会用 allowedDeclaredImageMIME 挡住不认识的类型。
	// 抽出来的图声明成一个它不认的 MIME，落库那一步会失败，而失败发生在
	// worker 里、只留一行日志——所以在这里就把它钉死。
	if !allowedDeclaredImageMIME(PosterMIMEType) {
		t.Fatalf("%q 应当是允许的派生图类型", PosterMIMEType)
	}
}
```

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/platform/assets/ -run TestFFmpegPoster
```

预期：编译失败，`undefined: FFmpegPosterExtractor`。

- [ ] **Step 3: 实现抽帧**

创建 `internal/platform/assets/poster.go`：

```go
package assets

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PosterMIMEType 是抽出来的首帧图的类型。JPEG 不是随便选的：
// 缩略图要的是小和快，不是无损。
const PosterMIMEType = "image/jpeg"

// PosterExtractor 从一段视频里抽一帧当封面。
//
// 和 VideoMetadataProbe 一样进字节出字节，不向调用方暴露临时文件路径——
// 调用方拿到的是一张图，不是一个要记得删的文件。
type PosterExtractor interface {
	ExtractPoster(ctx context.Context, contents []byte) ([]byte, error)
}

// FFmpegPosterExtractor 用 ffmpeg 抽帧。形状照着 FFprobeVideoProbe 来，
// 包括 WorkRoot 的默认值约定和临时文件一定删掉这条。
type FFmpegPosterExtractor struct {
	Path     string
	WorkRoot string
	// SeekSeconds 是抽第几秒。默认 1 秒而不是 0：很多片子第一帧是纯黑的
	// 开场，抽出来的封面等于没有。
	SeekSeconds float64
}

func (p FFmpegPosterExtractor) ExtractPoster(ctx context.Context, contents []byte) ([]byte, error) {
	if strings.TrimSpace(p.Path) == "" {
		return nil, fmt.Errorf("ffmpeg path is required for poster extraction")
	}
	if len(contents) == 0 {
		return nil, fmt.Errorf("video contents are required for poster extraction")
	}
	workRoot := strings.TrimSpace(p.WorkRoot)
	if workRoot == "" {
		workRoot = filepath.Join(".data", "poster-work")
	}
	if err := os.MkdirAll(workRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create poster work directory: %w", err)
	}
	input, err := os.CreateTemp(workRoot, "poster-in-*.mp4")
	if err != nil {
		return nil, fmt.Errorf("create poster input: %w", err)
	}
	inputName := input.Name()
	defer os.Remove(inputName)
	if err := input.Chmod(0o600); err != nil {
		input.Close()
		return nil, err
	}
	if _, err := input.Write(contents); err != nil {
		input.Close()
		return nil, err
	}
	if err := input.Close(); err != nil {
		return nil, err
	}

	outputName := inputName + ".jpg"
	defer os.Remove(outputName)

	seek := p.SeekSeconds
	if seek <= 0 {
		seek = 1
	}
	// -ss 放在 -i 前面是快速定位（关键帧对齐，够用且快得多）。
	// -frames:v 1 只要一帧；-vf scale 把长边压到 640，缩略图不需要更大。
	command := exec.CommandContext(ctx, p.Path,
		"-hide_banner", "-loglevel", "error", "-y",
		"-ss", fmt.Sprintf("%.3f", seek), "-i", inputName,
		"-frames:v", "1", "-vf", "scale='min(640,iw)':-2",
		"-q:v", "4", outputName)
	if output, err := command.CombinedOutput(); err != nil {
		// 视频短于 SeekSeconds 时上面那次会抽不到帧，退回第 0 秒再试一次。
		// 六秒的广告前贴在库里不算少见。
		retry := exec.CommandContext(ctx, p.Path,
			"-hide_banner", "-loglevel", "error", "-y",
			"-i", inputName, "-frames:v", "1", "-vf", "scale='min(640,iw)':-2",
			"-q:v", "4", outputName)
		if retryOutput, retryErr := retry.CombinedOutput(); retryErr != nil {
			return nil, fmt.Errorf("ffmpeg 抽帧失败: %w: %s / %s", err,
				strings.TrimSpace(string(output)), strings.TrimSpace(string(retryOutput)))
		}
	}
	poster, err := os.ReadFile(outputName)
	if err != nil {
		return nil, fmt.Errorf("read poster output: %w", err)
	}
	if len(poster) == 0 {
		return nil, fmt.Errorf("ffmpeg 抽出来的封面是空的")
	}
	return poster, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/platform/assets/ -run 'TestFFmpegPoster|TestPosterMIMEType'
```

预期：`ok`，3 个测试全过。若 `TestPosterMIMETypeIsAllowedForDerivedImages` 失败，说明 `allowedDeclaredImageMIME` 不收 `image/jpeg`；先跑

```bash
cd /d/project/cookies-integration-mvp && grep -n "func allowedDeclaredImageMIME" -A 10 internal/platform/assets/upload_service.go
```

看它收哪些，把 `PosterMIMEType` 改成它收的那个（很可能是 `image/jpeg` 就在列），并把 ffmpeg 的输出后缀跟着改。

- [ ] **Step 5: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/platform/assets/poster.go internal/platform/assets/poster_test.go && git commit -m "feat(assets): ffmpeg 抽首帧当素材封面"
```

---

### Task 2: 派生物查得到、结得掉

**Files:**
- Modify: `internal/platform/assets/derivatives.go`（`DerivativeRepository` 接口）
- Modify: `internal/platform/assets/derivatives_mysql.go`
- Test: `internal/platform/assets/derivatives_test.go`

**Interfaces:**
- Produces:
  - `DerivativeRepository.GetDerivative(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, source contract.AssetVersionRef, profile DerivativeProfile) (AssetDerivative, error)`
  - `DerivativeRepository.GetDerivativeByID(ctx context.Context, id string) (AssetDerivative, error)`
  - `DerivativeRepository.CompleteDerivative(ctx context.Context, id string, output contract.AssetVersionRef, now time.Time) (AssetDerivative, error)`
  - `func (s DerivativeService) FindDerivative(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, source contract.AssetVersionRef, profile DerivativeProfile) (AssetDerivative, error)`

> 现有接口只有 `EnsureDerivative` / `FailDerivativeScheduling` / `RetryDerivative` —— 能建、能失败、能重试，唯独**不能完成、不能查**。这是这套脚手架从没跑起来过的直接原因。

- [ ] **Step 1: 写失败的测试**

在 `internal/platform/assets/derivatives_test.go` 末尾追加：

```go
func TestFindDerivativeRequiresConfiguredRepository(t *testing.T) {
	service := DerivativeService{}
	_, err := service.FindDerivative(context.Background(), "k_org_1", "k_project_1",
		contract.AssetVersionRef{AssetID: "asset_1", Version: 1}, DerivativePoster)
	if err == nil {
		t.Fatal("没有仓储时应当报错")
	}
}

func TestFindDerivativeRejectsUnknownProfile(t *testing.T) {
	service := DerivativeService{Repository: &memoryDerivativeRepository{}}
	_, err := service.FindDerivative(context.Background(), "k_org_1", "k_project_1",
		contract.AssetVersionRef{AssetID: "asset_1", Version: 1}, DerivativeProfile("thumbnail_v9"))
	if err == nil {
		t.Fatal("不认识的派生物规格应当被拒")
	}
}
```

> `memoryDerivativeRepository` 是 `derivatives_test.go` 里已有的测试替身。若它叫别的名字，用现有的那个；给它补上三个新方法（`GetDerivative` 返回 `ErrNotFound` 即可，`GetDerivativeByID` 同理，`CompleteDerivative` 原样返回入参构成的记录）。

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/platform/assets/ -run TestFindDerivative
```

预期：编译失败，`service.FindDerivative undefined`。

- [ ] **Step 3: 扩接口与服务方法**

在 `internal/platform/assets/derivatives.go` 的 `DerivativeRepository` 接口里追加三行：

```go
	GetDerivative(context.Context, contract.OrganizationID, contract.ProjectID, contract.AssetVersionRef, DerivativeProfile) (AssetDerivative, error)
	GetDerivativeByID(context.Context, string) (AssetDerivative, error)
	CompleteDerivative(context.Context, string, contract.AssetVersionRef, time.Time) (AssetDerivative, error)
```

在 `RetryDerivative` 之后加：

```go
// FindDerivative 查一个素材版本的某种派生物现在什么状态、产物在哪。
//
// 派生物这套东西建好之后一直没有查询口——只能建不能查，所以谁也用不上它。
func (s DerivativeService) FindDerivative(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, source contract.AssetVersionRef, profile DerivativeProfile) (AssetDerivative, error) {
	if s.Repository == nil {
		return AssetDerivative{}, fmt.Errorf("derivative repository is required")
	}
	if !validDerivativeProfile(profile) {
		return AssetDerivative{}, fmt.Errorf("unsupported derivative profile")
	}
	if err := source.Validate(); err != nil {
		return AssetDerivative{}, err
	}
	return s.Repository.GetDerivative(ctx, organizationID, projectID, source, profile)
}
```

- [ ] **Step 4: 实现 SQL**

在 `internal/platform/assets/derivatives_mysql.go` 末尾加：

```go
const assetDerivativeSelect = `SELECT id, organization_id, project_id, source_asset_id, source_asset_version, profile, status, output_asset_id, output_asset_version, COALESCE(error_code, ''), created_at, updated_at FROM asset_derivatives`

func scanDerivative(row interface{ Scan(...any) error }) (AssetDerivative, error) {
	var value AssetDerivative
	var outputAssetID sql.NullString
	var outputVersion sql.NullInt64
	if err := row.Scan(&value.ID, &value.OrganizationID, &value.ProjectID,
		&value.Source.AssetID, &value.Source.Version, &value.Profile, &value.Status,
		&outputAssetID, &outputVersion, &value.ErrorCode,
		&value.CreatedAt, &value.UpdatedAt); err != nil {
		return AssetDerivative{}, err
	}
	if outputAssetID.Valid && outputVersion.Valid {
		value.Output = &contract.AssetVersionRef{
			AssetID: contract.AssetID(outputAssetID.String), Version: outputVersion.Int64,
		}
	}
	return value, nil
}

func (r MySQLRepository) GetDerivative(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, source contract.AssetVersionRef, profile DerivativeProfile) (AssetDerivative, error) {
	db, err := r.db()
	if err != nil {
		return AssetDerivative{}, err
	}
	value, err := scanDerivative(db.QueryRowContext(ctx, assetDerivativeSelect+
		` WHERE organization_id = ? AND project_id = ? AND source_asset_id = ? AND source_asset_version = ? AND profile = ?`,
		organizationID, projectID, source.AssetID, source.Version, profile))
	if errors.Is(err, sql.ErrNoRows) {
		return AssetDerivative{}, ErrNotFound
	}
	return value, err
}

func (r MySQLRepository) GetDerivativeByID(ctx context.Context, id string) (AssetDerivative, error) {
	db, err := r.db()
	if err != nil {
		return AssetDerivative{}, err
	}
	value, err := scanDerivative(db.QueryRowContext(ctx, assetDerivativeSelect+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AssetDerivative{}, ErrNotFound
	}
	return value, err
}

// CompleteDerivative 把产物写回并置为 ready。
//
// 只从 queued/running 转过来：worker 重复投递时，第二次的 UPDATE 影响 0 行，
// 而不是把一条已经 ready 的记录指向另一张图。
func (r MySQLRepository) CompleteDerivative(ctx context.Context, id string, output contract.AssetVersionRef, now time.Time) (AssetDerivative, error) {
	db, err := r.db()
	if err != nil {
		return AssetDerivative{}, err
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE asset_derivatives SET status = ?, output_asset_id = ?, output_asset_version = ?, error_code = NULL, updated_at = ?
		 WHERE id = ? AND status IN (?, ?)`,
		DerivativeReady, string(output.AssetID), output.Version, now, id, DerivativeQueued, DerivativeRunning); err != nil {
		return AssetDerivative{}, err
	}
	return r.GetDerivativeByID(ctx, id)
}
```

在该文件 import 块补 `"errors"`（若尚未引入；`database/sql` 与 `time` 已在）。

> `ErrNotFound` 在这个包里的名字要确认一次：
> ```bash
> cd /d/project/cookies-integration-mvp && grep -n "ErrNotFound\|ErrAssetNotFound" internal/platform/assets/*.go | grep -v _test | head -3
> ```
> 用它现有的那个哨兵，不要新造。

- [ ] **Step 5: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go build ./... && go test ./internal/platform/assets/ -run TestFindDerivative
```

预期：`ok`。

- [ ] **Step 6: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/platform/assets/derivatives.go internal/platform/assets/derivatives_mysql.go internal/platform/assets/derivatives_test.go && git commit -m "feat(assets): 派生物补上查询与完成两个口"
```

---

### Task 3: 把派生任务跑起来

**Files:**
- Modify: `internal/platform/assets/derivatives.go`（kind 常量、调度器、handler）
- Test: `internal/platform/assets/derivatives_test.go`

**Interfaces:**
- Consumes: Task 1 的 `PosterExtractor` / `PosterMIMEType`、Task 2 的 `GetDerivativeByID` / `CompleteDerivative`。
- Produces:
  - `const DerivativeJobKind = "assets.derivative.run"`
  - `type JobRuntimeDerivativeScheduler struct { Store jobruntime.Store; NewID func() (string, error); Now func() time.Time }`，实现 `DerivativeScheduler`
  - `type DerivativeRunner struct { Repository DerivativeRepository; Assets Repository; Blobs BlobStore; Upload UploadService; Poster PosterExtractor; Actor contract.ActorContext; Now func() time.Time }`
  - `func (r DerivativeRunner) Run(ctx context.Context, derivativeID string) error`
  - `func DerivativeRuntimeHandler(runner DerivativeRunner) jobruntime.Handler`

- [ ] **Step 1: 写失败的测试**

在 `internal/platform/assets/derivatives_test.go` 末尾追加：

```go
func TestDerivativeSchedulerRequiresStore(t *testing.T) {
	scheduler := JobRuntimeDerivativeScheduler{}
	if err := scheduler.ScheduleAssetDerivative(context.Background(), ProcessingJob{ID: "assetjob_1"}); err == nil {
		t.Fatal("没有 job store 时应当报错")
	}
}

func TestDerivativeHandlerRejectsWrongKind(t *testing.T) {
	handler := DerivativeRuntimeHandler(DerivativeRunner{})
	_, err := handler(context.Background(), jobruntime.Claim{
		Job:     contract.Job{Kind: "creative.video.render"},
		Payload: []byte(`{"derivative_id":"derivative_1"}`),
	})
	if err == nil {
		t.Fatal("别人的任务类型不该被这个 handler 接下来")
	}
}

func TestDerivativeHandlerRejectsEmptyPayload(t *testing.T) {
	handler := DerivativeRuntimeHandler(DerivativeRunner{})
	_, err := handler(context.Background(), jobruntime.Claim{
		Job: contract.Job{Kind: DerivativeJobKind}, Payload: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("载荷里没有派生物 ID 时应当报不可重试的错")
	}
	var executionErr jobruntime.ExecutionError
	if !errors.As(err, &executionErr) {
		t.Fatalf("应当是 ExecutionError，得到 %T", err)
	}
	// 载荷坏了重试多少次都还是坏的，重试只是在浪费 worker。
	if executionErr.JobError.Retryable {
		t.Fatal("载荷错误不该标成可重试")
	}
}

func TestDerivativeRunnerSkipsNonVideo(t *testing.T) {
	// 图片没有「首帧」。给图片排一个抽帧任务是上游的错，
	// 但 runner 不该因此把任务标成失败——那会在派生物表里留一堆红色噪音。
	runner := DerivativeRunner{}
	if err := runner.Run(context.Background(), ""); err == nil {
		t.Fatal("没有派生物 ID 时应当报错")
	}
}
```

> 若 `derivatives_test.go` 尚未 import `"errors"` / `"github.com/shikanon/cookies/internal/platform/jobruntime"`，加上。

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/platform/assets/ -run TestDerivative
```

预期：编译失败，`undefined: JobRuntimeDerivativeScheduler`。

- [ ] **Step 3: 实现调度器与 runner**

在 `internal/platform/assets/derivatives.go` 末尾加：

```go
// DerivativeJobKind 是派生物任务在共享 runtime 上的类型名。
const DerivativeJobKind = "assets.derivative.run"

// JobRuntimeDerivativeScheduler 把派生物任务投进 jobruntime。
// 形状照着 creative 那边的 JobRuntimeRenderScheduler 来，同一套幂等键约定。
type JobRuntimeDerivativeScheduler struct {
	Store jobruntime.Store
	NewID func() (string, error)
	Now   func() time.Time
}

func (s JobRuntimeDerivativeScheduler) ScheduleAssetDerivative(ctx context.Context, job ProcessingJob) error {
	if s.Store == nil || s.NewID == nil {
		return fmt.Errorf("job runtime store and ID generator are required")
	}
	payload, err := json.Marshal(struct {
		DerivativeID string `json:"derivative_id"`
	}{DerivativeID: job.DerivativeID})
	if err != nil {
		return err
	}
	id, err := s.NewID()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	digest := sha256.Sum256([]byte(job.ID))
	_, _, err = s.Store.Enqueue(ctx, jobruntime.CreateRequest{
		Job: contract.Job{
			ID: id, Kind: DerivativeJobKind, OrganizationID: job.OrganizationID, ProjectID: job.ProjectID,
			Status: contract.JobQueued, MaxAttempts: 3, Version: 1, CreatedAt: now, UpdatedAt: now,
		},
		// 幂等键用 ProcessingJob 的 ID 而不是派生物 ID：重试会新建一个
		// ProcessingJob，那次是要真的再跑一遍的。
		Payload: payload, IdempotencyKey: contract.IdempotencyKey("assets-derivative-" + job.ID),
		RequestHash: hex.EncodeToString(digest[:]),
	})
	return err
}

// DerivativeRunner 真的把一个派生物做出来。
//
// 目前只做 poster_v1。另外三种规格（video_proxy_v1 / waveform_v1 / font_woff2_v1）
// 的实现不在这一期——遇到它们直接跳过，不标失败，免得在表里留一堆做不出来的红条。
type DerivativeRunner struct {
	Repository DerivativeRepository
	Assets     Repository
	Blobs      BlobStore
	Upload     UploadService
	Poster     PosterExtractor
	// Actor 是系统身份。IngestDerivedImage 要 assets.write，
	// 而这条路上没有人，只有 worker。
	Actor contract.ActorContext
	Now   func() time.Time
}

func (r DerivativeRunner) Run(ctx context.Context, derivativeID string) error {
	if r.Repository == nil || r.Blobs == nil || r.Poster == nil {
		return fmt.Errorf("derivative runner is not configured")
	}
	if strings.TrimSpace(derivativeID) == "" {
		return fmt.Errorf("derivative id is required")
	}
	derivative, err := r.Repository.GetDerivativeByID(ctx, derivativeID)
	if err != nil {
		return err
	}
	if derivative.Profile != DerivativePoster {
		// 别的规格这一期没有实现。跳过，保持 queued，等以后有实现了重试就能跑。
		return nil
	}
	if derivative.Status == DerivativeReady {
		return nil
	}

	actor := r.Actor
	actor.OrganizationID = derivative.OrganizationID
	source, err := r.Assets.GetProjectAsset(ctx, derivative.OrganizationID, derivative.ProjectID, derivative.Source)
	if err != nil {
		return err
	}
	if source.Asset.Kind != contract.AssetVideo {
		// 图片没有「首帧」。排到这儿是上游排错了，不是这条任务失败了。
		return nil
	}
	reader, _, err := r.Blobs.Open(ctx, source.Version.Blob)
	if err != nil {
		return err
	}
	defer reader.Close()
	contents, err := io.ReadAll(io.LimitReader(reader, MaxVideoBytes))
	if err != nil {
		return err
	}
	poster, err := r.Poster.ExtractPoster(ctx, contents)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now()
	}
	ref, err := r.Upload.IngestDerivedImage(ctx,
		contract.RequestContext{Actor: actor},
		derivative.ProjectID, derivative.ID, derivative.Source,
		bytes.NewReader(poster), int64(len(poster)), PosterMIMEType)
	if err != nil {
		return err
	}
	_, err = r.Repository.CompleteDerivative(ctx, derivative.ID, ref.AssetVersion, now)
	return err
}

func DerivativeRuntimeHandler(runner DerivativeRunner) jobruntime.Handler {
	return func(ctx context.Context, claim jobruntime.Claim) (jobruntime.Result, error) {
		var payload struct {
			DerivativeID string `json:"derivative_id"`
		}
		if claim.Job.Kind != DerivativeJobKind || json.Unmarshal(claim.Payload, &payload) != nil || strings.TrimSpace(payload.DerivativeID) == "" {
			return jobruntime.Result{}, jobruntime.ExecutionError{JobError: contract.JobError{
				Code: "ASSET_DERIVATIVE_PAYLOAD_INVALID", Message: "Asset derivative payload is invalid", Retryable: false,
			}}
		}
		if err := runner.Run(ctx, payload.DerivativeID); err != nil {
			// 标成可重试：抽帧失败大多是取文件超时或 ffmpeg 一时起不来，
			// 重试三次比第一次就放弃合理。三次之后 jobruntime 自己会收手。
			return jobruntime.Result{}, jobruntime.ExecutionError{JobError: contract.JobError{
				Code: "ASSET_DERIVATIVE_FAILED", Message: "Asset derivative execution failed", Retryable: true,
			}}
		}
		return jobruntime.Result{}, nil
	}
}
```

在 `derivatives.go` 的 import 块补上：

```go
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/shikanon/cookies/internal/platform/jobruntime"
```

> 字段位置已核对：`ProjectAsset` 的 `Kind` 在 `.Asset` 上（见 `services_test.go:279` 的 `stored.Asset.Kind`），`Blob` 与 `Status` 在 `.Version` 上（见 `upload_service.go:245-249`）。别把 `Kind` 写到 `.Version` 上去，那是编译期才会发现的错。`MaxVideoBytes`（`model.go:13`，200 MiB）与 `ErrNotFound`（`repository.go:11`）都是这个包里已有的。
>
> `r.Assets` 的类型是这个包的 `Repository` 接口，`GetProjectAsset(ctx, org, project, ref) (ProjectAsset, error)` 就在上面。

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go build ./... && go test ./internal/platform/assets/ -run TestDerivative
```

预期：`ok`。

- [ ] **Step 5: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/platform/assets/derivatives.go internal/platform/assets/derivatives_test.go && git commit -m "feat(assets): 派生物的调度器与 worker，先只做首帧图"
```

---

### Task 4: 洞察侧取用

**Files:**
- Modify: `internal/systems/insights/assets.go`（`PosterReader` 端口 + `ReadAssetPoster`）
- Create: `internal/integrations/insightsposter/reader.go`
- Modify: `internal/systems/insights/httpapi/assets.go`
- Test: `internal/systems/insights/assets_test.go`

**Interfaces:**
- Consumes: Task 2 的 `DerivativeService.FindDerivative`、已有的 `UploadService.Preview`。
- Produces:
  - `type PosterReader interface { ReadPoster(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, platformAssetID string, platformAssetVersion int64) (string, error) }`
  - `Service.Posters PosterReader`（新字段）
  - `func (s Service) ReadAssetPoster(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string) (string, error)`
  - `insightsposter.Reader{Derivatives assets.DerivativeService; Uploads *assets.UploadService}`
  - `GET /api/insights/v1/projects/{project_id}/assets/{asset_id}/poster` → 302

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/assets_test.go` 末尾追加：

```go
func TestReadAssetPosterNeedsPlatformReference(t *testing.T) {
	service, actor := testService(), testActor()
	ctx := context.Background()
	asset, err := service.IndexAsset(ctx, actor, "k_project_1", IndexAssetRequest{
		Title: "手工登记的一条", SourceKind: AssetSourceUpload,
	})
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	// 没有平台引用就没有源文件，也就无从抽帧。这时候要明确说没有，
	// 不能返回空串让前端拿去当 URL——那会加载一个空地址，控制台一片红。
	_, err = service.ReadAssetPoster(ctx, actor, "k_project_1", asset.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("没有平台引用时应当报没有，得到 %v", err)
	}
}

func TestReadAssetPosterWithoutPortSaysSo(t *testing.T) {
	service, actor := testService(), testActor()
	service.Posters = nil
	ctx := context.Background()
	asset, err := service.IndexAsset(ctx, actor, "k_project_1", IndexAssetRequest{
		Title: "有平台引用的一条", SourceKind: AssetSourceUpload,
		PlatformAssetID: "asset_1", PlatformAssetVersion: 1,
	})
	if err != nil {
		t.Fatalf("登记失败：%v", err)
	}
	if _, err := service.ReadAssetPoster(ctx, actor, "k_project_1", asset.ID); err == nil {
		t.Fatal("没接封面端口时应当报错，而不是假装有封面")
	}
}
```

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestReadAssetPoster
```

预期：编译失败，`service.ReadAssetPoster undefined`。

- [ ] **Step 3: 加端口与服务方法**

在 `internal/systems/insights/assets.go` 里，`AssetRepository` 接口定义之后加：

```go
// PosterReader 把一个平台素材版本的封面换成一个可以直接放进 <img src> 的地址。
//
// 洞察自己不抽帧、不存图：那是素材库的事。这里只要一个能看的地址。
type PosterReader interface {
	ReadPoster(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, platformAssetID string, platformAssetVersion int64) (string, error)
}
```

在 `Service` 结构体里，`Media` 字段附近加：

```go
	Posters PosterReader
```

在 `ListAssetPage` 之后加：

```go
// ReadAssetPoster 取一条素材的封面地址。
//
// 取不到就报 ErrNotFound，让前端退回类型图标。封面是锦上添花——
// 为了一张缩略图让整个清单打不开是本末倒置。
func (s Service) ReadAssetPoster(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, assetID string) (string, error) {
	if err := s.assetsReady(actor, projectID, ScopeRead); err != nil {
		return "", err
	}
	asset, err := s.Assets.GetAsset(ctx, actor.OrganizationID, projectID, assetID)
	if err != nil {
		return "", err
	}
	if asset.PlatformAssetID == "" || asset.PlatformAssetVersion == 0 {
		return "", fmt.Errorf("%w: 这条素材没有对应的平台文件，没有封面可取", ErrNotFound)
	}
	if s.Posters == nil {
		return "", fmt.Errorf("封面服务没有接通")
	}
	return s.Posters.ReadPoster(ctx, actor, projectID, asset.PlatformAssetID, asset.PlatformAssetVersion)
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && go test ./internal/systems/insights/ -run TestReadAssetPoster
```

预期：`ok`，2 个测试全过。

- [ ] **Step 5: 写适配器**

创建 `internal/integrations/insightsposter/reader.go`：

```go
// Package insightsposter 把素材库的 poster_v1 派生物换成洞察能直接用的地址。
//
// 分层：洞察不许 import 素材库的实现细节（派生物、签名、blob），
// 素材库也不该认识洞察。中间这一层两边都认识，装配时塞进去。
package insightsposter

import (
	"context"
	"fmt"

	"github.com/shikanon/cookies/internal/platform/assets"
	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/systems/insights"
)

type Reader struct {
	Derivatives assets.DerivativeService
	Uploads     *assets.UploadService
}

func (r Reader) ReadPoster(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, platformAssetID string, platformAssetVersion int64) (string, error) {
	if r.Uploads == nil {
		return "", fmt.Errorf("asset upload service is required")
	}
	source := contract.AssetVersionRef{AssetID: contract.AssetID(platformAssetID), Version: platformAssetVersion}
	derivative, err := r.Derivatives.FindDerivative(ctx, actor.OrganizationID, projectID, source, assets.DerivativePoster)
	if err != nil {
		return "", err
	}
	// 还在排队或者做失败了，都算「现在没有封面」。前端退回类型图标，
	// 过一会儿再进这一页就有了——不必给用户看一个「生成中」的转圈。
	if derivative.Status != assets.DerivativeReady || derivative.Output == nil {
		return "", insights.ErrNotFound
	}
	signed, err := r.Uploads.Preview(ctx, actor, projectID, *derivative.Output)
	if err != nil {
		return "", err
	}
	return signed.URL, nil
}
```

> `SignedRequest` 的地址字段名要确认一次：
> ```bash
> cd /d/project/cookies-integration-mvp && grep -n "type SignedRequest" -A 8 internal/platform/assets/blobstore.go
> ```
> 若不叫 `URL`，改成现有的那个字段名。

- [ ] **Step 6: 加 HTTP 路由**

在 `internal/systems/insights/httpapi/assets.go` 的路由注册里，`{asset_id}/lineage` 那一行之后加：

```go
	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/assets/{asset_id}/poster", s.getAssetPoster)
```

在 `listAssetLineage` 之后加：

```go
// getAssetPoster 把请求 302 到一个带签名、会过期的地址。
//
// 不代理字节：缩略图是最高频的请求，一个清单一屏就是几十张。让它们直接
// 打到对象存储，API 只负责说清「哪一张、能看多久」。
func (s *Server) getAssetPoster(writer http.ResponseWriter, request *http.Request) {
	url, err := s.app.ReadAssetPoster(request.Context(), mustActor(request), projectID(request), request.PathValue("asset_id"))
	if err != nil {
		writeError(writer, request, err)
		return
	}
	http.Redirect(writer, request, url, http.StatusFound)
}
```

- [ ] **Step 7: 编译并跑 httpapi 测试**

```bash
cd /d/project/cookies-integration-mvp && go build ./... && go test ./internal/systems/insights/... ./internal/integrations/insightsposter/
```

预期：`ok`。

- [ ] **Step 8: 更新 OpenAPI**

在 `api/openapi/insights-v1.yaml` 的 `/projects/{project_id}/assets/{asset_id}/lineage` 之后加：

```yaml
  /projects/{project_id}/assets/{asset_id}/poster:
    get:
      summary: 取素材封面
      description: 302 跳到一个带签名、会过期的图片地址。没有封面（还没做出来、做失败了、这条素材没有平台文件）时返回 404，前端应退回类型图标。
      operationId: getInsightAssetPoster
      parameters:
        - $ref: '#/components/parameters/ProjectId'
        - name: asset_id
          in: path
          required: true
          schema:
            type: string
      responses:
        '302':
          description: 跳到封面图片
          headers:
            Location:
              schema:
                type: string
        '404':
          description: 现在没有封面
```

> `ProjectId` 这个 parameter 引用名以该文件里现有的写法为准（其他素材端点怎么引的就怎么引）。

- [ ] **Step 9: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/systems/insights/assets.go internal/systems/insights/assets_test.go internal/systems/insights/httpapi/assets.go internal/integrations/insightsposter/ api/openapi/insights-v1.yaml && git commit -m "feat(insights): 素材封面的取用端点"
```

---

### Task 5: 装配与触发

**Files:**
- Modify: `cmd/cookies-api/main.go`
- Modify: `internal/platform/assets/upload_service.go`（`recordLedger` 旁边加 `ensurePoster`）

**Interfaces:**
- Consumes: Task 1-4 的全部产物、主计划 Task 8 的 `recordLedger` 挂钩位置。
- Produces: `UploadService.Derivatives *DerivativeService`（新字段）、`func (s UploadService) ensurePoster(ctx context.Context, projectID contract.ProjectID, organizationID contract.OrganizationID, ref contract.AssetVersionRef, kind contract.AssetKind)`。

- [ ] **Step 1: 加触发**

在 `internal/platform/assets/upload_service.go` 的 `UploadService` 结构体里，`Ledger` 之后加：

```go
	Derivatives      *DerivativeService
```

在 `recordLedger` 之后加：

```go
// ensurePoster 给刚入库的视频排一个抽帧任务。
//
// 只对视频排：图片自己就是自己的缩略图，音频和文档没有画面。
// 失败只记日志：封面做不出来不影响素材本身，清单退回类型图标就是了。
func (s UploadService) ensurePoster(ctx context.Context, organizationID contract.OrganizationID, projectID contract.ProjectID, ref contract.AssetVersionRef, kind contract.AssetKind) {
	if s.Derivatives == nil || kind != contract.AssetVideo {
		return
	}
	if _, _, _, err := s.Derivatives.EnsureDerivative(ctx, EnsureDerivativeRequest{
		OrganizationID: organizationID, ProjectID: projectID, AssetRef: ref, Profile: DerivativePoster,
	}); err != nil {
		log.Printf("排素材封面任务失败 asset=%s version=%d: %v", ref.AssetID, ref.Version, err)
	}
}
```

在主计划 Task 8 加过 `s.recordLedger(...)` 的**四个位置**，各在其后补一行（变量名沿用那一处已有的）：

```go
	s.ensurePoster(ctx, <该处的 organizationID>, <该处的 projectID>, contract.AssetVersionRef{AssetID: ref.AssetVersion.AssetID, Version: ref.AssetVersion.Version}, commit.Kind)
```

`generated_intake_service.go` 那一处的 receiver 是 `w.Upload`，写成 `w.Upload.ensurePoster(...)`，ref 用 `contract.AssetVersionRef{AssetID: commit.AssetID, Version: commit.Version}`。

- [ ] **Step 2: 装配**

在 `cmd/cookies-api/main.go` 中：

**2a.** 在 `ledgerRelay := &assets.LedgerRelay{}` 附近（uploadService 构造之前）加：

```go
	// 派生物（目前只有视频首帧图）。ffmpeg 路径没配就整条不启用——
	// 本地开发机没装 ffmpeg 也要能把服务跑起来。
	var derivativeService *assets.DerivativeService
	if strings.TrimSpace(ffmpegPath) != "" {
		derivativeService = &assets.DerivativeService{
			Repository: assets.MySQLRepository{DB: db},
			Scheduler: assets.JobRuntimeDerivativeScheduler{
				Store: runtimeStore, NewID: func() (string, error) { return ids.New("job") },
			},
		}
	}
```

> `ffmpegPath` 是 main.go 里已经解析出来的那个变量（:63-64 附近，和 `ffprobePath` 一起）。`assets.MySQLRepository` 的构造方式照该文件里 `assetRepository` 那一行来。`ids.New` 的调用签名照 `JobRuntimeRenderScheduler` 在 main.go 里的装配处抄。
>
> 注意 `runtimeStore` 在 :322 才构造，而 uploadService 在 :132。把上面这一段放在 `runtimeStore := jobruntime.MySQLStore{DB: db}` **之后**，然后用 `uploadService.Derivatives = derivativeService` 回填，而不是塞进字面量。

**2b.** 在 `runtimeStore` 构造之后加：

```go
	uploadService.Derivatives = derivativeService
```

**2c.** 在 `runtimeHandlers` 那一段（:615 附近）加：

```go
	if derivativeService != nil {
		runtimeHandlers[assets.DerivativeJobKind] = assets.DerivativeRuntimeHandler(assets.DerivativeRunner{
			Repository: assets.MySQLRepository{DB: db}, Assets: assetRepository, Blobs: blobs,
			Upload: *uploadService,
			Poster: assets.FFmpegPosterExtractor{Path: ffmpegPath, WorkRoot: filepath.Join(".data", "poster-work")},
			Actor:  *actor,
		})
	}
```

> `*actor` 是 :76 拿到的系统身份，和 `intakeWorker`（:786）用的是同一个。若 `filepath` 尚未 import，加上。

**2d.** 在 `insightsService` 字面量里，`Media:` 那一行之后加：

```go
		Posters: insightsposter.Reader{Uploads: uploadService},
```

并在 `insightsService` 构造之后、`ledgerRelay.Recorder = ...` 附近补一行（`Posters` 里的 `Derivatives` 是值类型，要在 derivativeService 非 nil 时才填）：

```go
	if derivativeService != nil {
		insightsService.Posters = insightsposter.Reader{Derivatives: *derivativeService, Uploads: uploadService}
	}
```

**2e.** import 块加：

```go
	"github.com/shikanon/cookies/internal/integrations/insightsposter"
```

- [ ] **Step 3: 编译并跑全量**

```bash
cd /d/project/cookies-integration-mvp && go build ./... && go vet ./... && go test ./internal/... ./cmd/...
```

预期：全 `ok`。

- [ ] **Step 4: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add internal/platform/assets/upload_service.go internal/platform/assets/generated_intake_service.go internal/platform/assets/external_import.go cmd/cookies-api/main.go && git commit -m "feat(assets): 视频入库后自动排首帧图，装配 worker 与取用端口"
```

---

### Task 6: 清单上显示缩略图

**Files:**
- Modify: `src/data/api.ts`
- Modify: `src/components/insight/assets/LedgerView.tsx`
- Modify: `src/styles.css`
- Test: `test/insight-poster.test.ts`

**Interfaces:**
- Consumes: Task 4 的 `GET .../assets/{asset_id}/poster`。
- Produces: `api.insightAssetPosterUrl(projectId: string, assetId: string): string`、`LedgerThumb` 组件（`LedgerView.tsx` 内，导出供测试）。

- [ ] **Step 1: 写失败的测试**

创建 `test/insight-poster.test.ts`：

```ts
import assert from 'node:assert/strict'
import test from 'node:test'
import { api } from '../src/data/api.ts'

/**
 * 缩略图是 <img src> 直接打过去的，不走 fetch——所以它必须是一个能拼出来的地址，
 * 不是一个要先 await 的 Promise。一屏几十张图各发一次 JSON 请求再取地址，
 * 清单会卡住。
 */
test('封面地址拼得出来，而且带项目和素材两段', () => {
  const url = api.insightAssetPosterUrl('k_project_1', 'insightasset_7')
  assert.match(url, /k_project_1/)
  assert.match(url, /insightasset_7/)
  assert.match(url, /\/poster$/)
})
```

- [ ] **Step 2: 跑它确认失败**

```bash
cd /d/project/cookies-integration-mvp && npx tsx --test test/insight-poster.test.ts
```

预期：FAIL，`api.insightAssetPosterUrl is not a function`。

- [ ] **Step 3: 加地址拼装**

在 `src/data/api.ts` 的 `listInsightAssetLineage` 附近加：

```ts
  // 缩略图给 <img src> 用，所以是同步拼地址而不是发请求。
  // 后端 302 到一个带签名的对象存储地址；没有封面时返回 404，
  // 浏览器的 onError 会把它换成类型图标。
  insightAssetPosterUrl: (projectId: string, assetId: string) =>
    `${insightAssetPath(projectId, assetId)}/poster`,
```

> `insightAssetPath` 是该文件里已有的路径辅助。若它返回的是相对路径而 `<img>` 需要绝对地址，照该文件里 `request` 拼 base URL 的方式补上同一个前缀。

- [ ] **Step 4: 跑测试确认通过**

```bash
cd /d/project/cookies-integration-mvp && npx tsx --test test/insight-poster.test.ts
```

预期：通过。

- [ ] **Step 5: 台账上显示**

在 `src/components/insight/assets/LedgerView.tsx` 中，`ledgerSourceLabel` 之后加：

```tsx
/**
 * 缩略图。取不到就退回一个类型图标——封面是锦上添花，
 * 一张图裂了不该让整行看起来像坏了。
 */
export function LedgerThumb({ projectId, assetId, title }: { projectId: string; assetId: string; title: string }) {
  const [failed, setFailed] = useState(false)
  if (failed) return <span className="ledger-thumb ledger-thumb-empty" aria-hidden="true"><ImageOff size={16}/></span>
  return <img
    className="ledger-thumb"
    src={api.insightAssetPosterUrl(projectId, assetId)}
    alt={`${title} 的封面`}
    loading="lazy"
    onError={() => setFailed(true)}/>
}
```

把 `<li>` 里的内容改成：

```tsx
      {items.map(asset => <li key={asset.id}>
        <LedgerThumb projectId={currentProject.id} assetId={asset.id} title={asset.title}/>
        <div>
          <strong>{asset.title}</strong>
          <small>{ledgerSourceLabel(asset.source_kind)} · {isoDate(new Date(asset.created_at))}</small>
        </div>
        <button type="button" onClick={() => void promote(asset)}>拉进分析</button>
      </li>)}
```

把该文件的 lucide 引入改成：

```tsx
import { CircleAlert, ImageOff } from 'lucide-react'
```

- [ ] **Step 6: 加样式**

在 `src/styles.css` 末尾加：

```css
.assets-ledger-list li { display: grid; grid-template-columns: 64px 1fr auto; gap: 12px; align-items: center; }
.ledger-thumb { width: 64px; height: 36px; object-fit: cover; border-radius: 4px; background: oklch(.22 .01 260); }
.ledger-thumb-empty { display: grid; place-items: center; color: oklch(.62 .01 260); }
```

> 若 `.assets-ledger-list li` 在主计划 Task 12 已有布局规则，改那一条而不是新增第二条 —— 两条同选择器的规则会让后来读的人搞不清哪条生效。

- [ ] **Step 7: 全量前端验证**

```bash
cd /d/project/cookies-integration-mvp && npx tsc --noEmit -p tsconfig.json && npm test && npm run build
```

预期：`tsc` 与 `build` 无错，`npm test` 比主计划完成后的基线多 1 个通过、0 failed。

- [ ] **Step 8: 提交**

```bash
cd /d/project/cookies-integration-mvp && git add src/data/api.ts src/components/insight/assets/LedgerView.tsx src/styles.css test/insight-poster.test.ts && git commit -m "feat(web): 素材台账显示视频首帧缩略图"
```

---

### Task 7: 端到端验一次

**Files:** 无新增，只跑验证。

- [ ] **Step 1: 起服务，传一个视频**

按项目现有的本地起法启动 `cookies-api`（`go run ./cmd/cookies-api`，环境变量照 README）。确认启动日志里 ffmpeg 路径已解析（没有的话这一条链路是关的，本步跳过并记录）。

通过素材库上传一个 mp4，记下返回的 `asset_id` 与 `version`。

- [ ] **Step 2: 确认任务排上了**

```bash
cd /d/project/cookies-integration-mvp && mysql -h 127.0.0.1 -u root -p"$MYSQL_PASSWORD" cookies -e "SELECT id, profile, status, output_asset_id FROM asset_derivatives ORDER BY created_at DESC LIMIT 5;"
```

预期：最上面一条 `profile=poster_v1`。刚上传时可能是 `queued`，等十几秒 worker 跑过之后应变成 `ready` 且 `output_asset_id` 非空。

> 连接参数以本地实际配置为准；连不上就跳过，并在最终汇报里写明「端到端未验」。

- [ ] **Step 3: 确认封面取得到**

```bash
curl -sI -H "Authorization: Bearer $TOKEN" "http://localhost:8080/api/insights/v1/projects/$PROJECT/assets/$INSIGHT_ASSET_ID/poster" | head -3
```

预期：`HTTP/1.1 302 Found` 且有 `Location:` 头。

- [ ] **Step 4: 看一眼台账**

打开素材 → 台账，确认视频那几条左边是首帧图而不是灰块。非视频那几条是灰块加图标，这是对的。

- [ ] **Step 5: 收尾提交**

```bash
cd /d/project/cookies-integration-mvp && git status --short
```

有遗漏的改动就逐个 `git add <路径>` 后提交。**不要用 `git add -A`**。

---

## 验收

| # | 条件 | 由哪个 Task 满足 |
|---|---|---|
| 1 | 有一个真的会抽帧的 `poster_v1` 实现 | Task 1 |
| 2 | 派生物能查、能标完成 | Task 2 |
| 3 | 派生任务真的会跑 | Task 3、5 |
| 4 | 封面有取用端点，取不到时明确说没有 | Task 4 |
| 5 | 视频入库自动排任务 | Task 5 |
| 6 | 清单上看得见，失败退回图标 | Task 6 |
| 7 | ffmpeg 缺席时整条链路静默关闭，服务照常起 | Task 5 Step 2a |

**不在本计划内**：另外三种派生规格（`video_proxy_v1` / `waveform_v1` / `font_woff2_v1`）。`DerivativeRunner` 遇到它们直接跳过、不标失败，等各自的实现补上之后重试就能跑。派生物的重试 HTTP 入口（`RetryDerivative` 至今没有路由）也不在这一期——本期失败的封面靠 jobruntime 自己的三次重试，三次都失败就是没有封面。
