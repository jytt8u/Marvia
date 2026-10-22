package panel

import "testing"

// TestEveryCountryNameDrawsItsFlag: страну, которую панель подставила по
// адресу, сама же панель узнаёт в названии и ставит флаг для Happ и Hiddify.
func TestEveryCountryNameDrawsItsFlag(t *testing.T) {
	for code, name := range countryNames {
		if got := countryCodeOf(name + " · нода"); got != code {
			t.Errorf("%s (%s) узнаётся как %q", name, code, got)
		}
	}
}
