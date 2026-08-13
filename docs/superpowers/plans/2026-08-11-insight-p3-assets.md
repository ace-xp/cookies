# 素材洞察 · 模块四「素材」实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把「数据接入」「分析素材库」「内容分析」合成一个叫「素材」的入口，并补上两件让样本变厚的能力：按特征重叠找相似素材（❓ 的升级通道），以及把平台外的素材作为**证据**收进来而不是作为资产。

**Architecture:** 素材库本身归创意创作那边，洞察这边只维护**分析索引**——两者靠 `platform_asset_id` 两个裸字段挂钩，本期不引入对 `internal/platform/assets` 的 import。相似素材用特征重叠算，不用向量：重叠能说出「像在哪」，向量只能给一个分数，而分析页上一条说不出理由的推荐没人敢用。外部素材单独一张表、单独一个存储前缀、只读、到期清理原件——它是证据，不是资产，永远不进共享素材库。

**Tech Stack:** Go 1.22+、MySQL 8、React 19 + TypeScript 5.9 + Vite 6、`tsx --test`。

## Global Constraints

- 前置依赖：**P0 地基**。相似素材会回填 P0 `NotEnoughSample` 的 `onFindSimilar`。
- 一律中文注释与中文用户可见文案；注释写「为什么」，不写「是什么」。
- **不引入向量检索、不引入任何新依赖。** 相似度用特征重叠在 Go 里算。引入新库要先经使用者同意。
- **外部素材永远不进共享素材库。** 创意创作那边不接受平台外素材（2026-08-04 确认）。任何把 `external_assets` 行写进 `assets` 表或调用 `IngestRenderedVideo` 的代码都是错的。
- 外部素材**只读**：不能改标题、不能改特征、不能改用途声明。要改就重新导一份。
- 归因只认 `derived`（客观可测）和 `human`（人工标注）两类变量。相似检索可以用 `ai` 变量找候选，但结果里必须标出来「这一条的相似只建立在模型推断的变量上」。
- 名词按 P0 的名词表：这一页叫「素材」；`external` 那批叫「外部素材」不叫竞品素材；「找相似素材」是固定说法。
- 迁移文件放 `migrations/insights/`，命名 `YYYYMMDDHHMMSS_<描述>.up.sql`，文件头必须有中文注释说明为什么要改。
- 提交信息用中文，格式 `<type>(insights): <做了什么>`。

---

## 文件结构

| 文件 | 职责 | 本期动作 |
|---|---|---|
| `internal/systems/insights/similar.go` | 特征重叠相似度与检索 | 新建 |
| `internal/systems/insights/similar_test.go` | 相似度的全部约束 | 新建 |
| `internal/systems/insights/external.go` | 外部素材：模型、导入、留存 | 新建 |
| `internal/systems/insights/external_test.go` | 外部素材的全部约束 | 新建 |
| `internal/systems/insights/mysql_external_repository.go` | 外部素材落库 | 新建 |
| `internal/systems/insights/assets.go` | `AssetRepository` 加两个查询方法 | 改 |
| `internal/systems/insights/httpapi/server.go` | 三条路由 | 改 |
| `migrations/insights/20260811110000_insight_external_assets.up.sql` | 外部素材表 | 新建 |
| `api/openapi/insights-v1.yaml` | 三个接口与三个 schema | 改 |
| `src/components/insight/assets/AssetsPage.tsx` | 素材入口的壳 | 新建 |
| `src/components/insight/assets/OverviewView.tsx` | 视图一 · 总览（平台内 / 外部两栏 + 三个队列） | 新建 |
| `src/components/insight/assets/IntakeView.tsx` | 视图二 · 数据接入（原「数据接入」整页） | 新建 |
| `src/components/insight/assets/FeatureView.tsx` | 视图三 · 变量（原「特征」＋内容分析） | 新建 |
| `src/components/insight/assets/SimilarView.tsx` | 视图四 · 找相似 | 新建 |
| `src/components/insight/assets/ExternalView.tsx` | 视图五 · 外部素材 | 新建 |
| `src/components/insight/assets/AssetDetail.tsx` | 单条素材：上半截读素材库、下半截是洞察自己的 | 新建 |
| `src/components/insight/assets/SimilarPanel.tsx` | 可嵌进分析页的相似素材面板 | 新建 |
| `src/components/insight/assets/index.ts` | 出口 | 新建 |
| `src/components/insight/shared/NotEnoughSample.tsx` | 回填 `onFindSimilar` | 改 |
| `src/data/api.ts` | `findSimilarAssets`、`importExternalAsset`、`listExternalAssets` | 改 |
| `src/data/navigation.ts` | 「数据接入」+「分析素材库」+「内容分析」→「素材」 | 改 |

> **需要确认的破坏性动作：** Task 7 删导航条目，以及最终删除 `AssetLibraryPage.tsx` / `ContentAnalysisPage.tsx` / `DataConnectionsPage.tsx`。执行到那一步前必须先向使用者确认；在得到确认之前保留旧文件不引用即可。

---

### Task 1: 特征重叠相似度

**Files:**
- Create: `internal/systems/insights/similar.go`
- Create: `internal/systems/insights/similar_test.go`

**Interfaces:**
- Consumes: 既有 `AssetFeature` / `FeatureValue` / `FeatureSource` / `SourceAI` / `SourceHuman`；P0 Task 3 的 `SourceDerived`。
- Produces:
  - `type FeatureProbe map[string]string` —— 变量键到值的映射，检索的探针
  - `type SimilarityReason struct{ Key, Label, Value string; Source FeatureSource }`
  - `type SimilarAsset struct{ AssetID, Title string; Overlap int; AdmissibleOverlap int; Score float64; Reasons []SimilarityReason }`
  - `func scoreSimilarity(probe FeatureProbe, candidate map[string]AssetFeature) SimilarAsset` —— 只填 `Overlap` / `AdmissibleOverlap` / `Score` / `Reasons`
  - `func rankSimilar(values []SimilarAsset, limit int) []SimilarAsset` —— 先按可归因重叠、再按总重叠、最后按素材 ID 排；ID 兜底是为了结果稳定

- [ ] **Step 1: 写失败的测试**

创建 `internal/systems/insights/similar_test.go`：

