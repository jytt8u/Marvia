package panel

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Флаг в названии ноды для чужих клиентов.
//
// Happ, Hiddify, v2rayTun и v2rayNG рисуют флаг, только если название ноды в
// подписке начинается с эмодзи флага: «🇫🇮 Финляндия». Отдельного поля для
// страны в ссылке vless:// нет, а по тексту «Финляндия» они флаг не
// подбирают — список стран в Happ был серым столбиком названий.
//
// Словарь — тот же, что у Android (Nodes.kt, Flags): страну продавец пишет
// руками, и незнакомое название остаётся без флага. Не тот флаг хуже, чем
// никакого — человек выберет страну по картинке и уедет не туда.

// flagged — название с эмодзи флага впереди, если страну узнали и флага там
// ещё нет.
func flagged(title string) string {
	if startsWithFlag(title) {
		return title
	}
	code := countryCodeOf(title)
	if code == "" {
		return title
	}
	return flagEmoji(code) + " " + title
}

// flagEmoji — две буквы кода особыми знаками: так флаг и устроен в юникоде.
func flagEmoji(code string) string {
	const base = 0x1F1E6
	return string([]rune{base + rune(code[0]-'a'), base + rune(code[1]-'a')})
}

func startsWithFlag(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

// countryCodeOf узнаёт страну по названию: сначала первая часть до «·»
// целиком («Финляндия · Хельсинки»), потом название страны словом внутри
// («Хельсинки, Финляндия»). Длинные названия проверяются раньше коротких:
// «Южная Корея» должна победить «Корею».
func countryCodeOf(title string) string {
	head, _, _ := strings.Cut(title, "·")
	key := normalizeCountry(head)
	if code, ok := countryCodes[key]; ok {
		return code
	}
	if len(key) == 2 {
		for _, c := range countryCodes {
			if c == key {
				return c
			}
		}
	}
	padded := " " + normalizeCountry(strings.NewReplacer("·", " ", ",", " ", "(", " ", ")", " ", "-", " ").Replace(title)) + " "
	for _, name := range countryNamesLongFirst {
		if strings.Contains(padded, " "+name+" ") {
			return countryCodes[name]
		}
	}
	return ""
}

func normalizeCountry(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ToLower(s), "ё", "е"))
}

var countryNamesLongFirst = func() []string {
	var names []string
	for name := range countryCodes {
		if utf8.RuneCountInString(name) > 2 {
			names = append(names, name)
		}
	}
	// Сначала длинные: «южная корея» раньше «корея». Равные — по алфавиту,
	// чтобы итог не зависел от порядка обхода словаря.
	sort.Slice(names, func(i, j int) bool {
		li, lj := utf8.RuneCountInString(names[i]), utf8.RuneCountInString(names[j])
		if li != lj {
			return li > lj
		}
		return names[i] < names[j]
	})
	return names
}()

