package mediatype

import "strings"

const (
	Video    = "video"
	Audio    = "audio"
	Image    = "image"
	Document = "document"
)

func Normalize(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case Audio:
		return Audio
	case Image:
		return Image
	case Document:
		return Document
	default:
		return Video
	}
}

// NormalizeListFilter 把 list 接口里的 media_type 查询参数归一化。
// 空字符串或非法值返回空串（调用方视为不过滤）。
func NormalizeListFilter(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case Video:
		return Video
	case Audio:
		return Audio
	case Image:
		return Image
	case Document:
		return Document
	default:
		return ""
	}
}

func FromExtension(ext string) string {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".mp3", ".m4a", ".aac", ".wav", ".flac", ".ogg", ".opus":
		return Audio
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".svg":
		return Image
	case ".pdf", ".epub", ".mobi", ".txt", ".azw", ".azw3", ".djvu":
		return Document
	default:
		return Video
	}
}

func IsAudioExtension(ext string) bool {
	return FromExtension(ext) == Audio
}

func IsImageExtension(ext string) bool {
	return FromExtension(ext) == Image
}

func IsDocumentExtension(ext string) bool {
	return FromExtension(ext) == Document
}