```go
package insights

import "testing"

func probeFeature(key, value string, source FeatureSource) AssetFeature {
	return AssetFeature{Key: key, Label: key, Source: source, Value: FeatureValue{Text: value}}
}

// 相似度是「重叠了几个变量」，不是一个不可解释的分数。
//
// 用向量的话结果只有一个 0.87，人问「像在哪」答不上来——而这批素材是要被拿去
// 做归因的，说不出像在哪就等于说不出为什么能凑成一组。
func TestSimilarityCountsOverlappingFeatures(t *testing.T) {
	t.Parallel()

	probe := FeatureProbe{"duration": "15s", "opening": "face", "bgm": "upbeat"}
	candidate := map[string]AssetFeature{
		"duration": probeFeature("duration", "15s", SourceHuman),
		"opening":  probeFeature("opening", "face", SourceHuman),
		"bgm":      probeFeature("bgm", "quiet", SourceHuman),
	}

	got := scoreSimilarity(probe, candidate)
	if got.Overlap != 2 {
		t.Errorf("重叠数应该是 2，得到 %d", got.Overlap)
	}
	if len(got.Reasons) != 2 {
		t.Fatalf("每个重叠都要给出理由，得到 %d 条", len(got.Reasons))
	}
	// 理由必须说清是哪个变量取什么值，「相似度 0.67」不算理由。
	if got.Reasons[0].Key == "" || got.Reasons[0].Value == "" {
		t.Errorf("理由缺变量或取值：%+v", got.Reasons[0])
	}
}

// 模型推断的变量能用来找候选，但不能算进「可归因重叠」。
//
// 找候选和做归因是两件事：找的时候宽一点没坏处，最多多看几个；
// 归因的时候松一格，结论就建立在一个没人核过的推断上。
func TestSimilarityCountsAdmissibleOverlapSeparately(t *testing.T) {
	t.Parallel()

	probe := FeatureProbe{"duration": "15s", "mood": "warm"}
	candidate := map[string]AssetFeature{
		"duration": probeFeature("duration", "15s", SourceDerived),
		"mood":     probeFeature("mood", "warm", SourceAI),
	}

	got := scoreSimilarity(probe, candidate)
	if got.Overlap != 2 {
		t.Errorf("总重叠应该是 2，得到 %d", got.Overlap)
	}
	if got.AdmissibleOverlap != 1 {
		t.Errorf("可归因重叠应该只算 duration 一个，得到 %d", got.AdmissibleOverlap)
	}
}

// 完全不重叠的候选分数为 0，不进结果。给一个「最不差的」出来比不给更糟：
// 人会以为系统找到了东西。
func TestSimilarityIsZeroWhenNothingOverlaps(t *testing.T) {
	t.Parallel()

	got := scoreSimilarity(FeatureProbe{"duration": "15s"}, map[string]AssetFeature{
		"duration": probeFeature("duration", "60s", SourceHuman),
	})
	if got.Overlap != 0 || got.Score != 0 {
		t.Errorf("不重叠应该是 0 分，得到 %+v", got)
	}
}

// 可归因重叠多的排前面，哪怕总重叠一样多。人拿这批素材去做归因，
// 排前面的应该是最能支撑结论的那些。
func TestRankPrefersAdmissibleOverlap(t *testing.T) {
	t.Parallel()

	ranked := rankSimilar([]SimilarAsset{
		{AssetID: "b", Overlap: 3, AdmissibleOverlap: 1},
		{AssetID: "a", Overlap: 3, AdmissibleOverlap: 3},
	}, 10)
	if ranked[0].AssetID != "a" {
		t.Errorf("可归因重叠多的应该排前面，得到 %q", ranked[0].AssetID)
	}
}

// 排序必须稳定。同分的两条今天这个在前、明天那个在前，人会以为数据变了。
func TestRankIsStableForTies(t *testing.T) {
	t.Parallel()

	first := rankSimilar([]SimilarAsset{
		{AssetID: "b", Overlap: 2, AdmissibleOverlap: 2},
		{AssetID: "a", Overlap: 2, AdmissibleOverlap: 2},
	}, 10)
	if first[0].AssetID != "a" {
		t.Errorf("同分按素材 ID 排，得到 %q", first[0].AssetID)
	}
}

func TestRankRespectsLimit(t *testing.T) {
	t.Parallel()

	values := make([]SimilarAsset, 0, 20)
	for index := 0; index < 20; index++ {
		values = append(values, SimilarAsset{AssetID: string(rune('a' + index)), Overlap: 1, AdmissibleOverlap: 1})
	}
	if got := rankSimilar(values, 5); len(got) != 5 {
		t.Errorf("limit 没生效，得到 %d 条", len(got))
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestSimilarity|TestRank' -v
```

Expected: 编译失败，`undefined: FeatureProbe` 等。

- [ ] **Step 3: 实现**

创建 `internal/systems/insights/similar.go`：

```go
package insights

import "sort"

// 找相似素材：❓「算不出来」的升级通道。
//
// 一个变量在本轮只有三条素材、几百次展示，算不出任何东西。但库里可能还有十几条
// 同样是这个取值的素材——把它们拉进来，样本就够了。这是「样本永远不够」这个
// 前提下唯一能做的事。
//
// **用特征重叠，不用向量。** 向量能给出更细的相似，但只能给一个分数；
// 这批素材是要被拿去做归因的，说不出「像在哪」就等于说不出为什么能凑成一组，
// 而一条说不出理由的推荐，在复盘会上没人敢用。

// FeatureProbe 是检索的探针：变量键到取值。
type FeatureProbe map[string]string

// SimilarityReason 是「像在哪」的一条。带 Source 是因为读的人有权知道
// 这一条相似是量出来的、人标的，还是模型猜的。
type SimilarityReason struct {
	Key    string        `json:"key"`
	Label  string        `json:"label"`
	Value  string        `json:"value"`
	Source FeatureSource `json:"source"`
}

// SimilarAsset 是一条检索结果。
type SimilarAsset struct {
	AssetID string `json:"asset_id"`
	Title   string `json:"title"`

	// Overlap 是重叠的变量数，AdmissibleOverlap 是其中能进归因的那些
	// （derived / human）。两个都给，因为它们回答的是不同的问题：
	// 前者是「像不像」，后者是「拉进来之后能不能真的做归因」。
	Overlap           int `json:"overlap"`
	AdmissibleOverlap int `json:"admissible_overlap"`

	// Score 是重叠数占探针变量数的比例，只用于展示「像到什么程度」。
	Score   float64            `json:"score"`
	Reasons []SimilarityReason `json:"reasons"`
}

// scoreSimilarity 算一个候选和探针的重叠。
func scoreSimilarity(probe FeatureProbe, candidate map[string]AssetFeature) SimilarAsset {
	result := SimilarAsset{Reasons: make([]SimilarityReason, 0, len(probe))}
	if len(probe) == 0 {
		return result
	}
	// 按键排序遍历，让理由的顺序稳定——同一次检索两次打开顺序不一样，
	// 人会以为结果变了。
	keys := make([]string, 0, len(probe))
	for key := range probe {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		feature, present := candidate[key]
		if !present || feature.Value.Text != probe[key] {
			continue
		}
		result.Overlap++
		if feature.Source.admissible() {
			result.AdmissibleOverlap++
		}
		label := feature.Label
		if label == "" {
			label = key
		}
		result.Reasons = append(result.Reasons, SimilarityReason{
			Key: key, Label: label, Value: feature.Value.Text, Source: feature.Source,
		})
	}
	result.Score = float64(result.Overlap) / float64(len(probe))
	return result
}

// rankSimilar 排序并截断。可归因重叠优先——人拿这批素材去做归因，
// 排前面的应该是最能支撑结论的那些。同分按素材 ID 排，保证结果稳定。
func rankSimilar(values []SimilarAsset, limit int) []SimilarAsset {
	ranked := make([]SimilarAsset, 0, len(values))
	for _, value := range values {
		if value.Overlap > 0 {
			ranked = append(ranked, value)
		}
	}
	sort.SliceStable(ranked, func(left, right int) bool {
		if ranked[left].AdmissibleOverlap != ranked[right].AdmissibleOverlap {
			return ranked[left].AdmissibleOverlap > ranked[right].AdmissibleOverlap
		}
		if ranked[left].Overlap != ranked[right].Overlap {
			return ranked[left].Overlap > ranked[right].Overlap
		}
		return ranked[left].AssetID < ranked[right].AssetID
	})
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}
```

> `Overlap > 0` 的过滤让 `TestSimilarityIsZeroWhenNothingOverlaps` 的意图在检索层也成立：0 分的候选永远不进结果。
>
> `FeatureSource.admissible()` 由 P0 Task 3 提供（`derived` 和 `human` 返回 true）。若 P0 里叫别的名字，以 P0 为准，不要新造一个。

- [ ] **Step 4: 跑测试**

```bash
go test ./internal/systems/insights/ -run 'TestSimilarity|TestRank' -v
```

Expected: 六个测试全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/systems/insights/similar.go internal/systems/insights/similar_test.go
git commit -m "feat(insights): 按特征重叠算素材相似度"
```

---

### Task 2: 找相似素材的服务与接口

**Files:**
- Modify: `internal/systems/insights/similar.go`（加服务方法）
- Modify: `internal/systems/insights/similar_test.go`（追加）
- Modify: `internal/systems/insights/httpapi/server.go`
- Modify: `api/openapi/insights-v1.yaml`

**Interfaces:**
- Consumes: Task 1 的全部；既有 `AssetRepository.ListAssets` / `ListAssetFeatures` / `GetAsset`。
- Produces:
  - `type SimilarAssetRequest struct{ AssetID string; Features map[string]string; Limit int }`，`Validate()`
  - `type SimilarAssetResult struct{ Probe []SimilarityReason; Items []SimilarAsset; Note string }`
  - `func (Service) FindSimilarAssets(context.Context, contract.ActorContext, contract.ProjectID, SimilarAssetRequest) (SimilarAssetResult, error)`
  - HTTP：`POST /api/insights/v1/projects/{project_id}/assets/similar`

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/similar_test.go` 追加：

