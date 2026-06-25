// seednovel 一次性脚本：往库里塞两条测试小说（text + pdf），方便本地验证。
// 用法：go run ./cmd/seednovel
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/video-site/backend/internal/catalog"
)

func main() {
	dbPath := "data/video-site.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}
	cat, err := catalog.Open(dbPath)
	if err != nil {
		panic(err)
	}
	defer cat.Close()

	now := time.Now().UnixMilli()
	tags := []string{"玄幻", "经典"}

	err = cat.UpsertNovelSet(context.Background(), &catalog.NovelSet{
		ID:          "test-novel-001",
		Title:       "测试小说：星辰大海",
		Author:      "测试作者",
		CoverURL:    "",
		ContentType: "text",
		Tags:        tags,
		Description: "这是一本用于验证阅读器的测试小说。",
		SourceKind:  "manual",
		CreatedAt:   now,
		UpdatedAt:   now,
		PublishedAt: now,
		Chapters: []catalog.NovelChapter{
			{Position: 0, Title: "第一章 启程", ContentType: "text", Body: "<p>这是<strong>第一章</strong>的正文。</p><p>故事从这里开始。</p>"},
			{Position: 1, Title: "第二章 相遇", ContentType: "text", Body: "<p>第二章内容。</p><p>主角遇到了同伴。</p>"},
			{Position: 2, Title: "第三章 冒险", ContentType: "text", Body: "<p>第三章内容。</p>"},
		},
	})
	if err != nil {
		panic(err)
	}

	err = cat.UpsertNovelSet(context.Background(), &catalog.NovelSet{
		ID:          "test-pdf-001",
		Title:       "测试 PDF：技术手册",
		Author:      "技术组",
		ContentType: "pdf",
		Tags:        []string{"技术", "文档"},
		Description: "PDF 阅读测试。",
		SourceKind:  "manual",
		CreatedAt:   now,
		UpdatedAt:   now,
		PublishedAt: now,
		Chapters: []catalog.NovelChapter{
			{Position: 0, Title: "完整文档", ContentType: "pdf", PDFURL: "https://www.africau.edu/images/default/sample.pdf", Headers: map[string]string{}},
		},
	})
	if err != nil {
		panic(err)
	}
	_ = cat.HideNovelSet(context.Background(), "test-pdf-001")

	fmt.Println("seeded: test-novel-001 (text, visible) + test-pdf-001 (pdf, hidden)")
}
