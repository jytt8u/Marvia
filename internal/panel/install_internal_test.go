package panel

import (
	"strings"
	"testing"
)

// Адрес панели без -sub-base берётся из заголовка Host, а в нём допустима
// одинарная кавычка. Строка установщика не должна из-за неё обрываться.
func TestPanelAddressWithQuoteStaysInsideInstallerString(t *testing.T) {
	out, err := renderInstall("https://x';touch /tmp/p;'", "invite")
	if err != nil {
		t.Fatal(err)
	}
	want := `PANEL='https://x'\'';touch /tmp/p;'\'''`
	if !strings.Contains(string(out), want+"\n") {
		t.Fatalf("адрес панели вырвался из кавычек, ждали строку %s", want)
	}
}