```go
func TestSimilarAssetRequestValidation(t *testing.T) {
	t.Parallel()

	byAsset := SimilarAssetRequest{AssetID: "asset_1"}
	if err := byAsset.Validate(); err != nil {
		t.Fatalf("按素材找相似应该合法：%v", err)
	}

	byFeature := SimilarAssetRequest{Features: map[string]string{"duration": "15s"}}
	if err := byFeature.Validate(); err != nil {
		t.Fatalf("按变量找相似应该合法：%v", err)
	}

	// 两个都不给就等于「把库里所有素材列出来」——那是素材列表，不是找相似。
	if err := (SimilarAssetRequest{}).Validate(); err == nil {
		t.Error("既没有素材也没有变量的请求应该被拒")
	}

	// 变量太多会退化成「找那一条素材自己」，没有意义。
	tooMany := SimilarAssetRequest{Features: map[string]string{}}
	for index := 0; index < 21; index++ {
		tooMany.Features[string(rune('a'+index))] = "x"
	}
	if err := tooMany.Validate(); err == nil {
		t.Error("变量超过 20 个应该被拒")
	}
}

// 默认条数要有上限。不限的话一个常见取值能拉回几百条，人在界面上根本挑不过来，
// 而且这批素材是要被拿去重算归因的，几百条会让那次计算变得很慢。
func TestSimilarAssetRequestDefaultsTheLimit(t *testing.T) {
	t.Parallel()

	request := SimilarAssetRequest{AssetID: "asset_1"}
	if got := request.effectiveLimit(); got != defaultSimilarLimit {
		t.Errorf("默认条数应该是 %d，得到 %d", defaultSimilarLimit, got)
	}
	over := SimilarAssetRequest{AssetID: "asset_1", Limit: 500}
	if got := over.effectiveLimit(); got != maxSimilarLimit {
		t.Errorf("超上限应该压到 %d，得到 %d", maxSimilarLimit, got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestSimilarAssetRequest' -v
```

Expected: `undefined: SimilarAssetRequest`。

- [ ] **Step 3: 实现服务**

`internal/systems/insights/similar.go` 追加：

```go
// 检索条数的上下限。不限的话一个常见取值能拉回几百条：界面上挑不过来，
// 而这批素材是要被拿去重算归因的，几百条会让那次计算变得很慢。
const (
	defaultSimilarLimit = 10
	maxSimilarLimit     = 50
	maxProbeFeatures    = 20
)

// SimilarAssetRequest 有两种问法：
//   - 给 AssetID：「和这条素材像的还有哪些」，探针取这条素材自己的变量；
//   - 给 Features：「时长 15 秒的还有哪些」，这是 ❓ 升级通道用的那种。
type SimilarAssetRequest struct {
	AssetID  string            `json:"asset_id,omitempty"`
	Features map[string]string `json:"features,omitempty"`
	Limit    int               `json:"limit,omitempty"`
}

func (r SimilarAssetRequest) Validate() error {
	if r.AssetID == "" && len(r.Features) == 0 {
		return ErrInvalidRequest
	}
	if len(r.Features) > maxProbeFeatures {
		return ErrInvalidRequest
	}
	return nil
}

func (r SimilarAssetRequest) effectiveLimit() int {
	if r.Limit <= 0 {
		return defaultSimilarLimit
	}
	if r.Limit > maxSimilarLimit {
		return maxSimilarLimit
	}
	return r.Limit
}

// SimilarAssetResult 把探针一起返回。人要看得见「系统是按哪几个变量找的」
// ——只给结果的话，找出一批看起来不像的东西时没人知道问题出在哪。
type SimilarAssetResult struct {
	Probe []SimilarityReason `json:"probe"`
	Items []SimilarAsset     `json:"items"`
	Note  string             `json:"note"`
}

func (s Service) FindSimilarAssets(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, request SimilarAssetRequest) (SimilarAssetResult, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return SimilarAssetResult{}, err
	}
	if err := request.Validate(); err != nil {
		return SimilarAssetResult{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return SimilarAssetResult{}, err
	}

	probe := FeatureProbe{}
	for key, value := range request.Features {
		probe[key] = value
	}
	probeReasons := make([]SimilarityReason, 0, len(probe))

	if request.AssetID != "" {
		features, err := s.AssetRepository.ListAssetFeatures(ctx, actor.OrganizationID, projectID,
			[]string{request.AssetID}, 0)
		if err != nil {
			return SimilarAssetResult{}, err
		}
		for _, feature := range features {
			if feature.Value.Text == "" {
				continue
			}
			probe[feature.Key] = feature.Value.Text
			probeReasons = append(probeReasons, SimilarityReason{
				Key: feature.Key, Label: feature.Label, Value: feature.Value.Text, Source: feature.Source,
			})
		}
	}
	if len(probe) == 0 {
		// 这条素材一个变量都没提取过。这不是「没有相似素材」，是「还没法找」
		// ——两者在界面上必须说成不同的话，否则人会以为库里真的没有像的。
		return SimilarAssetResult{
			Probe: probeReasons, Items: []SimilarAsset{},
			Note: "这条素材还没有提取过任何变量，没法按内容找相似。先去「素材 · 变量」把它的变量填上。",
		}, nil
	}

	assets, err := s.AssetRepository.ListAssets(ctx, actor.OrganizationID, projectID,
		AssetFilter{Limit: similarCandidateLimit})
	if err != nil {
		return SimilarAssetResult{}, err
	}
	ids := make([]string, 0, len(assets))
	titles := make(map[string]string, len(assets))
	for _, asset := range assets {
		if asset.ID == request.AssetID {
			continue // 自己永远和自己最像，列出来只是噪音
		}
		ids = append(ids, asset.ID)
		titles[asset.ID] = asset.Title
	}
	if len(ids) == 0 {
		return SimilarAssetResult{Probe: probeReasons, Items: []SimilarAsset{},
			Note: "这个 Project 里还没有别的素材可比。"}, nil
	}

	features, err := s.AssetRepository.ListAssetFeatures(ctx, actor.OrganizationID, projectID, ids, 0)
	if err != nil {
		return SimilarAssetResult{}, err
	}
	byAsset := make(map[string]map[string]AssetFeature, len(ids))
	for _, feature := range features {
		if byAsset[feature.AssetID] == nil {
			byAsset[feature.AssetID] = map[string]AssetFeature{}
		}
		byAsset[feature.AssetID][feature.Key] = feature
	}

	scored := make([]SimilarAsset, 0, len(ids))
	for _, id := range ids {
		item := scoreSimilarity(probe, byAsset[id])
		item.AssetID, item.Title = id, titles[id]
		scored = append(scored, item)
	}
	items := rankSimilar(scored, request.effectiveLimit())

	return SimilarAssetResult{Probe: probeReasons, Items: items, Note: similarNote(items)}, nil
}

// similarCandidateLimit 是一次最多扫多少条素材。在库里素材上万之前，
// 全表扫一遍再在内存里算重叠，比先建一套索引简单得多，也不会算错。
// 超过这个数就该改成先按探针里最稀有的那个变量筛一次——留给那时候的人。
const similarCandidateLimit = 2000

func similarNote(items []SimilarAsset) string {
	if len(items) == 0 {
		return "库里没有在这些变量上和它一致的素材。"
	}
	admissible := 0
	for _, item := range items {
		if item.AdmissibleOverlap > 0 {
			admissible++
		}
	}
	if admissible == 0 {
		// 这批的相似全建立在模型推断的变量上。拉进来能看，但不能拿去做归因
		// ——不说清楚的话，人会把它们当成和人工标注一样可靠的样本。
		return "找到的这些，相似之处全都建立在模型推断的变量上，只能参考，不能拿来做归因。"
	}
	return ""
}
```

> `s.AssetRepository` 的真实字段名以 `assets.go` 里 `Service` 的用法为准（`grep -n "s.AssetRepository\|s.Assets" internal/systems/insights/assets.go | head -3`）。`ListAssetFeatures` 的第四个参数是 limit，传 0 表示不限——先确认它的实际语义（`grep -n "func (r MySQLAssetRepository) ListAssetFeatures" -A 12 internal/systems/insights/mysql_asset_repository.go`），若 0 不是「不限」就传 `similarCandidateLimit * 20`。

- [ ] **Step 4: 挂路由**

`httpapi/server.go`：`Application` 接口加

```go
	FindSimilarAssets(context.Context, contract.ActorContext, contract.ProjectID, insights.SimilarAssetRequest) (insights.SimilarAssetResult, error)
```

`registerAssetRoutes()` 里加

```go
	s.mux.HandleFunc("POST /api/insights/v1/projects/{project_id}/assets/similar", s.findSimilarAssets)
```

handler 照该文件里其他 POST handler 的写法：解 body 到 `insights.SimilarAssetRequest`、取 actor 与 project、调用、`writeJSON` 200。

