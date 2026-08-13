package insights

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
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
//
// **没有文件就不给路径**。以前不带扩展名时也会拼出一条 external/xxx 的路径，于是
// 一条只填了标题和用途、根本没有任何文件的登记，在库里看起来和「存了原片」一模一样
// ——界面据此对它说「原片将在 X 前后清掉」，而那个原片从来不存在。空字符串是这里
// 唯一诚实的值：清理任务本来就跳过空 storage_key。
func externalStorageKey(id, ext string) string {
	if strings.TrimSpace(ext) == "" {
		return ""
	}
	return externalStoragePrefix + id + "." + strings.TrimPrefix(strings.TrimSpace(ext), ".")
}

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
		// 一律按自由文本存。外部素材是证据，它的变量既不参与归因也不参与
		// 相似度检索——给它一个能被比较的 kind，只会诱使后来的人拿它去比。
		features[key] = FeatureValue{Kind: FeatureKindText, Text: value}
	}
	return ExternalAsset{
		ID: id, Title: strings.TrimSpace(request.Title),
		SourceNote: strings.TrimSpace(request.SourceNote), AssetType: request.AssetType,
		Purpose: request.Purpose, PurposeNote: strings.TrimSpace(request.PurposeNote),
		StorageKey:     externalStorageKey(id, request.FileExt),
		Features:       features,
		RetentionUntil: externalRetentionUntil(request.WindowEnd),
		CreatedBy:      actorID, CreatedAt: now, UpdatedAt: now,
	}
}

// externalReady 照 assetsReady 的样子写，盯的是外部素材那一路依赖。
// 不复用 ready()：它查的是 Repository / Delivery，那两个装配齐了也不代表
// ExternalAssets 装了——没装就走下去，第一次调用直接空接口 panic。
func (s Service) externalReady(actor contract.ActorContext, projectID contract.ProjectID, scope contract.Scope) error {
	if s.ExternalAssets == nil || s.Projects == nil {
		return fmt.Errorf("insights external asset dependencies are incomplete")
	}
	if actor.OrganizationID == "" || projectID == "" || !actor.HasScope(scope) {
		return fmt.Errorf("%s scope is required", scope)
	}
	return nil
}

func (s Service) ImportExternalAsset(ctx context.Context, actor contract.ActorContext,
	projectID contract.ProjectID, request ImportExternalAssetRequest) (ExternalAsset, error) {
	if err := s.externalReady(actor, projectID, ScopeWrite); err != nil {
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
	if err := s.externalReady(actor, projectID, ScopeRead); err != nil {
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
	if s.ExternalAssets == nil {
		return nil, fmt.Errorf("insights external asset dependencies are incomplete")
	}
	return s.ExternalAssets.PurgeExpiredOriginals(ctx, s.now())
}