// countryCodes — названия стран, как их пишут продавцы, и коды ISO.
var countryCodes = map[string]string{
	// Русские названия: продавцы пишут по-русски чаще всего.
	"оаэ": "ae",
	"объединенные арабские эмираты": "ae",
	"эмираты":             "ae",
	"нидерланды":          "nl",
	"голландия":           "nl",
	"финляндия":           "fi",
	"германия":            "de",
	"франция":             "fr",
	"великобритания":      "gb",
	"англия":              "gb",
	"сша":                 "us",
	"соединенные штаты":   "us",
	"канада":              "ca",
	"швеция":              "se",
	"швейцария":           "ch",
	"норвегия":            "no",
	"дания":               "dk",
	"исландия":            "is",
	"ирландия":            "ie",
	"испания":             "es",
	"италия":              "it",
	"португалия":          "pt",
	"греция":              "gr",
	"австрия":             "at",
	"бельгия":             "be",
	"люксембург":          "lu",
	"польша":              "pl",
	"чехия":               "cz",
	"словакия":            "sk",
	"словения":            "si",
	"венгрия":             "hu",
	"румыния":             "ro",
	"болгария":            "bg",
	"сербия":              "rs",
	"хорватия":            "hr",
	"молдова":             "md",
	"молдавия":            "md",
	"украина":             "ua",
	"латвия":              "lv",
	"литва":               "lt",
	"эстония":             "ee",
	"кипр":                "cy",
	"турция":              "tr",
	"израиль":             "il",
	"грузия":              "ge",
	"армения":             "am",
	"азербайджан":         "az",
	"казахстан":           "kz",
	"узбекистан":          "uz",
	"киргизия":            "kg",
	"кыргызстан":          "kg",
	"белоруссия":          "by",
	"беларусь":            "by",
	"россия":              "ru",
	"индия":               "in",
	"китай":               "cn",
	"тайвань":             "tw",
	"гонконг":             "hk",
	"япония":              "jp",
	"южная корея":         "kr",
	"корея":               "kr",
	"сингапур":            "sg",
	"малайзия":            "my",
	"индонезия":           "id",
	"таиланд":             "th",
	"вьетнам":             "vn",
	"австралия":           "au",
	"новая зеландия":      "nz",
	"бразилия":            "br",
	"аргентина":           "ar",
	"чили":                "cl",
	"мексика":             "mx",
	"юар":                 "za",
	"египет":              "eg",
	"катар":               "qa",
	"бахрейн":             "bh",
	"кувейт":              "kw",
	"саудовская аравия":   "sa",
	"иран":                "ir",
	"пакистан":            "pk",
	"сербия и черногория": "rs",

	// Английские: панель у части продавцов ведётся на английском.
	"uae":                  "ae",
	"united arab emirates": "ae",
	"netherlands":          "nl",
	"finland":              "fi",
	"germany":              "de",
	"france":               "fr",
	"united kingdom":       "gb",
	"great britain":        "gb",
	"uk":                   "gb",
	"usa":                  "us",
	"united states":        "us",
	"canada":               "ca",
	"sweden":               "se",
	"switzerland":          "ch",
	"norway":               "no",
	"denmark":              "dk",
	"iceland":              "is",
	"ireland":              "ie",
	"spain":                "es",
	"italy":                "it",
	"portugal":             "pt",
	"greece":               "gr",
	"austria":              "at",
	"belgium":              "be",
	"luxembourg":           "lu",
	"poland":               "pl",
	"czechia":              "cz",
	"czech republic":       "cz",
	"slovakia":             "sk",
	"slovenia":             "si",
	"hungary":              "hu",
	"romania":              "ro",
	"bulgaria":             "bg",
	"serbia":               "rs",
	"croatia":              "hr",
	"moldova":              "md",
	"ukraine":              "ua",
	"latvia":               "lv",
	"lithuania":            "lt",
	"estonia":              "ee",
	"cyprus":               "cy",
	"turkey":               "tr",
	"israel":               "il",
	"georgia":              "ge",
	"armenia":              "am",
	"azerbaijan":           "az",
	"kazakhstan":           "kz",
	"uzbekistan":           "uz",
	"kyrgyzstan":           "kg",
	"belarus":              "by",
	"russia":               "ru",
	"india":                "in",
	"china":                "cn",
	"taiwan":               "tw",
	"hong kong":            "hk",
	"japan":                "jp",
	"south korea":          "kr",
	"korea":                "kr",
	"singapore":            "sg",
	"malaysia":             "my",
	"indonesia":            "id",
	"thailand":             "th",
	"vietnam":              "vn",
	"australia":            "au",
	"new zealand":          "nz",
	"brazil":               "br",
	"argentina":            "ar",
	"chile":                "cl",
	"mexico":               "mx",
	"south africa":         "za",
	"egypt":                "eg",
	"qatar":                "qa",
	"bahrain":              "bh",
	"kuwait":               "kw",
	"saudi arabia":         "sa",
	"iran":                 "ir",
	"pakistan":             "pk",
}