> 注意路由顺序：`/assets/similar` 必须不被 `/assets/{asset_id}` 吃掉。Go 1.22 的 ServeMux 按精确度匹配，字面量段优先于通配段，所以这里是安全的——但要在 Step 5 里实测一次，别只靠推断。

- [ ] **Step 5: 跑测试并实测路由**

```bash
go build ./... && go test ./internal/systems/insights/...
```

起后端后实测（`{token}` 与 `{project}` 换成本机 seed 出来的值）：

```bash
curl -s -X POST "http://localhost:8080/api/insights/v1/projects/{project}/assets/similar" -H "Authorization: Bearer {token}" -H "Content-Type: application/json" -d '{"features":{"duration":"15s"}}' | head -40
```

Expected: 200，返回体有 `probe` / `items` / `note` 三个键，且不是 `null`。

- [ ] **Step 6: 补 OpenAPI 并提交**

`api/openapi/insights-v1.yaml` 加 `/assets/similar` 接口与 `SimilarAsset` / `SimilarityReason` / `SimilarAssetResult` 三个 schema。描述里写明「两种问法」和「相似理由必须可陈述」这两条。

```bash
git add internal/systems/insights/ api/openapi/insights-v1.yaml
git commit -m "feat(insights): 找相似素材的检索接口"
```

---

### Task 3: 外部素材是证据，不是资产

**Files:**
- Create: `migrations/insights/20260811110000_insight_external_assets.up.sql`
- Create: `internal/systems/insights/external.go`
- Create: `internal/systems/insights/external_test.go`

**Interfaces:**
- Consumes: 既有 `AssetType` / `FeatureValue` / `contract.OrganizationID` / `contract.ProjectID`。
- Produces:
  - `type ExternalAsset struct{...}`（见 Step 3）
  - `type ExternalPurpose string`，取值 `PurposeBenchmark="benchmark"` / `PurposeReference="reference"`；`Label()`；`valid()`
  - `type ImportExternalAssetRequest struct{...}`，`Validate()`
  - `func externalRetentionUntil(windowEnd time.Time) time.Time`
  - `const externalStoragePrefix = "insights/external/"`

- [ ] **Step 1: 写迁移**

创建 `migrations/insights/20260811110000_insight_external_assets.up.sql`：

```sql
-- 外部素材单独一张表，不进 assets。
--
-- 素材库归创意创作那边，他们不接受平台外的素材（2026-08-04 确认）。这不是流程
-- 洁癖：共享素材库里的东西是可以被拿去投放的，而外部素材没有授权，混进去之后
-- 没有任何机制拦住它被投出去。
--
-- 洞察这边仍然需要它——「行业里同类素材长什么样」是解释本轮结果时绕不开的参照。
-- 所以它以**证据**的身份存在：只读、有用途声明、有留存期限、到期删原件。
--
-- retention_until 由导入时那个复盘窗口的结束日期 + 90 天算出来，存下来而不是
-- 每次现算：现算的话改一次常量，所有历史素材的到期日一起变，而人是按导入时
-- 告知的期限做的决定。
CREATE TABLE insight_external_assets (
  id              VARCHAR(64)  NOT NULL,
  organization_id VARCHAR(64)  NOT NULL,
  project_id      VARCHAR(64)  NOT NULL,

  title           VARCHAR(255) NOT NULL,
  -- source_note 是「这东西哪来的」，人自己写。不做成下拉：来源千奇百怪，
  -- 硬套选项只会让人全选「其他」，那一栏就废了。
  source_note     VARCHAR(512) NOT NULL DEFAULT '',
  asset_type      VARCHAR(64)  NOT NULL DEFAULT '',

  -- purpose 是用途声明，导入时必须选。它不是分类标签，是一份记录：
  -- 到了要解释「为什么留着这个」的时候，这一栏就是答案。
  purpose         VARCHAR(32)  NOT NULL,
  purpose_note    VARCHAR(512) NOT NULL DEFAULT '',

  -- storage_key 前缀固定为 insights/external/，和平台素材的存储路径物理隔开。
  -- 同一个前缀下的东西迟早会被某个批处理当成同类对待。
  storage_key     VARCHAR(512) NOT NULL DEFAULT '',
  -- original_purged 为 true 表示原件已删、只剩派生物（变量、截图）。
  -- 到期删的是原件不是整行：删整行的话，引用过它的那份复盘就变成了
  -- 「引用了一个不存在的东西」。
  original_purged TINYINT(1)   NOT NULL DEFAULT 0,

  features        JSON         NOT NULL,
  retention_until DATETIME     NOT NULL,

  created_by      VARCHAR(64)  NOT NULL,
  created_at      DATETIME     NOT NULL,
  updated_at      DATETIME     NOT NULL,

  PRIMARY KEY (id),
  KEY idx_external_project (organization_id, project_id, created_at),
  KEY idx_external_retention (retention_until, original_purged)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

- [ ] **Step 2: 跑迁移**

```bash
go run ./cmd/cookies-migrate
```

Expected: 无错误。

- [ ] **Step 3: 写模型与校验的失败测试**

创建 `internal/systems/insights/external_test.go`：

```go
package insights

import (
	"testing"
	"time"
)

// 留存期 = 复盘窗口结束 + 90 天。
//
// 从窗口结束算而不是从导入日算：这东西是为了解释那一轮投放而收进来的，
// 那一轮的复盘结束了它的用处就到头了。从导入日算的话，一个投放中途导入的素材
// 会比投放结束后导入的多留一个月，而两者的用处是一样的。
func TestExternalRetentionCountsFromTheReviewWindow(t *testing.T) {
	t.Parallel()

	windowEnd := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	got := externalRetentionUntil(windowEnd)
	want := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("留存到期日应该是 %s，得到 %s", want.Format("2006-01-02"), got.Format("2006-01-02"))
	}
}

// 用途声明是必填的。它不是分类标签，是一份记录：到了要解释「为什么留着这个」
// 的时候，这一栏就是答案。留空的话，那个问题只能靠人回忆。
func TestImportExternalAssetRequiresAPurpose(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	valid := ImportExternalAssetRequest{
		Title: "同行的一条 15 秒竖版", Purpose: PurposeBenchmark,
		SourceNote: "公开投放素材，2026-07 抓取", WindowEnd: now,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法请求被拒了：%v", err)
	}

	noPurpose := valid
	noPurpose.Purpose = ""
	if err := noPurpose.Validate(); err == nil {
		t.Error("没有用途声明应该被拒")
	}

	badPurpose := valid
	badPurpose.Purpose = "whatever"
	if err := badPurpose.Validate(); err == nil {
		t.Error("用途只能是列举里的那几个")
	}

	noTitle := valid
	noTitle.Title = "  "
	if err := noTitle.Validate(); err == nil {
		t.Error("没有标题的外部素材应该被拒——列表里全是无题，谁也认不出哪个是哪个")
	}

	noWindow := valid
	noWindow.WindowEnd = time.Time{}
	if err := noWindow.Validate(); err == nil {
		t.Error("没有窗口就算不出留存期限")
	}
}

// 存储前缀必须和平台素材物理隔开。同一个前缀下的东西迟早会被某个批处理
// 当成同类对待——那时候外部素材就跟着平台素材一起进了某个可投放的池子。
func TestExternalStorageKeyIsPrefixed(t *testing.T) {
	t.Parallel()

	key := externalStorageKey("ext_123", "mp4")
	if len(key) < len(externalStoragePrefix) || key[:len(externalStoragePrefix)] != externalStoragePrefix {
		t.Errorf("存储路径必须以 %q 开头，得到 %q", externalStoragePrefix, key)
	}
}
```

- [ ] **Step 4: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestExternal|TestImportExternal' -v
```

Expected: `undefined: externalRetentionUntil` 等。

- [ ] **Step 5: 实现**

创建 `internal/systems/insights/external.go`：

