package assets

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/contract"
)

// 台账是洞察那边的账本：平台里落库成功的每一个素材版本都该在里面有一条。
//
// 为什么是回调不是事件：仓里的 assets_outbox 和 event_outbox 两张表都在写，
// 但全仓没有任何一处 SELECT 它们，Dispatcher 在生产代码里从没被实例化过。
// 接一条事实上不会被消费的事件，等于台账永远是空的。
//
// 分层不能破：这个包在 internal/platform 下，不许 import internal/systems。
// 所以这里只定义接口，实现放在 internal/integrations/insightsledger，
// 由 cmd/cookies-api 在装配时塞进来。
type LedgerEntry struct {
	OrganizationID contract.OrganizationID
	ProjectID      contract.ProjectID
	// ActorID 是触发这次入库的人。台账要能回答「这条是谁弄进来的」。
	ActorID string

	AssetID    contract.AssetID
	Version    int64
	Kind       contract.AssetKind
	SourceType contract.AssetSourceType
	Title      string
}

type LedgerRecorder interface {
	Record(ctx context.Context, entry LedgerEntry) error
}

// LedgerRelay 解装配顺序的死结：uploadService 在 main.go 靠前的地方就构造好了，
// 而 insightsService 要到几百行之后才有；中间还有几处按值把 UploadService 拷走。
// 拷的是这个指针，回填的也是这个指针里的字段，两边就对上了。
type LedgerRelay struct {
	Recorder LedgerRecorder
}

func (r *LedgerRelay) Record(ctx context.Context, entry LedgerEntry) error {
	if r == nil || r.Recorder == nil {
		return nil
	}
	return r.Recorder.Record(ctx, entry)
}

const ledgerTitleMaxRunes = 255

// LedgerTitle 给台账里的一条素材起个人看得懂的名字。
//
// 上传有文件名就用文件名——那是人自己起的，比任何生成的名字都准。
// 其余几条路径没有名字可用，按来源给个说得清出处的兜底，至少不是一串 ID。
func LedgerTitle(filename string, source contract.AssetSourceType, at time.Time) string {
	if trimmed := strings.TrimSpace(filename); trimmed != "" {
		runes := []rune(trimmed)
		if len(runes) > ledgerTitleMaxRunes {
			return string(runes[:ledgerTitleMaxRunes])
		}
		return trimmed
	}
	date := at.Format("2006-01-02")
	switch source {
	case contract.AssetSourceRendered:
		return fmt.Sprintf("渲染成片 · %s", date)
	case contract.AssetSourceProviderGenerated:
		return fmt.Sprintf("模型产物 · %s", date)
	case contract.AssetSourceImported:
		return fmt.Sprintf("外部导入 · %s", date)
	case contract.AssetSourceCaptured:
		return fmt.Sprintf("采集素材 · %s", date)
	}
	return fmt.Sprintf("未命名素材 · %s", date)
}
