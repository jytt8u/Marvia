package panel

import "testing"

// TestFlagIsAddedOnlyWhenTheCountryIsKnown: флаг — только у узнанной страны и
// только один; незнакомое название остаётся как есть, а не получает чужой
// флаг.
func TestFlagIsAddedOnlyWhenTheCountryIsKnown(t *testing.T) {
	cases := map[string]string{
		"Финляндия · Хельсинки · fi-1": "🇫🇮 Финляндия · Хельсинки · fi-1",
		"ОАЭ · Дубай":                  "🇦🇪 ОАЭ · Дубай",
		"Хельсинки, Финляндия":         "🇫🇮 Хельсинки, Финляндия",
		"Южная Корея · Сеул":           "🇰🇷 Южная Корея · Сеул",
		"Germany · Frankfurt":          "🇩🇪 Germany · Frankfurt",
		"🇳🇱 Нидерланды":                "🇳🇱 Нидерланды",
		"marvia-185":                   "marvia-185",
		"Луна · база 1":                "Луна · база 1",
	}
	for in, want := range cases {
		if got := flagged(in); got != want {
			t.Errorf("%q → %q, ожидалось %q", in, got, want)
		}
	}
}
