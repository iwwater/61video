package fixedtags

import (
	"strings"
	"unicode"
)

// Tag 是 system 标签的 label + 别名集合。systemtags 包在初始化时会读一份做种子。
type Tag struct {
	Label   string
	Aliases []string
}

var Labels = []string{"后入", "奶子", "口交", "臀", "人妻", "女大", "AV"}

var aliases = map[string][]string{
	"后入": {"后入", "後入", "后入式", "後入式", "后进", "後進", "后位", "後位", "背入", "背后", "后背", "背后式", "后背位", "狗爬", "狗爬式", "追尾", "doggy", "doggystyle", "doggy style", "doggy-style", "backshot", "back shot", "back-shot", "from behind", "rear entry"},
	"奶子": {"奶子", "奶", "大奶", "巨乳", "美乳", "爆乳", "丰乳", "丰胸", "大胸", "胸", "胸部", "胸器", "胸前", "揉胸", "揉奶", "揉乳", "双乳", "乳房", "乳头", "美胸", "boob", "boobs", "big boobs", "big-boobs", "tits", "titties", "titty", "breast", "breasts"},
	"口交": {"口交", "口爆", "口活", "口射", "吹箫", "吹萧", "深喉", "吞精", "含屌", "含鸡巴", "含龟头", "舔屌", "bj", "blowjob", "blow job", "oral", "oral sex", "oral-sex", "oralsex", "fellatio"},
	"臀":  {"臀", "屁股", "屁屁", "翘臀", "美臀", "肥臀", "巨臀", "蜜桃臀", "大屁股", "尻", "后庭", "後庭", "菊花", "肛", "肛交", "屁眼", "ass", "big ass", "big-ass", "butt", "big butt", "big-butt", "booty", "buttocks", "hip"},
	"人妻": {"人妻", "妻子", "老婆", "太太", "少妇", "少熟", "熟女", "已婚", "良家", "人妇", "人夫", "wife", "housewife", "married", "married woman", "young wife", "milf"},
	"女大": {"女大", "女大学生", "大学生", "女子大生", "大学", "女学生", "学生妹", "校花", "学妹", "校园", "大一", "大二", "大三", "大四", "college", "college student", "university", "university student", "campus", "coed"},
	"AV": {"AV", "JAV", "番号", "番號"},
}

// DefaultSystemTags 返回内置默认的 system 标签集合，settings 表为空时作种子用。
func DefaultSystemTags() []Tag {
	out := make([]Tag, 0, len(Labels))
	for _, label := range Labels {
		out = append(out, Tag{
			Label:   label,
			Aliases: append([]string(nil), aliases[label]...),
		})
	}
	return out
}

func AliasesFor(label string) []string {
	values := aliases[label]
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func MatchFilename(name string) []string {
	text := normalize(name)
	out := make([]string, 0, len(Labels))
	for _, label := range Labels {
		for _, alias := range aliases[label] {
			if text.contains(alias) {
				out = append(out, label)
				break
			}
		}
	}
	return out
}

type normalizedText struct {
	lower   string
	compact string
	tokens  map[string]struct{}
}

func normalize(s string) normalizedText {
	lower := strings.ToLower(s)
	var compact strings.Builder
	var spaced strings.Builder
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			compact.WriteRune(r)
			spaced.WriteRune(r)
			continue
		}
		spaced.WriteByte(' ')
	}

	tokens := make(map[string]struct{})
	for _, token := range strings.Fields(spaced.String()) {
		tokens[token] = struct{}{}
	}

	return normalizedText{
		lower:   lower,
		compact: compact.String(),
		tokens:  tokens,
	}
}

func (n normalizedText) contains(alias string) bool {
	lowerAlias := strings.ToLower(alias)
	compactAlias := compact(lowerAlias)
	if compactAlias == "" {
		return false
	}
	if isShortASCIIWord(compactAlias) && compactAlias == lowerAlias {
		_, ok := n.tokens[compactAlias]
		return ok
	}
	if strings.Contains(n.lower, lowerAlias) {
		return true
	}
	return strings.Contains(n.compact, compactAlias)
}

func compact(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isShortASCIIWord(s string) bool {
	if len(s) > 3 {
		return false
	}
	for _, r := range s {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}