```go
package insights

import (
	"strings"
	"time"

	"github.com/cookies/internal/platform/contract"
)

// 外部素材证据区。
//
// **这里的东西是证据，不是资产。** 素材库归创意创作那边，他们不接受平台外的
// 素材（2026-08-04 确认）——共享素材库里的东西是可以被拿去投放的，而外部素材
// 没有授权，混进去之后没有任何机制拦住它被投出去。
//
// 但洞察这边确实需要它：「行业里同类素材长什么样」是解释本轮结果时绕不开的参照。
// 所以它以证据的身份存在，四条约束一条都不能松：
//   - 单独一张表，永不写进 assets，永不调 IngestRenderedVideo；
//   - 单独一个存储前缀，和平台素材物理隔开；
//   - 只读，改就重新导一份；
//   - 有用途声明、有留存期限，到期删原件只留派生物。

// externalStoragePrefix 是外部素材的存储前缀。固定成常量而不是拼在调用处，
// 是为了让「有没有隔开」这件事能被一个测试盯住。
const externalStoragePrefix = "insights/external/"

// externalRetentionDays 是复盘窗口结束之后再留多久。
//
// 90 天是个待定的数：它应该来自一份合规口径，而不是这里拍出来的。
// 有了口径之后改这个常量即可——历史行的到期日是导入时算好存下的，不会跟着变。
const externalRetentionDays = 90

// ExternalPurpose 是用途声明。它不是分类标签，是一份记录：
// 到了要解释「为什么留着这个」的时候，这一栏就是答案。
type ExternalPurpose string

const (
	// PurposeBenchmark：拿来当参照，回答「同类素材大概什么水平」。
	PurposeBenchmark ExternalPurpose = "benchmark"
	// PurposeReference：拿来当反例或正例，解释本轮某条结论。
	PurposeReference ExternalPurpose = "reference"
)

func (p ExternalPurpose) valid() bool {
	return p == PurposeBenchmark || p == PurposeReference
}

func (p ExternalPurpose) Label() string {
	switch p {
	case PurposeBenchmark:
		return "同类参照"
	case PurposeReference:
		return "解释用例"
	}
	return string(p)
}

// ExternalAsset 是一条外部素材证据。**没有版本、没有血缘、没有状态流转**
// ——那些是资产才需要的东西。它只读：要改就重新导一份，改一份证据等于篡改。
type ExternalAsset struct {
	ID             string                  `json:"id"`
	OrganizationID contract.OrganizationID `json:"organization_id"`
	ProjectID      contract.ProjectID      `json:"project_id"`

	Title      string    `json:"title"`
	SourceNote string    `json:"source_note,omitempty"`
	AssetType  AssetType `json:"asset_type,omitempty"`

	Purpose     ExternalPurpose `json:"purpose"`
	PurposeNote string          `json:"purpose_note,omitempty"`

	StorageKey     string `json:"storage_key,omitempty"`
	OriginalPurged bool   `json:"original_purged"`

	// Features 是人对它标的变量。它们是派生物：到期删原件之后，
	// 这些留着——引用过它的复盘还得说得清当时看到的是什么。
	Features map[string]FeatureValue `json:"features"`

	RetentionUntil time.Time `json:"retention_until"`

	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ImportExternalAssetRequest 是导入的入参。
type ImportExternalAssetRequest struct {
	Title      string    `json:"title"`
	SourceNote string    `json:"source_note"`
	AssetType  AssetType `json:"asset_type,omitempty"`

	Purpose     ExternalPurpose `json:"purpose"`
	PurposeNote string          `json:"purpose_note,omitempty"`

	// WindowEnd 是「这东西是为了解释哪一轮」的那个窗口的结束日。留存期从它算起。
	WindowEnd time.Time `json:"-"`

	Features map[string]string `json:"features,omitempty"`
	// FileExt 只用来拼存储路径，不做校验——校验文件类型是上传那一层的事。
	FileExt string `json:"file_ext,omitempty"`
}

func (r ImportExternalAssetRequest) Validate() error {
	if strings.TrimSpace(r.Title) == "" {
		return ErrInvalidRequest
	}
	if !r.Purpose.valid() {
		return ErrInvalidRequest
	}
	if r.WindowEnd.IsZero() {
		return ErrInvalidRequest
	}
	if len(r.Features) > maxProbeFeatures {
		return ErrInvalidRequest
	}
	return nil
}

// externalRetentionUntil 从复盘窗口结束日算起。
//
// 不从导入日算：这东西是为了解释那一轮投放而收进来的，那一轮的复盘结束了它的
// 用处就到头了。从导入日算的话，投放中途导入的会比投放后导入的多留一个月，
// 而两者的用处一样。
func externalRetentionUntil(windowEnd time.Time) time.Time {
	return windowEnd.AddDate(0, 0, externalRetentionDays)
}

// externalStorageKey 拼存储路径。前缀写死，和平台素材物理隔开。
func externalStorageKey(id, ext string) string {
	if ext == "" {
		return externalStoragePrefix + id
	}
	return externalStoragePrefix + id + "." + strings.TrimPrefix(ext, ".")
}
```

> import 路径 `github.com/cookies/internal/platform/contract` 以仓里其他文件的实际 module path 为准（看 `internal/systems/insights/assets.go` 顶部）。

- [ ] **Step 6: 跑测试并提交**

```bash
go test ./internal/systems/insights/ -run 'TestExternal|TestImportExternal' -v && go build ./...
```

```bash
git add internal/systems/insights/external.go internal/systems/insights/external_test.go migrations/insights/
git commit -m "feat(insights): 外部素材以证据身份单独建表"
```

---

### Task 4: 导入、列出、到期清理外部素材

**Files:**
- Create: `internal/systems/insights/mysql_external_repository.go`
- Modify: `internal/systems/insights/external.go`（服务方法）
- Modify: `internal/systems/insights/external_test.go`（追加）
- Modify: `internal/systems/insights/httpapi/server.go`
- Modify: `api/openapi/insights-v1.yaml`

**Interfaces:**
- Consumes: Task 3 的全部。
- Produces:
  - `type ExternalAssetRepository interface{ CreateExternalAsset; ListExternalAssets; PurgeExpiredOriginals }`
  - `func (Service) ImportExternalAsset(...) (ExternalAsset, error)`
  - `func (Service) ListExternalAssets(...) ([]ExternalAsset, error)`
  - `func (Service) PurgeExpiredExternalOriginals(ctx) (int64, error)`
  - HTTP：`POST .../external-assets`、`GET .../external-assets`

- [ ] **Step 1: 写失败的测试**

在 `internal/systems/insights/external_test.go` 追加：

```go
// 导入的结果必须是只读的形状：没有 Version、没有状态、没有血缘。
// 这条测试盯的是「有没有人后来给它加了资产的字段」——加了就说明有人开始
// 把它当资产用了。
func TestImportedExternalAssetCarriesItsPurposeAndDeadline(t *testing.T) {
	t.Parallel()

	windowEnd := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	request := ImportExternalAssetRequest{
		Title: "同行的一条 15 秒竖版", Purpose: PurposeBenchmark,
		SourceNote: "公开投放素材，2026-07 抓取", WindowEnd: windowEnd,
		Features: map[string]string{"duration": "15s"},
	}
	value := buildExternalAsset("ext_1", request, "user_1",
		time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))

	if value.Purpose != PurposeBenchmark {
		t.Errorf("用途没带上，得到 %q", value.Purpose)
	}
	if !value.RetentionUntil.Equal(externalRetentionUntil(windowEnd)) {
		t.Errorf("留存期限算错了，得到 %s", value.RetentionUntil)
	}
	if value.Features["duration"].Text != "15s" {
		t.Errorf("变量没带上：%+v", value.Features)
	}
	if value.OriginalPurged {
		t.Error("刚导入的原件不该是已删状态")
	}
	if got := value.StorageKey; got[:len(externalStoragePrefix)] != externalStoragePrefix {
		t.Errorf("存储路径没加前缀：%q", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/systems/insights/ -run 'TestImportedExternalAsset' -v
```

Expected: `undefined: buildExternalAsset`。

- [ ] **Step 3: 实现服务**

`internal/systems/insights/external.go` 追加：

