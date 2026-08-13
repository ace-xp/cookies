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

// 没装洞察服务时（比如只跑素材库的部署）什么都不做，不能崩。
func TestRecorderIsSafeWithoutService(t *testing.T) {
	if err := (Recorder{}).Record(context.Background(), assets.LedgerEntry{}); err != nil {
		t.Fatalf("没接服务时应当什么都不做，得到 %v", err)
	}
}
