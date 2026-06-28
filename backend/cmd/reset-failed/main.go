package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/video-site/backend/internal/catalog"
)

// reset-failed：把失败状态的封面/预览视频重置回 pending，让后台 worker 重新生成。
// 用法：cd backend && go run ./cmd/reset-failed [--dry-run]
//
// 触发场景：之前因为 ffmpeg PATH 等环境问题导致大批失败，修复环境后想一次性重跑。
// 只改 status 和 error 字段，不动 url/local（已经成功的保留）。
func main() {
	dryRun := false
	for _, arg := range os.Args[1:] {
		if arg == "--dry-run" {
			dryRun = true
		}
	}

	cat, err := catalog.Open("./data/video-site.db")
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer cat.Close()
	ctx := context.Background()

	// 先统计
	row := cat.DB().QueryRowContext(ctx,
		`SELECT
		   SUM(CASE WHEN COALESCE(thumbnail_status, 'pending') = 'failed' THEN 1 ELSE 0 END),
		   SUM(CASE WHEN COALESCE(preview_status, 'pending') = 'failed' THEN 1 ELSE 0 END)
		 FROM videos`)
	var thumbFailed, previewFailed int
	if err := row.Scan(&thumbFailed, &previewFailed); err != nil {
		fmt.Fprintf(os.Stderr, "stats: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("当前状态：thumbnail 失败 %d 条，preview 失败 %d 条\n", thumbFailed, previewFailed)
	if thumbFailed == 0 && previewFailed == 0 {
		fmt.Println("没有失败条目，无需重置。")
		return
	}

	if dryRun {
		fmt.Println("[dry-run] 不会真的修改 DB，仅打印将执行的操作。")
		return
	}

	// 重置失败条目
	now := time.Now().UnixMilli()
	res1, err := cat.DB().ExecContext(ctx,
		`UPDATE videos
		    SET thumbnail_status = 'pending',
		        thumbnail_error = '',
		        thumbnail_failures = 0,
		        updated_at = ?
		  WHERE COALESCE(thumbnail_status, 'pending') = 'failed'`,
		now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reset thumbnail: %v\n", err)
		os.Exit(1)
	}
	thumbReset, _ := res1.RowsAffected()

	res2, err := cat.DB().ExecContext(ctx,
		`UPDATE videos
		    SET preview_status = 'pending',
		        preview_error = '',
		        preview_failures = 0,
		        updated_at = ?
		  WHERE COALESCE(preview_status, 'pending') = 'failed'`,
		now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reset preview: %v\n", err)
		os.Exit(1)
	}
	previewReset, _ := res2.RowsAffected()

	fmt.Printf("已重置：thumbnail %d 条，preview %d 条。重启 launcher 后台 worker 会自动入队重新生成。\n",
		thumbReset, previewReset)
}