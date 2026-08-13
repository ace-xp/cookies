// cookies-maintain 跑一次性的维护动作。
//
// 仓里没有定时任务框架，也不该为了一个清理动作引入调度库。所以这些动作先做成
// 手工命令：需要的时候跑一次，看得见删了多少条。要自动化的时候，外面挂一个
// 系统级定时器调它就行，不必改代码。
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/shikanon/cookies/internal/platform/config"
	"github.com/shikanon/cookies/internal/platform/database"
	"github.com/shikanon/cookies/internal/systems/insights"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: cookies-maintain <purge-empty-drafts|backfill-ledger|prune-ledger-documents>")
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Open(ctx, cfg.MySQL)
	if err != nil {
		log.Fatalf("open MySQL: %v", err)
	}
	defer db.Close()

	switch command := os.Args[1]; command {
	case "purge-empty-drafts":
		// 「记一笔」建了但人什么都没记的复盘草稿。留着会一直占住
		// (项目 + 窗口) 的唯一键，下一次记一笔就撞在这份空壳上。
		service := insights.Service{Repository: insights.MySQLRepository{DB: db}}
		purged, err := service.PurgeEmptyDrafts(ctx)
		if err != nil {
			log.Fatalf("purge empty insight report drafts: %v", err)
		}
		log.Printf("清掉 %d 份空的复盘草稿", purged)
	case "backfill-ledger":
		// 台账是这次才建的，之前入库的素材一条都没登记。这个命令把它们补上。
		// 一次补 1000 条，反复跑到回报 0 为止；重复的那些由唯一键挡住，跑几次都无害。
		//
		// 一批是 1000 次单行 INSERT，30 秒的默认预算不够用，单给它十分钟。
		backfillCtx, cancelBackfill := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancelBackfill()
		service := insights.Service{Assets: insights.MySQLRepository{DB: db}}
		recorded, err := service.BackfillLedger(backfillCtx, 1000)
		if err != nil {
			log.Fatalf("backfill insight asset ledger: %v", err)
		}
		log.Printf("补了 %d 条台账素材（回报 0 就是补完了）", recorded)
	case "prune-ledger-documents":
		// 台账头一版是照单全收的，策略、简报、洞察报告这些文档也被回填进去了。
		// 它们投不出去、等不到回流数据，却在台账里各占一行，右边还挂着「拉进分析」。
		// 这个命令做一次性订正；收录侧已经挡住了，跑完不会再长回来。
		//
		// 只清还躺在台账里的。已经被人拉进分析的不碰——那上面有人做过的判断。
		service := insights.Service{Assets: insights.MySQLRepository{DB: db}}
		pruned, err := service.PruneLedgerDocuments(ctx)
		if err != nil {
			log.Fatalf("prune insight ledger documents: %v", err)
		}
		log.Printf("从台账清掉 %d 条文档", pruned)
	default:
		fmt.Fprintf(os.Stderr, "未知命令 %q\n", command)
		os.Exit(2)
	}
}
