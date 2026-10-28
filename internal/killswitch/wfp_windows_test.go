//go:build windows && (amd64 || arm64)

package killswitch

import (
	"testing"

	"golang.org/x/sys/windows"
)

// Весь набор правил принимает настоящий WFP — а не только наш тест в
// rules_test.go. Общий запрет здесь не ставим: машина проверки живёт по
// сети, и закрывать её посреди прогона нельзя. Раскладку структур, условия
// всех видов (процесс, петля, интерфейс, порты, ICMP, подсети IPv4 и IPv6)
// и неотменяемое разрешение проверяет и без него.
func TestWindowsAcceptsEveryRuleWeMake(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("WFP требует прав администратора")
	}
	var rules []Rule
	for _, r := range Plan(Config{Tunnel: 1, Home: true}) {
		if r.Permit {
			rules = append(rules, r)
		}
	}
	g, err := Start(rules)
	if err != nil {
		t.Fatalf("WFP не принял правила: %v", err)
	}
	if err := g.Close(); err != nil {
		t.Fatalf("сессия не закрылась: %v", err)
	}
	if err := g.Close(); err != nil {
		t.Errorf("повторное закрытие: %v", err)
	}
	// Вторая сессия после первой: правила снялись вместе с ней и не мешают.
	g, err = Start(rules)
	if err != nil {
		t.Fatalf("повторная установка: %v", err)
	}
	_ = g.Close()
}
