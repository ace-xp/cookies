package insights

import (
	"context"
	"errors"
	"slices"
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

// 文档不进台账。台账每一行右边都挂着「拉进分析」，而一份 .txt 的策略文档
// 永远不会被投放、等不到回流数据、进不了复盘——SQL 已经滤掉一层，这里守住第二层。
func TestBackfillLedgerSkipsDocuments(t *testing.T) {
	t.Parallel()
	service := testAssetService()
	repository := service.Assets.(*memoryAssetRepository)
	repository.unledgered = []UnledgeredPlatformAsset{
		{OrganizationID: "org_1", ProjectID: "project_1", AssetID: "asset_1", Version: 1, SourceType: "imported", Kind: "document"},
		{OrganizationID: "org_1", ProjectID: "project_1", AssetID: "asset_2", Version: 1, SourceType: "imported", Kind: "text"},
		{OrganizationID: "org_1", ProjectID: "project_1", AssetID: "asset_3", Version: 1, SourceType: "provider_generated", Kind: "video"},
	}
	recorded, err := service.BackfillLedger(context.Background(), 100)
	if err != nil {
		t.Fatalf("回填失败：%v", err)
	}
	if recorded != 1 {
		t.Fatalf("三条里只有那条视频该进台账，得到 %d 条", recorded)
	}
}

// 台账建起来的头一版是照单全收的，库里已经躺着一批文档。这条命令做一次性订正。
func TestPruneLedgerDocumentsRemovesOnlyLedgerDocuments(t *testing.T) {
	t.Parallel()
	service, actor := testAssetService(), testActor()
	repository := service.Assets.(*memoryAssetRepository)
	repository.platformKinds = map[string]string{
		"platform_doc": "document", "platform_copy": "text", "platform_video": "video",
	}
	repository.assets = map[string]Asset{
		"a1": {ID: "a1", OrganizationID: actor.OrganizationID, ProjectID: "project_1",
			Role: AssetRoleLedger, PlatformAssetID: "platform_doc", Title: "精度证据增长策略"},
		"a2": {ID: "a2", OrganizationID: actor.OrganizationID, ProjectID: "project_1",
			Role: AssetRoleLedger, PlatformAssetID: "platform_copy", Title: "一版文案"},
		"a3": {ID: "a3", OrganizationID: actor.OrganizationID, ProjectID: "project_1",
			Role: AssetRoleLedger, PlatformAssetID: "platform_video", Title: "品牌片"},
		// 有人已经把它拉进分析了。这上面挂着人做过的判断，不能因为它是文档就抹掉。
		"a4": {ID: "a4", OrganizationID: actor.OrganizationID, ProjectID: "project_1",
			Role: AssetRoleAnalysis, PlatformAssetID: "platform_doc", Title: "被拉进分析的那份"},
		// 手工登记的没有平台来源，查不到类型——不该被顺手删掉。
		"a5": {ID: "a5", OrganizationID: actor.OrganizationID, ProjectID: "project_1",
			Role: AssetRoleLedger, Title: "手工登记的一条"},
	}

	pruned, err := service.PruneLedgerDocuments(context.Background())
	if err != nil {
		t.Fatalf("清理失败：%v", err)
	}
	if pruned != 2 {
		t.Fatalf("该清掉那份策略文档和那版文案，共 2 条，得到 %d 条", pruned)
	}
	for _, id := range []string{"a3", "a4", "a5"} {
		if _, ok := repository.assets[id]; !ok {
			t.Fatalf("%s 不该被清掉", id)
		}
	}
	if want := []string{"document", "text"}; !slices.Equal(repository.prunedKinds, want) {
		t.Fatalf("清理的类型应是 %v，得到 %v", want, repository.prunedKinds)
	}
}

// 回填的标题以前一律是「模型产物 · 日期」。同一天进来的素材因此全部同名，
// 台账上一屏六行四个一样的名字，人只能挨个点开去认。
func TestBackfillLedgerNamesAssetsByObjectKey(t *testing.T) {
	t.Parallel()
	service, actor := testAssetService(), testActor()
	at := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	repository := service.Assets.(*memoryAssetRepository)
	repository.unledgered = []UnledgeredPlatformAsset{
		{OrganizationID: "org_1", ProjectID: "project_1", AssetID: "asset_1", Version: 1,
			SourceType: "provider_generated", Kind: "video", CreatedAt: at,
			ObjectKey: "orgs/org_1/projects/project_1/creative-video.mp4"},
		// 对象键有时就是一串哈希，认不出是什么——那种还不如按来源兜底。
		{OrganizationID: "org_1", ProjectID: "project_1", AssetID: "asset_2", Version: 1,
			SourceType: "provider_generated", Kind: "image", CreatedAt: at,
			ObjectKey: "orgs/org_1/blobs/9f2c1ab7d4e05c6839aa1b0e77c4f2d5"},
	}
	ctx := context.Background()

	if _, err := service.BackfillLedger(ctx, 100); err != nil {
		t.Fatalf("回填失败：%v", err)
	}
	ledger, err := service.ListAssets(ctx, actor, "project_1", AssetFilter{Roles: []AssetRole{AssetRoleLedger}})
	if err != nil {
		t.Fatalf("查台账失败：%v", err)
	}
	titles := map[string]bool{}
	for _, asset := range ledger {
		titles[asset.Title] = true
	}
	if !titles["creative-video.mp4"] {
		t.Fatalf("有文件名就该用文件名：%#v", titles)
	}
	if !titles["模型产物 · 2026-08-01"] {
		t.Fatalf("认不出名字的仍按来源兜底：%#v", titles)
	}
}

func TestLedgerObjectTitle(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"orgs/o/projects/p/主视觉 KV.png": "主视觉 KV.png",
		"creative-video.mp4":           "creative-video.mp4",
		// 没有扩展名、又长得像哈希的，说不出这是个什么东西。
		"orgs/o/blobs/9f2c1ab7d4e05c6839aa1b0e77c4f2d5": "",
		"orgs/o/projects/p/":                            "",
		"":                                              "",
	}
	for key, want := range cases {
		if got := ledgerObjectTitle(key); got != want {
			t.Fatalf("%q 应取出 %q，得到 %q", key, want, got)
		}
	}
}

func TestLedgerAcceptsKind(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"video", "image", "audio"} {
		if !LedgerAcceptsKind(kind) {
			t.Fatalf("%q 能拿去投，必须进台账", kind)
		}
	}
	for _, kind := range []string{"document", "text"} {
		if LedgerAcceptsKind(kind) {
			t.Fatalf("%q 投不出去，不该进台账", kind)
		}
	}
	// 老数据和内存实现都可能不带类型。少收一条人不会发现，多收一条人一眼看见。
	if !LedgerAcceptsKind("") {
		t.Fatal("类型不明的应当先收着")
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
