//go:build windows

package main

import (
	"net/netip"
	"testing"
)

// Список качается секундами, и человек успевает выключить обход, пока он
// идёт. Опоздавшая загрузка не должна включить обход обратно.
func TestLateListDoesNotTurnBypassBackOn(t *testing.T) {
	dir := t.TempDir()
	if err := saveRuRoutes(dir, []string{"77.88.0.0/18"}); err != nil {
		t.Fatal(err)
	}
	yandex := netip.MustParseAddr("77.88.55.242")

	c := &Controller{}
	if n := c.applyRussian(dir); n != 0 || c.around(yandex) {
		t.Fatalf("выключенный обход увёл %d подсетей", n)
	}
	c.settings.BypassRussian = true
	if n := c.applyRussian(dir); n != 1 || !c.around(yandex) {
		t.Fatalf("включённый обход увёл %d подсетей", n)
	}
	if c.around(netip.MustParseAddr("8.8.8.8")) {
		t.Error("заграничный адрес пошёл мимо туннеля")
	}
}