```go
// ExternalAssetRepository 单独一个接口，不并进 AssetRepository。
// 并进去的话，下一个实现 AssetRepository 的人会以为外部素材是素材的一种。
type ExternalAssetRepository interface {
	CreateExternalAsset(context.Context, ExternalAsset) (ExternalAsset, error)
	ListExternalAssets(context.Context, contract.OrganizationID, contract.ProjectID, int) ([]ExternalAsset, error)
	// PurgeExpiredOriginals 清掉到期的原件，把行标成 original_purged。
	// 不删整行：删了的话，引用过它的那份复盘就成了「引用了一个不存在的东西」。
	PurgeExpiredOriginals(context.Context, time.Time) ([]string, error)
}

// buildExternalAsset 单独拆出来，让「导入产出什么形状」能被直接测到。
func buildExternalAsset(id string, request ImportExternalAssetRequest,
	actorID string, now time.Time) ExternalAsset {
	features := make(map[string]FeatureValue, len(request.Features))
	for key, value := range request.Features {
		features[key] = FeatureValue{Text: value}
	}
	return ExternalAsset{
		ID: id, Title: strings.TrimSpace(request.Title),
		SourceNote: strings.TrimSpace(request.SourceNote), AssetType: request.AssetType,
		Purpose: request.Purpose, PurposeNote: strings.TrimSpace(request.PurposeNote),
		StorageKey: externalStorageKey(id, request.FileExt),
		Features:   features,
		RetentionUntil: externalRetentionUntil(request.WindowEnd),
		CreatedBy:      actorID, CreatedAt: now, UpdatedAt: now,
	}
}

func (s Service) ImportExternalAsset(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, request ImportExternalAssetRequest) (ExternalAsset, error) {
	if err := s.ready(actor, projectID, ScopeWrite); err != nil {
		return ExternalAsset{}, err
	}
	if err := request.Validate(); err != nil {
		return ExternalAsset{}, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return ExternalAsset{}, err
	}
	id, err := s.idGenerator()("externalasset")
	if err != nil {
		return ExternalAsset{}, err
	}
	value := buildExternalAsset(id, request, actor.Principal.ID, s.now())
	value.OrganizationID, value.ProjectID = actor.OrganizationID, projectID
	return s.ExternalAssets.CreateExternalAsset(ctx, value)
}

func (s Service) ListExternalAssets(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, limit int) ([]ExternalAsset, error) {
	if err := s.ready(actor, projectID, ScopeRead); err != nil {
		return nil, err
	}
	if _, err := s.Projects.RequireActiveContext(ctx, actor, projectID); err != nil {
		return nil, err
	}
	return s.ExternalAssets.ListExternalAssets(ctx, actor.OrganizationID, projectID, normalizeLimit(limit))
}

// PurgeExpiredExternalOriginals 由维护命令调用。返回被清掉原件的存储路径，
// 让调用方去对象存储上删对应的文件——这里只管数据库那一半。
func (s Service) PurgeExpiredExternalOriginals(ctx context.Context) ([]string, error) {
	return s.ExternalAssets.PurgeExpiredOriginals(ctx, s.now())
}
```

`Service` 结构体加一个字段 `ExternalAssets ExternalAssetRepository`，并在装配处（`grep -rn "insights.Service{" cmd/ internal/ --include=*.go`）传进去。

- [ ] **Step 4: 实现仓储**

创建 `internal/systems/insights/mysql_external_repository.go`。三个方法：`CreateExternalAsset`（INSERT，features 走 `json.Marshal`）、`ListExternalAssets`（按 `created_at DESC`）、`PurgeExpiredOriginals`（先 SELECT 出到期未清的 `id, storage_key`，再 UPDATE 置 `original_purged=1, storage_key=''`，返回收集到的 storage_key）。照 `mysql_asset_repository.go` 的风格写，列名与 Task 3 的迁移逐字对齐。

> `PurgeExpiredOriginals` 先查再改，两步之间要在一个事务里——不然并发跑两次会返回两批相同的 storage_key，对象存储那边删第二次会报不存在。

- [ ] **Step 5: 挂路由**

`Application` 接口加 `ImportExternalAsset` 与 `ListExternalAssets`；`registerAssetRoutes()` 加：

```go
	s.mux.HandleFunc("POST /api/insights/v1/projects/{project_id}/external-assets", s.importExternalAsset)
	s.mux.HandleFunc("GET /api/insights/v1/projects/{project_id}/external-assets", s.listExternalAssets)
```

`importExternalAsset` 的 body 要额外收 `window_end`（日期串），解析后填进 `request.WindowEnd`——它在结构体上是 `json:"-"`，因为它不该被当成一个普通字段随便传，必须走和其他窗口一样的日期解析。

- [ ] **Step 6: 跑测试、补 OpenAPI、提交**

```bash
go build ./... && go test ./internal/systems/insights/...
```

OpenAPI 加两个接口与 `ExternalAsset` schema，描述里写明「外部素材永不进共享素材库」和「到期删原件保留派生物」。

```bash
git add internal/systems/insights/ api/openapi/insights-v1.yaml
git commit -m "feat(insights): 外部素材的导入、列出与到期清理"
```

---

### Task 5: 素材入口的五个视图

**Files:**
- Create: `src/components/insight/assets/AssetsPage.tsx`
- Create: `src/components/insight/assets/OverviewView.tsx`
- Create: `src/components/insight/assets/IntakeView.tsx`
- Create: `src/components/insight/assets/FeatureView.tsx`
- Create: `src/components/insight/assets/SimilarView.tsx`
- Create: `src/components/insight/assets/ExternalView.tsx`
- Create: `src/components/insight/assets/index.ts`
- Modify: `src/data/api.ts`
- Reference: `src/components/AssetLibraryPage.tsx`、`src/components/ContentAnalysisPage.tsx`

**Interfaces:**
- Consumes: Task 2、Task 4 的接口。
- Produces:
  - `export function AssetsPage({ view }: { view: AssetsView })`
  - `export type AssetsView = 'index' | 'features' | 'similar' | 'external'`

- [ ] **Step 1: 加 api 方法**

`src/data/api.ts`：

```ts
  findSimilarAssets: (projectId: string, body: {
    asset_id?: string
    features?: Record<string, string>
    limit?: number
  }) => request<ApiSimilarAssetResult>(`${insightProjectPath(projectId)}/assets/similar`, 'POST', body),

  importExternalAsset: (projectId: string, body: {
    title: string
    source_note: string
    purpose: 'benchmark' | 'reference'
    purpose_note?: string
    asset_type?: string
    window_end: string
    features?: Record<string, string>
  }) => request<ApiExternalAsset>(`${insightProjectPath(projectId)}/external-assets`, 'POST', body),

  listExternalAssets: (projectId: string, limit = 50) =>
    request<ApiExternalAsset[]>(`${insightProjectPath(projectId)}/external-assets?limit=${limit}`),
```

配套类型：

```ts
export interface ApiSimilarityReason {
  key: string
  label: string
  value: string
  source: 'derived' | 'human' | 'ai'
}

export interface ApiSimilarAsset {
  asset_id: string
  title: string
  overlap: number
  admissible_overlap: number
  score: number
  reasons: ApiSimilarityReason[]
}

export interface ApiSimilarAssetResult {
  probe: ApiSimilarityReason[]
  items: ApiSimilarAsset[]
  note: string
}

export interface ApiExternalAsset {
  id: string
  title: string
  source_note?: string
  asset_type?: string
  purpose: 'benchmark' | 'reference'
  purpose_note?: string
  original_purged: boolean
  features: Record<string, { text?: string }>
  retention_until: string
  created_at: string
}
```

- [ ] **Step 2: 写 SimilarView**

创建 `src/components/insight/assets/SimilarView.tsx`：

```tsx
import { useState } from 'react'
import { Search } from 'lucide-react'
import { api, type ApiSimilarAssetResult } from '../../../data/api'
import { useProject } from '../../../context/ProjectContext'
import { SimilarPanel } from './SimilarPanel'

/**
 * 「找相似」视图。
 *
 * 两种问法都从这里进：给一条素材问「和它像的还有哪些」，或者直接给一组变量问
 * 「时长 15 秒的还有哪些」。后一种是 ❓「算不出来」的升级通道——本轮样本不够时，
 * 从库里把同样取值的素材拉过来，样本就厚了。
 */
export function SimilarView() {
  const { currentProject } = useProject()
  const [assetId, setAssetId] = useState('')
  const [result, setResult] = useState<ApiSimilarAssetResult | null>(null)
  const [state, setState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle')

  const search = () => {
    if (!currentProject.id || !assetId.trim()) return
    setState('loading')
    api.findSimilarAssets(currentProject.id, { asset_id: assetId.trim() })
      .then(value => { setResult(value); setState('ready') })
      .catch(() => { setResult(null); setState('error') })
  }

  return <section className="similar-view">
    <p className="prelaunch-disclosure">
      按变量重叠找，不按整体相似度。所以每条结果都能说清「像在哪」——
      拿这批素材去把样本做厚的时候，你得说得出它们为什么能凑成一组。
    </p>
    <div className="similar-search">
      <input value={assetId} onChange={event => setAssetId(event.target.value)}
        placeholder="素材 ID" aria-label="素材 ID"/>
      <button type="button" className="secondary-button" onClick={search}
        disabled={state === 'loading' || !assetId.trim()}>
        <Search size={15}/>{state === 'loading' ? '正在找…' : '找相似'}
      </button>
    </div>
    {state === 'error' ? <p className="form-error">没找成，稍后再试。</p> : null}
    {result ? <SimilarPanel result={result}/> : null}
  </section>
}
```

