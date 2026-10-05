package redact_test

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/redact"
)

// TestAddressNeverSurvivesTheJournal — ни один вид адреса не доходит до журнала.
//
// Строки взяты из того, что реально пишут net/http и ошибки сети: адрес там
// бывает голым, с портом, в квадратных скобках и сразу перед двоеточием фразы.
func TestAddressNeverSurvivesTheJournal(t *testing.T) {
	cases := map[string]string{
		"рукопожатие TLS":   "http: TLS handshake error from 203.0.113.7:51234: EOF",
		"IPv6 со скобками":  "http: TLS handshake error from [2001:db8::7]:51234: EOF",
		"IPv6 голый":        "dial udp 2001:db8::7: network is unreachable",
		"оба конца":         "write tcp 192.0.2.1:443->203.0.113.7:51234: write: broken pipe",
		"конец предложения": "пришли с 203.0.113.7.",
		"паника":            "http: panic serving 203.0.113.7:51234: runtime error",
	}
	for name, line := range cases {
		got := redact.Addresses(line)
		for _, leak := range []string{"203.0.113.7", "2001:db8::7", "51234"} {
			if strings.Contains(got, leak) {
				t.Errorf("%s: в журнал уехало %q: %q", name, leak, got)
			}
		}
		if !strings.Contains(got, redact.Placeholder) {
			t.Errorf("%s: адрес пропал без пометки, оператор не поймёт, что тут было: %q", name, got)
		}
	}
}

// TestRedactionKeepsTheRestOfTheLine — время, версии и причины остаются.
//
// Пометка вместо всего подряд, похожего на цифры, сделала бы журнал
// бесполезным: «ошибка в <адрес>» на месте времени ничего не объясняет.
func TestRedactionKeepsTheRestOfTheLine(t *testing.T) {
	for _, line := range []string{
		"копия базы снята в 12:30:45",
		"marvia-node v0.13.0 слушает :443",
		"сессия vp1 закрыта",
		"ключ deadbeef.cafe не подошёл",
	} {
		if got := redact.Addresses(line); got != line {
			t.Errorf("строка без адресов изменилась: %q → %q", line, got)
		}
	}
	if got := redact.Addresses("from 203.0.113.7:51234: EOF"); got != "from "+redact.Placeholder+": EOF" {
		t.Errorf("вокруг адреса потерялся смысл: %q", got)
	}
}

// TestLoggerWritesToTheProcessJournalWithoutAddresses — журнал для
// http.Server пишет туда же, куда и остальная программа, но уже без адресов.
func TestLoggerWritesToTheProcessJournalWithoutAddresses(t *testing.T) {
	var buf bytes.Buffer
	was, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(was); log.SetFlags(flags) })

	redact.Logger().Printf("http: TLS handshake error from %s: %v", "203.0.113.7:51234", "EOF")

	got := buf.String()
	if strings.Contains(got, "203.0.113.7") {
		t.Fatalf("адрес дошёл до журнала: %q", got)
	}
	if !strings.Contains(got, "TLS handshake error") {
		t.Fatalf("строка не дошла до журнала вовсе: %q", got)
	}
}
