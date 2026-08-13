package insights

import (
	"strings"
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
	// 这一条没带文件，所以不该有存储路径。有路径就意味着界面会对它说
	// 「原片将在 X 前后清掉」——而那个原片根本不存在。
	if value.StorageKey != "" {
		t.Errorf("没带文件却拼出了存储路径：%q", value.StorageKey)
	}

	withFile := request
	withFile.FileExt = "mp4"
	stored := buildExternalAsset("ext_2", withFile, "user_1", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if got := stored.StorageKey; !strings.HasPrefix(got, externalStoragePrefix) {
		t.Errorf("存储路径没加前缀：%q", got)
	}
	if got := stored.StorageKey; !strings.HasSuffix(got, ".mp4") {
		t.Errorf("存储路径没带扩展名：%q", got)
	}
}
