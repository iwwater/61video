package catalog

import (
	"net/url"
	"strings"
)

// expandTemplate 把模板里的 {key} 占位符替换为 values 里的值。
// 支持 {key} 形式，不支持条件/循环/嵌套。
func expandTemplate(template string, values map[string]string) string {
	if template == "" {
		return ""
	}
	for k, v := range values {
		template = strings.ReplaceAll(template, "{"+k+"}", v)
	}
	return template
}

func queryEscape(s string) string { return url.QueryEscape(s) }
