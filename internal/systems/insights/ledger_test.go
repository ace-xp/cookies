package insights

import (
	"context"
	"errors"
	"testing"
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
