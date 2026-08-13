// Package insightsledger 把平台素材库的入库事实翻成洞察台账的一条记录。
//
// 它存在的唯一理由是分层：internal/platform/assets 不许 import
// internal/systems/insights，所以那边只定义接口，翻译放在这里，
// 由 cmd/cookies-api 在装配时把这个实现塞回去。
package insightsledger

import (
	"context"

	"github.com/shikanon/cookies/internal/platform/assets"
	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/systems/insights"
)

type Recorder struct {
	Service *insights.Service
}

func (r Recorder) Record(ctx context.Context, entry assets.LedgerEntry) error {
	if r.Service == nil {
		return nil
	}
	if !playable(entry.Kind) {
		return nil
	}
	kind, ok := sourceKind(entry.SourceType)
	if !ok {
		return nil
	}
	_, err := r.Service.RecordLedgerAsset(ctx, insights.RecordLedgerAssetRequest{
		OrganizationID: entry.OrganizationID, ProjectID: entry.ProjectID,
		ActorID: entry.ActorID, Title: entry.Title, SourceKind: kind,
		PlatformAssetID: string(entry.AssetID), PlatformAssetVersion: entry.Version,
	})
	return err
}

// playable 判断这东西投不投得出去。规则本身在 insights 那边——
// 回填命令走的是另一条路，两条路必须同一份规则，不然哪天只改了一边。
func playable(kind contract.AssetKind) bool {
	return insights.LedgerAcceptsKind(string(kind))
}

// sourceKind 把平台的六种入库来源折成洞察的三种。
//
// 折叠是有损的，但洞察关心的只有「这东西是我们自己做的、人传的、还是外面来的」——
// 平台那六种说的是入库通道，不是出处。返回 false 表示这一种根本不进台账。
func sourceKind(source contract.AssetSourceType) (insights.AssetSourceKind, bool) {
	switch source {
	case contract.AssetSourceUpload:
		return insights.AssetSourceUpload, true
	case contract.AssetSourceRendered, contract.AssetSourceProviderGenerated:
		// 渲染成片和模型产物都是创意模块做出来的。
		return insights.AssetSourceCreative, true
	case contract.AssetSourceImported, contract.AssetSourceCaptured:
		return insights.AssetSourceExternal, true
	}
	// AssetSourceDerived 落在这里：缩略图、转码档、抽出来的音轨
	// 是同一个素材的不同形态，收进台账只会让素材数翻好几倍。
	return "", false
}
