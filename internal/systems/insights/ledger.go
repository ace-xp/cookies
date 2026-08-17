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
		if !LedgerAcceptsKind(item.Kind) {
			continue
		}
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
			Title:           backfillTitle(item.ObjectKey, item.SourceType, item.CreatedAt),
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

// ledgerRejectedKinds 是台账不收的那几种，一次性订正命令按它清库。
var ledgerRejectedKinds = []string{"document", "text"}

// PruneLedgerDocuments 把台账里早先收进来的文档清掉。
//
// 台账刚建起来的时候是照单全收的，回填命令把平台素材库里的策略、简报、洞察报告
// 一并收了进来——它们投不出去，却在台账里各占一行，每行右边还挂着一个
// 「拉进分析」。这个命令做一次性订正，跑完台账里就只剩投得出去的东西。
//
// 只删还躺在台账里的（role = ledger）。已经被人拉进分析的那些不碰：
// 有人为它做过判断，还可能已经挂上了变量和映射，删了就是把人的活儿抹掉。
func (s Service) PruneLedgerDocuments(ctx context.Context) (int, error) {
	if s.Assets == nil {
		return 0, fmt.Errorf("insight asset repository is not configured")
	}
	return s.Assets.DeleteLedgerAssetsByPlatformKind(ctx, ledgerRejectedKinds)
}

// LedgerAcceptsKind 判断这一类东西该不该进台账。
//
// 台账每一行右边都挂着一个「拉进分析」，按下去这条素材就进队列等投放数据回流。
// 一份 .txt 的策略文档、一段文案永远不会被投放，也就永远等不到回流数据——
// 给它一个「拉进分析」，是在邀请人做一件做完就卡住的事。这类产出留在平台素材库里，
// 那边本来就管着所有东西。
//
// 认不出来的（包括空值）先收着：台账少一条素材人不会发现，多一条人一眼就看见，
// 而且能自己判断。运行时那条通路（internal/integrations/insightsledger）也调这里，
// 两条路共用一份规则，免得哪天只改了一边。
func LedgerAcceptsKind(kind string) bool {
	switch kind {
	case "document", "text":
		return false
	}
	return true
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

// ledgerObjectTitle 从对象存储的路径里取出一个像文件名的名字。
//
// 回填拿不到当初的上传文件名——那躺在上传会话里，早随会话过期清掉了。但对象键的
// 最后一段常常就是它，比如 orgs/o/projects/p/creative-video.mp4。
//
// 认不出来就返回空串，让调用方退回按来源兜底那条路。宁可写「模型产物 · 8月1日」，
// 也别把 9f2c1ab7d4e05c6839aa1b0e77c4f2d5 摆到台账上——那不是名字，是个编号。
func ledgerObjectTitle(objectKey string) string {
	name := strings.TrimSpace(objectKey)
	if index := strings.LastIndex(name, "/"); index >= 0 {
		name = name[index+1:]
	}
	// 认不认得出，就看有没有扩展名。最后一段要是连扩展名都没有，
	// 剩下的只可能是一串对象 ID，摆在台账上读不出任何东西。
	dot := strings.LastIndex(name, ".")
	if dot <= 0 || dot == len(name)-1 || len(name) > 255 {
		return ""
	}
	return name
}

func backfillTitle(objectKey, sourceType string, at time.Time) string {
	if title := ledgerObjectTitle(objectKey); title != "" {
		return title
	}
	// 没有名字可用时，至少说得清它是哪天、从哪条路进来的。
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
