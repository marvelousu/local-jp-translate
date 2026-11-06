package plamo

import (
	"sort"
	"strings"
)

// NormalizeLanguage accepts casing and common Chinese locale spellings while
// keeping the model's language codes separate from caller locale filenames.
func NormalizeLanguage(code string) string {
	code = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "_", "-"))
	switch code {
	case "zh-cn", "zh-sg":
		return "zh-hans"
	case "zh-tw", "zh-hk":
		return "zh-hant"
	}
	return code
}

// Language is a model language code and its Japanese display name.
type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// SupportedLanguages is the 53-language list from the official PLaMo 3
// Translate 0.8B GGUF model card, in the card's order.
var SupportedLanguages = []Language{
	{"ja", "日本語"},
	{"en", "英語"},
	{"zh-hans", "中国語（簡体字）"},
	{"zh-hant", "中国語（繁体字）"},
	{"ko", "韓国語"},
	{"ar", "アラビア語"},
	{"az", "アゼルバイジャン語"},
	{"be", "ベラルーシ語"},
	{"bg", "ブルガリア語"},
	{"bn", "ベンガル語"},
	{"ca", "カタルーニャ語"},
	{"ceb", "セブアノ語"},
	{"de", "ドイツ語"},
	{"es", "スペイン語"},
	{"et", "エストニア語"},
	{"eu", "バスク語"},
	{"fa", "ペルシャ語"},
	{"fr", "フランス語"},
	{"gu", "グジャラート語"},
	{"he", "ヘブライ語"},
	{"hi", "ヒンディー語"},
	{"hr", "クロアチア語"},
	{"hu", "ハンガリー語"},
	{"hy", "アルメニア語"},
	{"id", "インドネシア語"},
	{"it", "イタリア語"},
	{"jv", "ジャワ語"},
	{"ka", "ジョージア語"},
	{"kk", "カザフ語"},
	{"km", "クメール語"},
	{"lt", "リトアニア語"},
	{"lv", "ラトビア語"},
	{"ml", "マラヤーラム語"},
	{"mr", "マラーティー語"},
	{"ms", "マレー語"},
	{"my", "ミャンマー語"},
	{"ne", "ネパール語"},
	{"nl", "オランダ語"},
	{"pa", "パンジャーブ語"},
	{"pl", "ポーランド語"},
	{"ru", "ロシア語"},
	{"si", "シンハラ語"},
	{"sk", "スロバキア語"},
	{"sl", "スロベニア語"},
	{"sr", "セルビア語"},
	{"sw", "スワヒリ語"},
	{"ta", "タミル語"},
	{"te", "テルグ語"},
	{"th", "タイ語"},
	{"tl", "タガログ語"},
	{"ur", "ウルドゥー語"},
	{"uz", "ウズベク語"},
	{"vi", "ベトナム語"},
}

var supportedSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(SupportedLanguages))
	for _, lang := range SupportedLanguages {
		m[lang.Code] = struct{}{}
	}
	return m
}()

// IsSupported reports whether code is one of the official 53 language codes.
func IsSupported(code string) bool {
	_, ok := supportedSet[code]
	return ok
}

// LanguageCodes returns a sorted copy of all supported codes.
func LanguageCodes() []string {
	codes := make([]string, 0, len(SupportedLanguages))
	for _, lang := range SupportedLanguages {
		codes = append(codes, lang.Code)
	}
	sort.Strings(codes)
	return codes
}

// LanguageName returns the display name for code, or the code itself.
func LanguageName(code string) string {
	for _, lang := range SupportedLanguages {
		if lang.Code == code {
			return lang.Name
		}
	}
	return code
}