- [ ] **Step 3: 写 SimilarPanel**

创建 `src/components/insight/assets/SimilarPanel.tsx`——它同时被「找相似」视图和分析页的 `NotEnoughSample` 用，所以单独一个文件：

```tsx
import type { ApiSimilarAssetResult, ApiSimilarityReason } from '../../../data/api'

const sourceLabels: Record<ApiSimilarityReason['source'], string> = {
  derived: '量出来的',
  human: '人标的',
  ai: '模型猜的',
}

/**
 * 相似素材结果。每条都列出「像在哪」——这是这个功能和一个相似度分数的全部区别。
 */
export function SimilarPanel({ result }: { result: ApiSimilarAssetResult }) {
  return <div className="similar-panel">
    {result.probe.length ? <p className="similar-probe">
      按这些变量找的：{result.probe.map(item => `${item.label}=${item.value}`).join('、')}
    </p> : null}

    {result.note ? <p className="similar-note">{result.note}</p> : null}

    {result.items.length ? <ul className="similar-list">
      {result.items.map(item => <li key={item.asset_id}>
        <div className="similar-head">
          <strong>{item.title || item.asset_id}</strong>
          <span className="similar-count">重叠 {item.overlap} 个变量
            {item.admissible_overlap < item.overlap
              ? `（其中 ${item.admissible_overlap} 个能进归因）` : ''}</span>
        </div>
        <ul className="similar-reasons">
          {item.reasons.map(reason => <li key={reason.key}>
            {reason.label} = {reason.value}
            <small>（{sourceLabels[reason.source]}）</small>
          </li>)}
        </ul>
      </li>)}
    </ul> : <p className="panel-empty">没有在这些变量上一致的素材。</p>}
  </div>
}
```

- [ ] **Step 4: 写 ExternalView**

创建 `src/components/insight/assets/ExternalView.tsx`。列表 + 导入表单。三条必须出现在界面上的文案：

```tsx
    <p className="prelaunch-disclosure">
      这里的素材是**证据**，不是资产。它们不会进共享素材库、不能被拿去投放，
      也不能改——要改就重新导一份。收它们只有一个用处：解释本轮结果时有个参照。
    </p>
```

每一行要显示留存到期日和用途：

```tsx
      <span className="external-retention">
        留到 {formatDate(item.retention_until)}
        {item.original_purged ? '（原件已删，只剩标注的变量）' : ''}
      </span>
```

导入表单里用途声明是必选的单选，两个选项：`benchmark` 同类参照 / `reference` 解释用例。表单下方固定一句：

```tsx
      <small className="form-hint">
        用途要选，因为留存期到了之后，「当初为什么收这个」只有这一栏说得清。
      </small>
```

`window_end` 取当前复盘窗口的结束日。表单上不给人手填的口子——它决定留存期限，填错了这条素材会比该留的时间多留或少留。从页面当前窗口取，和分析页同一套。

- [ ] **Step 5: 写 OverviewView、IntakeView、FeatureView、AssetDetail 与壳**

- `OverviewView.tsx`：**左右两栏，左边平台内、右边外部。**两栏不只是分类，是两种所有权——左栏的素材躺在素材库里，洞察一个字节都没存，所以左栏顶上是一个「← 创意模块 · 素材库」的跳转；右栏的文件是洞察自己存的，所以只有右栏有「原片到期」这一行。到期提示必须放在这里，为的是让人在原片被清掉之前还有机会做完想做的分析。两栏下面汇合成一个数：「可分析素材 N 条 → 进分析 / 复盘」。

  三个队列（对不上号、待提取变量、提取失败）从 `AssetLibraryPage.tsx` 的「待匹配」「待提取」两段搬过来，**对不上号的红色置顶**——它是唯一一个「不处理后面全错」的问题，其余两个只是少几条样本。
- `IntakeView.tsx`：从 `DataConnectionsPage.tsx` 整页搬（数据源、导入任务、字段映射、素材映射、同步记录）。它进「素材」而不是留在治理里，理由是：一条素材要能进复盘必须齐三样——对上号、内容变量、投放数据，而数据接入管的正是第三样。把它放在治理里，等于让备料这件事分两个入口做。五个原视图在这里降级成小标题分段。
- `AssetDetail.tsx`：单条素材。**上半截和下半截视觉上分开**：上半截（预览、时长、版本、血缘）读素材库，右上角一个「去素材库」；下半截（内容变量、投放数据、分析状态）才是洞察自己的。分开是为了让人知道哪些东西改了要去别处改——混在一屏里，人会在这里找编辑时长的按钮然后找不到。

  > 上半截现在**没有数据可读**：洞察侧只存了 `PlatformAssetID` / `PlatformAssetVersion` 两个裸字段，没有真链接。本期上半截显示这两个号码和「去素材库」的跳转，并明写「预览与版本信息在素材库那边」。**不要为了让它好看而在洞察这边复制一份素材元数据**——复制出来的那份第二天就和素材库对不上了。真接上是创意侧交接时的事。
- `FeatureView.tsx`：从 `AssetLibraryPage.tsx` 的「特征」和 `ContentAnalysisPage.tsx` 整页搬。内容分析原来按素材类型分了七个视图（小红书、公众号、品牌广告、数字人、广告前贴、爆款复刻、单素材拆解），在这里改成一个类型下拉 + 一张变量表——七个视图的差别只是特征系统不同，做成七个入口等于把一个筛选条件提升成了七个页面。**每个变量必须显示它的来源**（量出来的 / 人标的 / 模型猜的），这是 P0 定的准入规则在这一页的落点。
- `AssetsPage.tsx`：壳，按 `view` 分发到五个视图，顶部是项目与窗口；点任意一条素材打开 `AssetDetail`。参照 P1 的 `AnalysisPage` 写法。
- `index.ts`：`export { AssetsPage, type AssetsView } from './AssetsPage'`

`AssetsView` 的取值：`'overview' | 'intake' | 'features' | 'similar' | 'external'`。

- [ ] **Step 6: 构建并在浏览器里走一遍**

```bash
npm run build
```

用 `preview_start` 起服务，确认：

1. 「素材 · 找相似」输入一个 seed 出来的素材 ID，能返回结果，每条都列出了「像在哪」。
2. 结果里如果有只靠模型推断变量的，页面上出现那句「只能参考，不能拿来做归因」。
3. 「素材 · 变量」切换类型下拉，变量表跟着换，每个变量都标了来源。
4. 「素材 · 外部素材」能导入一条，列表里显示用途和留存到期日。
5. 「素材 · 总览」两栏都在，左栏有「← 创意模块 · 素材库」跳转、右栏有到期提示，对不上号的队列红色置顶。
6. 点开一条素材，上下两截视觉上分开，上半截明写了「预览与版本信息在素材库那边」。
7. 「素材 · 数据接入」五段都在，原数据接入页的功能没丢。
8. `preview_console_logs` 无报错。

- [ ] **Step 7: 提交**

```bash
git add src/components/insight/assets/ src/data/api.ts src/styles.css
git commit -m "feat(insights-web): 素材入口的四个视图"
```

---

### Task 6: 把「找相似素材」接回 ❓ 的升级通道

**Files:**
- Modify: `src/components/insight/shared/NotEnoughSample.tsx`
- Modify: `src/components/insight/analysis/*.tsx`（传 `onFindSimilar`）

**Interfaces:**
- Consumes: Task 5 的 `SimilarPanel`；Task 2 的接口。
- Produces: 无新导出。

- [ ] **Step 1: 让 NotEnoughSample 能展开相似结果**

P0 里 `NotEnoughSample` 的「找相似素材」按钮是不可点的（`onFindSimilar` 没人传）。改成：接到 `onFindSimilar` 就可点，点了在原地展开 `SimilarPanel`，不跳页——跳走的话人就丢了刚才在看的那条结论。

```tsx
  const [result, setResult] = useState<ApiSimilarAssetResult | null>(null)
  ...
  {onFindSimilar ? <button type="button" className="text-button"
    onClick={() => { void onFindSimilar().then(setResult) }}>
    找相似素材把样本做厚
  </button> : null}
  {result ? <SimilarPanel result={result}/> : null}
```

