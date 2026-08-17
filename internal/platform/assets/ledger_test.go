package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// recordingLedger 只记下收到了什么，从不失败。
type recordingLedger struct {
	entries []LedgerEntry
	err     error
}

func (l *recordingLedger) Record(_ context.Context, entry LedgerEntry) error {
	l.entries = append(l.entries, entry)
	return l.err
}

func TestLedgerTitlePrefersFilename(t *testing.T) {
	at := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	if got := LedgerTitle("主视觉 KV.png", contract.AssetSourceUpload, at); got != "主视觉 KV.png" {
		t.Fatalf("有文件名就用文件名，得到 %q", got)
	}
}

func TestLedgerTitleFallsBackBySource(t *testing.T) {
	at := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	cases := map[contract.AssetSourceType]string{
		contract.AssetSourceRendered:          "渲染成片 · 2026-08-13",
		contract.AssetSourceProviderGenerated: "模型产物 · 2026-08-13",
		contract.AssetSourceImported:          "外部导入 · 2026-08-13",
		contract.AssetSourceCaptured:          "采集素材 · 2026-08-13",
		contract.AssetSourceUpload:            "未命名素材 · 2026-08-13",
	}
	for source, want := range cases {
		if got := LedgerTitle("", source, at); got != want {
			t.Fatalf("%q 的兜底标题应是 %q，得到 %q", source, want, got)
		}
	}
}

func TestLedgerTitleTrimsOverlongFilename(t *testing.T) {
	// 台账的 title 列是 VARCHAR(255)。超长文件名不截断的话，
	// 收录会在 INSERT 那一步失败，而失败是被吞掉的——素材就这么悄悄漏登记了。
	long := ""
	for len(long) < 400 {
		long += "长"
	}
	got := LedgerTitle(long, contract.AssetSourceUpload, time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC))
	if len([]rune(got)) > 255 {
		t.Fatalf("标题应截到 255 个字符以内，得到 %d 个", len([]rune(got)))
	}
}

func TestLedgerRelayIsSafeWhenUnwired(t *testing.T) {
	// 装配顺序决定了 relay 一定先于 recorder 存在。这段时间里入库不能崩。
	relay := &LedgerRelay{}
	if err := relay.Record(t.Context(), LedgerEntry{}); err != nil {
		t.Fatalf("没接上 recorder 时应当什么都不做，得到 %v", err)
	}
}

// 上传成功后台账必须收到那一条。钩子挂错或漏挂时代码照样编译、上传照样成功，
// 症状只是「洞察那边的素材库永远是空的」——只能靠这条测试兜住。
func TestFinalizeRecordsLedgerEntry(t *testing.T) {
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	ledger := &recordingLedger{}
	service := UploadService{
		Repository: newFakeRepository(), Projects: fakeProjects{organization: "org_1", project: "project_1", version: 4},
		Blobs: NewMemoryBlobStore(), Scanner: NoopScanner{},
		QuarantineBucket: "quarantine", AssetsBucket: "assets",
		Now: func() time.Time { return now }, NewID: sequenceIDs(), Ledger: ledger,
	}
	data := testPNG(t)
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	rc := testRequestContext("org_1", "project_1")
	ctx := context.Background()

	created, err := service.Create(ctx, rc, "project_1", "upload-key", CreateUploadRequest{
		Filename: "主视觉 KV.png", DeclaredMIMEType: "image/png",
		DeclaredSizeBytes: int64(len(data)), DeclaredSHA256: &hash,
	})
	if err != nil {
		t.Fatalf("建上传会话失败：%v", err)
	}
	if err := service.PutContent(ctx, rc.Actor, "project_1", created.Session.ID, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("传内容失败：%v", err)
	}
	result, err := service.Finalize(ctx, rc, "project_1", created.Session.ID)
	if err != nil {
		t.Fatalf("收尾失败：%v", err)
	}

	if len(ledger.entries) != 1 {
		t.Fatalf("台账应收到 1 条，得到 %d 条", len(ledger.entries))
	}
	entry := ledger.entries[0]
	if entry.Title != "主视觉 KV.png" {
		t.Fatalf("标题应是用户起的文件名，得到 %q", entry.Title)
	}
	if entry.AssetID != result.ProjectAssetRef.AssetVersion.AssetID ||
		entry.Version != result.ProjectAssetRef.AssetVersion.Version {
		t.Fatalf("台账记的版本对不上落库的版本：%#v", entry)
	}
	if entry.SourceType != contract.AssetSourceUpload || entry.ActorID != rc.Actor.Principal.ID {
		t.Fatalf("来源或操作人不对：%#v", entry)
	}
}

// 台账写失败不许把上传拖下水：文件已经在库里了，为了一条账目退掉它是本末倒置。
func TestFinalizeSurvivesLedgerFailure(t *testing.T) {
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	ledger := &recordingLedger{err: errors.New("洞察库连不上")}
	service := UploadService{
		Repository: newFakeRepository(), Projects: fakeProjects{organization: "org_1", project: "project_1", version: 4},
		Blobs: NewMemoryBlobStore(), Scanner: NoopScanner{},
		QuarantineBucket: "quarantine", AssetsBucket: "assets",
		Now: func() time.Time { return now }, NewID: sequenceIDs(), Ledger: ledger,
	}
	data := testPNG(t)
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	rc := testRequestContext("org_1", "project_1")
	ctx := context.Background()

	created, err := service.Create(ctx, rc, "project_1", "upload-key", CreateUploadRequest{
		Filename: "hero.png", DeclaredMIMEType: "image/png",
		DeclaredSizeBytes: int64(len(data)), DeclaredSHA256: &hash,
	})
	if err != nil {
		t.Fatalf("建上传会话失败：%v", err)
	}
	if err := service.PutContent(ctx, rc.Actor, "project_1", created.Session.ID, bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("传内容失败：%v", err)
	}
	result, err := service.Finalize(ctx, rc, "project_1", created.Session.ID)
	if err != nil || result.Status != UploadSucceeded {
		t.Fatalf("台账失败不该影响上传，得到 status=%q err=%v", result.Status, err)
	}
}
