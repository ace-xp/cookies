// Package insightsposter 把素材库的 poster_v1 派生物换成洞察能直接用的地址。
//
// 分层：洞察不许 import 素材库的实现细节（派生物、签名、blob），
// 素材库也不该认识洞察。中间这一层两边都认识，装配时塞进去。
package insightsposter

import (
	"context"
	"errors"
	"fmt"

	"github.com/shikanon/cookies/internal/platform/assets"
	"github.com/shikanon/cookies/internal/platform/contract"
	"github.com/shikanon/cookies/internal/systems/insights"
)

type Reader struct {
	Derivatives assets.DerivativeService
	Uploads     *assets.UploadService
}

func (r Reader) ReadPoster(ctx context.Context, actor contract.ActorContext, projectID contract.ProjectID, platformAssetID string, platformAssetVersion int64) (string, error) {
	if r.Uploads == nil {
		return "", fmt.Errorf("asset upload service is required")
	}
	source := contract.AssetVersionRef{AssetID: contract.AssetID(platformAssetID), Version: platformAssetVersion}
	derivative, err := r.Derivatives.FindDerivative(ctx, actor.OrganizationID, projectID, source, assets.DerivativePoster)
	if err != nil {
		// 素材库和洞察各有一个「没找到」。不换过来的话，HTTP 层认不出
		// assets.ErrNotFound，一条还没抽帧的素材会让接口回 500。
		if errors.Is(err, assets.ErrNotFound) {
			return "", insights.ErrNotFound
		}
		return "", err
	}
	// 还在排队或者做失败了，都算「现在没有封面」。前端退回类型图标，
	// 过一会儿再进这一页就有了——不必给用户看一个「生成中」的转圈。
	if derivative.Status != assets.DerivativeReady || derivative.Output == nil {
		return "", insights.ErrNotFound
	}
	signed, err := r.Uploads.Preview(ctx, actor, projectID, *derivative.Output)
	if err != nil {
		return "", err
	}
	return signed.URL, nil
}
