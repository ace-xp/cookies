package insights

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecordLedgerAssetWritesLedgerRole(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	asset, err := service.RecordLedgerAsset(context.Background(), RecordLedgerAssetRequest{
		OrganizationID: "org_1", ProjectID: "project_1", ActorID: "user_1",
		Title: "主视觉 KV.png", SourceKind: AssetSourceUpload,
		PlatformAssetID: "asset_1", PlatformAssetVersion: 1,
	})
	if err != nil {
		t.Fatalf("收录失败：%v", err)
	}
	if asset.Role != AssetRoleLedger {
		t.Fatalf("收录进来的必须是台账，得到 %q", asset.Role)
	}
	// 台账素材没有广告形态。AssetType 是六种广告形态之一，不是 image/video，
	// 从平台的 AssetKind 是推不出来的——推出来就是编。
	if asset.AssetType != AssetTypeUnknown {
		t.Fatalf("收录不该猜广告形态，得到 %q", asset.AssetType)
	}
}

func TestRecordLedgerAssetRequiresPlatformPair(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	_, err := service.RecordLedgerAsset(context.Background(), RecordLedgerAssetRequest{
		OrganizationID: "org_1", ProjectID: "project_1", ActorID: "user_1",
		Title: "缺版本号的引用", SourceKind: AssetSourceUpload, PlatformAssetID: "asset_1",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("平台引用给一半应被拒，得到 %v", err)
	}
}

func TestRecordLedgerAssetRequiresTitle(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	_, err := service.RecordLedgerAsset(context.Background(), RecordLedgerAssetRequest{
		OrganizationID: "org_1", ProjectID: "project_1", ActorID: "user_1",
		SourceKind: AssetSourceUpload, PlatformAssetID: "asset_1", PlatformAssetVersion: 1,
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("标题为空应被拒——兜底标题由调用方给，服务层不替它编，得到 %v", err)
	}
}

// 收录进来的素材必须落在台账那一侧：默认的清单（不传 role）看不见它，
// 显式要台账时才出现。这条断言守的是「台账不淹没四个分析队列」。
func TestRecordedLedgerAssetStaysOutOfAnalysisQueues(t *testing.T) {
	t.Parallel()
	service, actor := testAssetService(), testActor()
	ctx := context.Background()
	if _, err := service.RecordLedgerAsset(ctx, RecordLedgerAssetRequest{
		OrganizationID: actor.OrganizationID, ProjectID: "project_1", ActorID: "user_1",
		Title: "开屏 15s.mp4", SourceKind: AssetSourceUpload,
		PlatformAssetID: "asset_9", PlatformAssetVersion: 2,
	}); err != nil {
		t.Fatalf("收录失败：%v", err)
	}

	analysis, err := service.ListAssets(ctx, actor, "project_1", AssetFilter{})
	if err != nil {
		t.Fatalf("查分析对象失败：%v", err)
	}
	if len(analysis) != 0 {
		t.Fatalf("默认清单不该看见台账素材，得到 %d 条", len(analysis))
	}

	ledger, err := service.ListAssets(ctx, actor, "project_1", AssetFilter{Roles: []AssetRole{AssetRoleLedger}})
	if err != nil {
		t.Fatalf("查台账失败：%v", err)
	}
	if len(ledger) != 1 || ledger[0].Title != "开屏 15s.mp4" {
		t.Fatalf("台账里应有那一条，得到 %#v", ledger)
	}
}

func TestBackfillLedgerRejectsBadBatch(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	if _, err := service.BackfillLedger(context.Background(), 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("批量必须是正数，得到 %v", err)
	}
	if _, err := service.BackfillLedger(context.Background(), 100000); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("批量过大应被拒——一次拉十万行会把库拖垮，得到 %v", err)
	}
}

func TestBackfillLedgerRecordsPendingAssets(t *testing.T) {
	t.Parallel()
	service, actor := testAssetService(), testActor()
	at := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	repository := service.Assets.(*memoryAssetRepository)
	repository.unledgered = []UnledgeredPlatformAsset{
		{OrganizationID: "org_1", ProjectID: "project_1", AssetID: "asset_1", Version: 1, SourceType: "upload", CreatedAt: at},
		{OrganizationID: "org_1", ProjectID: "project_1", AssetID: "asset_2", Version: 3, SourceType: "rendered", CreatedAt: at},
		// 同一个版本挂在第二个项目下。台账的唯一键是组织级的，只能进一个项目。
		{OrganizationID: "org_1", ProjectID: "project_2", AssetID: "asset_2", Version: 3, SourceType: "rendered", CreatedAt: at},
	}
	ctx := context.Background()

	recorded, err := service.BackfillLedger(ctx, 100)
	if err != nil {
		t.Fatalf("回填失败：%v", err)
	}
	if recorded != 2 {
		t.Fatalf("三条里有两条是不同的素材版本，应补 2 条，得到 %d 条", recorded)
	}

	ledger, err := service.ListAssets(ctx, actor, "project_1", AssetFilter{Roles: []AssetRole{AssetRoleLedger}})
	if err != nil {
		t.Fatalf("查台账失败：%v", err)
	}
	if len(ledger) != 2 {
		t.Fatalf("台账里应有 2 条，得到 %d 条", len(ledger))
	}
	// 回填拿不到当初的文件名，也拿不到是谁传的；标题按来源加日期兜底，操作人落 system。
	titles := map[string]bool{}
	for _, asset := range ledger {
		titles[asset.Title] = true
		if asset.CreatedBy != "system" {
			t.Fatalf("回填的操作人应是 system，得到 %q", asset.CreatedBy)
		}
	}
	if !titles["未命名素材 · 2026-08-01"] || !titles["渲染成片 · 2026-08-01"] {
		t.Fatalf("兜底标题不对：%#v", titles)
	}
}

// 派生物不进台账。它们是同一个素材的另一种形态（缩略图、转码档），
// 收进来只会让素材数翻好几倍——SQL 已经滤掉一层，这里守住第二层。
func TestBackfillLedgerSkipsUnknownSourceTypes(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	repository := service.Assets.(*memoryAssetRepository)
	repository.unledgered = []UnledgeredPlatformAsset{
		{OrganizationID: "org_1", ProjectID: "project_1", AssetID: "asset_1", Version: 1, SourceType: "derived"},
	}
	recorded, err := service.BackfillLedger(context.Background(), 100)
	if err != nil {
		t.Fatalf("回填失败：%v", err)
	}
	if recorded != 0 {
		t.Fatalf("派生物不该进台账，得到 %d 条", recorded)
	}
}