- [ ] **Step 2: 六个视图传进去**

分析页每个视图在渲染 `NotEnoughSample` 的地方，传一个按当前变量取值检索的函数。以驱动因素为例：

```tsx
  onFindSimilar={() => api.findSimilarAssets(projectId, {
    features: { [item.key]: item.value },
  })}
```

素材对比传 `changed_features` 里全部**可归因**的变量（`diff.admissible` 为真的那些）——把模型推断的也塞进去，找回来的一批就是靠一个没人核过的推断凑起来的。

趋势、疲劳、异常这三个视图**不给**「找相似素材」：它们说的是某一条素材随时间的变化，把别的素材拉进来不会让这条素材的历史变长。这三处 `NotEnoughSample` 不传 `onFindSimilar`，按钮保持不出现。

- [ ] **Step 3: 在浏览器里确认**

找一条 ❓ 的驱动因素，点「找相似素材把样本做厚」，确认原地展开、结论那一行还在、结果里列出了「像在哪」。切到「趋势」确认那里没有这个按钮。

- [ ] **Step 4: 提交**

```bash
git add src/components/insight/
git commit -m "feat(insights-web): 算不出来时可就地找相似素材"
```

---

### Task 7: 导航从「数据接入 + 分析素材库 + 内容分析」收敛成「素材」

**Files:**
- Modify: `src/data/navigation.ts:49,50,53`
- Modify: 渲染分发处（`src/components/Pages.tsx:1501` 附近）

> **这一步修改现有导航结构。开始前必须向使用者确认。**

- [ ] **Step 1: 改导航条目**

把 `connections`、`assets`、`content` 三条替换为一条：

```ts
      // 「素材」= 原数据接入 + 原分析素材库 + 原内容分析。
      //
      // 合并的理由是这三件事服务同一个目的：给复盘备料。一条素材要能进复盘必须
      // 齐三样——对上号、内容变量、投放数据。数据接入管第三样，素材库管第一样，
      // 内容分析管第二样。分成三个入口，等于让人为了备齐一条素材跑三个地方，
      // 而且哪一样没齐还得自己去对。
      //
      // 内容分析原来按素材类型分了七个视图，那七个的差别只是特征系统不同——
      // 做成七个入口，等于把一个筛选条件提升成了七个页面。
      //
      // 素材库本身归创意创作那边，这里维护的是**分析索引**。
      {
        id: 'assets', label: '素材', icon: Library, group: '工作', layout: 'table',
        description: '能拿来分析的素材有哪些、还差什么、样本不够时从哪里补。',
        views: ['总览', '数据接入', '变量', '找相似', '外部素材'],
      },
```

`Film`、`Database` 图标若不再被引用，从 import 里删掉。

- [ ] **Step 2: 改渲染分发**

```bash
grep -rn "'content'\|'connections'" src/components/ src/App.tsx | grep -v api.ts
```

```ts
const assetsViews: Record<string, AssetsView> = {
  总览: 'overview',
  数据接入: 'intake',
  变量: 'features',
  找相似: 'similar',
  外部素材: 'external',
}
```

- [ ] **Step 3: 跑全量**

```bash
go test ./internal/systems/insights/... && npm run test && npm run build
```

用 `preview_snapshot` 核对侧栏：洞察下面是「分析」「复盘」「素材」，原「数据接入」「分析素材库」「内容分析」都不在了。逐一点开「素材」的五个视图，确认三个旧页面的功能一个没丢。

- [ ] **Step 4: 提交**

```bash
git add src/data/navigation.ts src/components/
git commit -m "feat(insights-web): 数据接入、分析素材库与内容分析收敛成「素材」入口"
```

---

## 自查

**1. 规格覆盖** —— 对照设计文档「模块三 · 素材」与第 3 期：

| 规格要求 | 落在 |
|---|---|
| 相似素材按特征重叠，不用向量 | Task 1（全部实现在 `similar.go`，无新依赖） |
| 相似理由必须可陈述 | Task 1 的 `Reasons` + Task 5 Step 3 的 `SimilarPanel` |
| 相似检索是 ❓ 的升级通道 | Task 6 |
| 归因只认 derived + human | Task 1 的 `AdmissibleOverlap` + Task 5 Step 5 的来源标注 + Task 6 Step 2 的 `admissible` 过滤 |
| 外部素材是证据不是资产 | Task 3 的独立表 + Global Constraints 的禁令 |
| 独立存储前缀 | Task 3 的 `externalStoragePrefix` + 一条专门盯它的测试 |
| 只读 | Task 3（没有任何 Update 方法）+ Task 5 Step 4 的界面文案 |
| 用途声明 | Task 3 的 `ExternalPurpose` 必填 + Task 5 Step 4 的必选单选 |
| 留存 = 窗口结束 + 90 天 | Task 3 的 `externalRetentionUntil` + 一条解释「为什么不从导入日算」的测试 |
| 到期删原件留派生物 | Task 4 的 `PurgeExpiredOriginals`（UPDATE 不 DELETE） |
| 素材库归创意，洞察只管索引 | 全程不 import `internal/platform/assets`；`platform_asset_id` 仍是裸字段 |
| 两栏是两种所有权，不只是分类 | Task 5 Step 5 的 `OverviewView`（左栏跳素材库、右栏才有到期提示） |
| 单条素材上下两截视觉分开 | Task 5 Step 5 的 `AssetDetail` |
| 对不上号红色置顶，该素材不进复盘 | Task 5 Step 5 的三个队列 |
| 数据接入 + 分析素材库 + 内容分析 → 素材 | Task 7 |

**未覆盖且是有意的：**

- **`derived` 变量的产出**（从视频文件里量出时长、分辨率、场景数）—— 视频探测是第 7 期。本期 `SourceDerived` 只有消费方，没有生产方，所以现阶段 `AdmissibleOverlap` 实际上只由 `human` 贡献。这不影响正确性，但会让「其中 N 个能进归因」这句话在有 `derived` 之前偏保守。
- **外部素材的文件上传本身** —— `storage_key` 拼出来了，但上传走的是平台既有的上传通道，不在洞察这边实现。Task 4 只管数据库那一半，对象存储的删除由调用方按返回的 storage_key 去做。
- **外部素材参与相似检索** —— 本期检索只扫 `assets`。让证据和资产在同一个结果列表里出现，人会分不清哪条能拿去投。等外部素材真的被用起来之后再单独设计。
- **90 天这个数** —— 它应该来自一份合规口径。常量和注释都写明了是待定的，改常量即可，历史行不受影响。
- **单条素材上半截的真实数据** —— 洞察侧只有 `PlatformAssetID` / `PlatformAssetVersion` 两个号码，没有真链接。本期显示号码加跳转，不复制素材元数据过来；真接上是创意侧交接时的事。Task 5 Step 5 里写明了为什么不能为了好看而复制。

**2. 占位扫描** —— 无 TBD / TODO / 「同 Task N」。Task 4 Step 4 的仓储实现给的是三个方法各自的 SQL 语义与事务要求而非代码，因为列名必须和 Task 3 的迁移逐字对齐、风格必须照 `mysql_asset_repository.go`——这两个约束写在步骤里比抄一段可能对不上的代码更可靠。Task 5 Step 5 同理：搬运任务给的是「搬什么、合并成什么形状、为什么」，代码在源文件里。

**3. 类型一致性** —— `FeatureSource` 的三个取值在 Go（`derived`/`human`/`ai`）、`ApiSimilarityReason['source']`、`sourceLabels` 三处一致。`ExternalPurpose` 的 `benchmark`/`reference` 在 Go、迁移的注释、`api.ts`、OpenAPI 四处一致。`AssetsView` 的四个取值与 `assetsViews` 映射表的四个键一致。`scoreSimilarity` 只填分数不填 `AssetID`/`Title`，由调用方补——这一点在 Interfaces 块里写明了，避免执行者以为它会自己带上素材信息。

---

## 依赖关系

前置：P0 全部（尤其 Task 3 的 `SourceDerived` 与 `admissible()`，Task 5 的 `NotEnoughSample`）。
与 P1 的关系：Task 6 要改 P1 建的分析视图文件，所以 **P1 必须先完成**。
后继：P4 经验会引用外部素材作为经验的旁证；第 7 期视频探测会给 `SourceDerived` 补上生产方。
