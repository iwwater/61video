package scanner

import (
	"path"
	"regexp"
	"strings"

	"github.com/video-site/backend/internal/fixedtags"
)

// SystemTagMatcher 提供 system 标签的文件名匹配。可由 main 注入到全局，
// 这样 scanner 不直接依赖 systemtags（避免循环依赖）。
type SystemTagMatcher interface {
	MatchFilename(name string) []string
}

var systemTagMatcher SystemTagMatcher = fixedtagsMatcher{}

type fixedtagsMatcher struct{}

func (fixedtagsMatcher) MatchFilename(name string) []string {
	return fixedtags.MatchFilename(name)
}

// SetSystemTagMatcher 替换全局匹配器。启动时调一次，切换到 settings 持久化的标签集。
func SetSystemTagMatcher(m SystemTagMatcher) {
	if m != nil {
		systemTagMatcher = m
	}
}

// ParsedName 从文件名里解析出的视频元数据
type ParsedName struct {
	Title  string
	Author string
	Tags   []string
}

var (
	reTags   = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*`) // [前缀]
	reAuthor = regexp.MustCompile(`\s*-\s*([^-]+?)\s*$`) // - author
)

// Parse 按约定解析：[前缀] 标题 - 作者.ext
// 任何字段缺失都能降级
func Parse(filename string) ParsedName {
	name := strings.TrimSuffix(filename, path.Ext(filename))

	var out ParsedName

	if m := reTags.FindStringSubmatch(name); m != nil {
		name = strings.TrimSpace(name[len(m[0]):])
	}

	if m := reAuthor.FindStringSubmatch(name); m != nil {
		out.Author = strings.TrimSpace(m[1])
		name = strings.TrimSpace(name[:len(name)-len(m[0])])
	}

	out.Title = strings.TrimSpace(name)
	out.Tags = systemTagMatcher.MatchFilename(filename)
	return out
}
