package main

import (
	"context"
	"fmt"
	"os"

	"github.com/video-site/backend/internal/catalog"
)

// diag-thumbfailures：列出失败封面/预览视频的样本，查看失败原因。
// 用法：cd backend && go run ./cmd/diag-thumbfailures
func main() {
	cat, err := catalog.Open("./data/video-site.db")
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	defer cat.Close()
	ctx := context.Background()

	fmt.Println("=== 失败封面样本（前 10 条） ===")
	rows, err := cat.DB().QueryContext(ctx,
		`SELECT id, drive_id, file_name, size_bytes, thumbnail_failures,
		        COALESCE(duration_seconds, 0), COALESCE(media_type, '?')
		 FROM videos
		 WHERE COALESCE(thumbnail_status, 'pending') = 'failed'
		 ORDER BY thumbnail_failures DESC, id ASC
		 LIMIT 10`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "thumb query: %v\n", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var id, drive, fname, mtype string
			var size int64
			var failures int
			var dur int
			if err := rows.Scan(&id, &drive, &fname, &size, &failures, &dur, &mtype); err != nil {
				fmt.Fprintf(os.Stderr, "scan: %v\n", err)
				continue
			}
			fmt.Printf("  id=%s drive=%s failures=%d dur=%d size=%d mtype=%s\n",
				id, drive, failures, dur, size, mtype)
			fmt.Printf("    name=%s\n", fname)
		}
	}

	fmt.Println()
	fmt.Println("=== 失败预览视频样本（前 10 条） ===")
	rows, err = cat.DB().QueryContext(ctx,
		`SELECT id, drive_id, file_name, size_bytes,
		        COALESCE(duration_seconds, 0), COALESCE(media_type, '?'),
		        COALESCE(preview_local, '')
		 FROM videos
		 WHERE COALESCE(preview_status, 'pending') = 'failed'
		 ORDER BY id ASC
		 LIMIT 10`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "preview query: %v\n", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var id, drive, fname, mtype, previewLocal string
			var size int64
			var dur int
			if err := rows.Scan(&id, &drive, &fname, &size, &dur, &mtype, &previewLocal); err != nil {
				fmt.Fprintf(os.Stderr, "scan: %v\n", err)
				continue
			}
			fmt.Printf("  id=%s drive=%s dur=%d size=%d mtype=%s preview=%q\n",
				id, drive, dur, size, mtype, previewLocal)
			fmt.Printf("    name=%s\n", fname)
		}
	}

	fmt.Println()
	fmt.Println("=== 失败原因统计（按 drive + 媒体类型） ===")
	rows, err = cat.DB().QueryContext(ctx,
		`SELECT drive_id,
		        COALESCE(media_type, 'video') AS mtype,
		        SUM(CASE WHEN COALESCE(thumbnail_status, 'pending') = 'failed' THEN 1 ELSE 0 END) AS thumb_failed,
		        SUM(CASE WHEN COALESCE(preview_status, 'pending') = 'failed' THEN 1 ELSE 0 END) AS preview_failed,
		        COUNT(*) AS total
		 FROM videos
		 GROUP BY drive_id, COALESCE(media_type, 'video')
		 ORDER BY drive_id, mtype`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stats query: %v\n", err)
	} else {
		defer rows.Close()
		fmt.Printf("  %-30s %-10s %12s %12s %10s\n", "drive", "type", "thumb_fail", "prev_fail", "total")
		for rows.Next() {
			var drive, mtype string
			var thumbF, prevF, total int
			if err := rows.Scan(&drive, &mtype, &thumbF, &prevF, &total); err != nil {
				fmt.Fprintf(os.Stderr, "scan: %v\n", err)
				continue
			}
			fmt.Printf("  %-30s %-10s %12d %12d %10d\n", drive, mtype, thumbF, prevF, total)
		}
	}

	fmt.Println()
	fmt.Println("=== ID 前缀分布（spider91- 失败的占比） ===")
	rows, err = cat.DB().QueryContext(ctx,
		`SELECT
		    CASE WHEN id LIKE 'spider91-%' THEN 'spider91-*' ELSE id END AS bucket,
		    SUM(CASE WHEN COALESCE(thumbnail_status, 'pending') = 'failed' THEN 1 ELSE 0 END) AS thumb_failed
		 FROM videos
		 GROUP BY bucket
		 ORDER BY thumb_failed DESC
		 LIMIT 15`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bucket query: %v\n", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var bucket string
			var failed int
			if err := rows.Scan(&bucket, &failed); err != nil {
				fmt.Fprintf(os.Stderr, "scan: %v\n", err)
				continue
			}
			if failed > 0 {
				fmt.Printf("  thumb_failed=%-5d  %s\n", failed, bucket)
			}
		}
	}
}