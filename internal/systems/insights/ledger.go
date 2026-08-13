package insights

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// RecordLedgerAssetRequest 是后台收录一条台账素材要的全部东西。
//
// 它和 IndexAssetRequest 长得像，但来路完全不同：那一条是人在界面上手工登记，
// 走 HTTP、过权限门、能填广告形态；这一条是素材库那边刚落库成功后回调进来的，
// 没有 HTTP 请求、没有登录会话，只有一个「是谁的动作触发的」的 ActorID。
type RecordLedgerAssetRequest struct {
	OrganizationID contract.OrganizationID
	ProjectID      contract.ProjectID
	// ActorID 是触发这次入库的人。台账要能回答「这条是谁弄进来的」，
	// 而收录发生在请求线程之外，拿不到完整的 ActorContext。
	ActorID string

	Title       string
	SourceKind  AssetSourceKind
	SourceRef   string
	SourceJobID string

	PlatformAssetID      string
	PlatformAssetVersion int64
}

func (r RecordLedgerAssetRequest) validate() error {
	if strings.TrimSpace(string(r.OrganizationID)) == "" || strings.TrimSpace(string(r.ProjectID)) == "" {
		return fmt.Errorf("%w: 收录台账素材必须带组织与项目", ErrInvalidRequest)
	}
	title := strings.TrimSpace(r.Title)
	if title == "" || len(title) > 255 {
		return fmt.Errorf("%w: 台账素材标题为空或超长", ErrInvalidRequest)
	}
	if !r.SourceKind.valid() {
		return fmt.Errorf("%w: 未知的素材来源 %q", ErrInvalidRequest, string(r.SourceKind))
	}
	if len(r.SourceRef) > 512 {
		return fmt.Errorf("%w: 来源引用超长", ErrInvalidRequest)
	}
	if (r.PlatformAssetID == "") != (r.PlatformAssetVersion == 0) {
		return fmt.Errorf("%w: 媒体资产引用要么同时给出 ID 与版本，要么都不给", ErrInvalidRequest)
	}
	return nil
}

// RecordLedgerAsset 把平台素材库刚入库的一个素材版本记进台账。
//
// 它**不过人的权限门**：调用它的不是人，是素材库落库成功后的回调。放权限门在这里
// 只会让所有后台入库路径都需要伪造一个带 insights.write 的身份，那才是真的没有门。
// 真正的门在写这一侧的唯一入口上——只有 internal/integrations/insightsledger 能调它。
//
// 它也**不判广告形态**：AssetType 是六种广告形态之一，从平台的 image/video 推不出来。
// 台账素材一律留空，等人把它「拉进分析」时再识别。
func (s Service) RecordLedgerAsset(ctx context.Context, request RecordLedgerAssetRequest) (Asset, error) {
	if s.Assets == nil {
		return Asset{}, fmt.Errorf("insight asset repository is not configured")
	}
	if err := request.validate(); err != nil {
		return Asset{}, err
	}
	id, err := s.idGenerator()("insightasset")
	if err != nil {
		return Asset{}, err
	}
	now := s.now()
	createdBy := strings.TrimSpace(request.ActorID)
	if createdBy == "" {
		createdBy = "system"
	}
	return s.Assets.CreateAsset(ctx, Asset{
		ID: id, OrganizationID: request.OrganizationID, ProjectID: request.ProjectID,
		Role: AssetRoleLedger,
		// 台账素材各自成一条血缘。血缘说的是「同一条创意改了几版」，
		// 那是分析对象才需要梳的关系；台账只管有没有这个东西。
		LineageID: id, Revision: 1,
		Title: strings.TrimSpace(request.Title), SourceKind: request.SourceKind,
		SourceRef: request.SourceRef, SourceJobID: request.SourceJobID,
		PlatformAssetID: request.PlatformAssetID, PlatformAssetVersion: request.PlatformAssetVersion,
		AssetType:      AssetTypeUnknown,
		AnalysisStatus: AnalysisAwaitingData,
		// 状态原因写清楚它为什么停在这儿，免得有人在库里看到 awaiting_data 就去补数据。
		AnalysisStatusReason:    "台账素材，未进入分析。",
		AnalysisStatusChangedAt: &now,
		Version:                 1, CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now,
	})
}

// BackfillLedger 把平台素材库里已有、台账里没有的素材版本补进来。
//
// 一次跑一批，跑完回报补了多少条。跑到回报 0 就是补完了——
// 这个命令可以反复跑，重复的那些会被 uq_insight_assets_ledger_object 挡在库里。
func (s Service) BackfillLedger(ctx context.Context, batch int) (int, error) {
	if s.Assets == nil {
		return 0, fmt.Errorf("insight asset repository is not configured")
	}
	if batch < 1 || batch > 5000 {
		return 0, fmt.Errorf("%w: 单批数量应在 1..5000 之间", ErrInvalidRequest)
	}
	pending, err := s.Assets.ListUnledgeredPlatformAssets(ctx, batch)
	if err != nil {
		return 0, err
	}
	recorded := 0
	seen := make(map[string]bool, len(pending))
	for _, item := range pending {
		kind := backfillSourceKind(item.SourceType)
		if kind == "" {
			continue
		}
		// 同一个素材版本可能挂在多个项目下。台账的唯一键是组织级的，
		// 所以它只能进一个项目——先来的那个。批内先挡掉，省得让库去报错。
		key := string(item.OrganizationID) + "|" + item.AssetID + "|" + fmt.Sprint(item.Version)
		if seen[key] {
			continue
		}
		seen[key] = true
		_, recordErr := s.RecordLedgerAsset(ctx, RecordLedgerAssetRequest{
			OrganizationID: item.OrganizationID, ProjectID: item.ProjectID,
			// 不传 ActorID：素材库没有记「这个版本是谁传的」，
			// 回填也就无从知道。落成 system，比编一个人名诚实。
			//
			// 回填也拿不到当初的文件名——那躺在上传会话里，早过期清掉了。
			// 用入库日期，至少说得清它是哪天进来的。
			Title:           backfillTitle(item.SourceType, item.CreatedAt),
			SourceKind:      kind,
			PlatformAssetID: item.AssetID, PlatformAssetVersion: item.Version,
		})
		if recordErr != nil {
			// 一条补不上不该让整批停下：唯一键撞了、项目已归档，都可能。
			// 报出来让人看见，接着补下一条。
			log.Printf("回填台账失败 asset=%s version=%d: %v", item.AssetID, item.Version, recordErr)
			continue
		}
		recorded++
	}
	return recorded, nil
}

func backfillSourceKind(sourceType string) AssetSourceKind {
	switch sourceType {
	case "upload":
		return AssetSourceUpload
	case "rendered", "provider_generated":
		return AssetSourceCreative
	case "imported", "captured":
		return AssetSourceExternal
	}
	return ""
}

func backfillTitle(sourceType string, at time.Time) string {
	date := at.Format("2006-01-02")
	switch sourceType {
	case "rendered":
		return fmt.Sprintf("渲染成片 · %s", date)
	case "provider_generated":
		return fmt.Sprintf("模型产物 · %s", date)
	case "imported":
		return fmt.Sprintf("外部导入 · %s", date)
	case "captured":
		return fmt.Sprintf("采集素材 · %s", date)
	}
	return fmt.Sprintf("未命名素材 · %s", date)
}
