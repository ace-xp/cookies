package insightsledger

import (
	"context"
	"testing"

	"github.com/shikanon/cookies/internal/platform/assets"
	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/systems/insights"
)

func TestSourceKindFromPlatformSource(t *testing.T) {
	cases := map[contract.AssetSourceType]insights.AssetSourceKind{
		contract.AssetSourceUpload:            insights.AssetSourceUpload,
		contract.AssetSourceRendered:          insights.AssetSourceCreative,
		contract.AssetSourceProviderGenerated: insights.AssetSourceCreative,
		contract.AssetSourceImported:          insights.AssetSourceExternal,
		contract.AssetSourceCaptured:          insights.AssetSourceExternal,
	}
	for source, want := range cases {
		got, ok := sourceKind(source)
		if !ok {
			t.Fatalf("%q 应该有对应的洞察来源", source)
		}
		if got != want {
			t.Fatalf("%q 应映射成 %q，得到 %q", source, want, got)
		}
	}
}

func TestSourceKindRejectsDerived(t *testing.T) {
	// 派生物是同一个素材的另一种形态，不是另一条素材。
	if _, ok := sourceKind(contract.AssetSourceDerived); ok {
		t.Fatal("派生物不该有洞察来源——它根本不进台账")
	}
}

func TestLedgerTakesPlayableKindsOnly(t *testing.T) {
	// 台账每一行右边都有个「拉进分析」。能按下去的必须是真能拿去投的东西。
	for _, kind := range []contract.AssetKind{contract.AssetVideo, contract.AssetImage, contract.AssetAudio} {
		if !playable(kind) {
			t.Fatalf("%q 能拿去投，必须进台账", kind)
		}
	}
	// 一份策略文档永远不会被投放、不会有回流数据、不会进复盘。
	// 收它进来只会让人在台账里挨条跳过，还得担心自己是不是漏了一条该拉的。
	for _, kind := range []contract.AssetKind{contract.AssetDocument, contract.AssetText} {
		if playable(kind) {
			t.Fatalf("%q 投不出去，不该进台账", kind)
		}
	}
}

// 认不出来的类型宁可收进来：台账少一条人不会发现，多一条人一眼就看见。
func TestLedgerKeepsUnknownKinds(t *testing.T) {
	if !playable(contract.AssetKind("hologram")) {
		t.Fatal("没见过的类型应当先收着，让人自己判断")
	}
}

// 没装洞察服务时（比如只跑素材库的部署）什么都不做，不能崩。
func TestRecorderIsSafeWithoutService(t *testing.T) {
	if err := (Recorder{}).Record(context.Background(), assets.LedgerEntry{}); err != nil {
		t.Fatalf("没接服务时应当什么都不做，得到 %v", err)
	}
}
