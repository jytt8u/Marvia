//go:build windows

package main

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/tunbridge"
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

// Сайт, вписанный в настройках, уходит мимо туннеля сразу, без
// переподключения, и сохраняется; неверное имя возвращается ошибкой с
// этим именем и не сохраняется.
func TestSiteAddedInSettingsAppliesAtOnceAndIsSaved(t *testing.T) {
	t.Setenv("AppData", t.TempDir())
	c := &Controller{sites: tunbridge.NewSites()}
	ip := netip.MustParseAddr("203.0.113.7")

	bad := []string{"sberbank.ru", "сбер банк"}
	if _, err := c.UpdateSettings(SettingsPatch{SitesDirect: &bad}); err == nil || !strings.Contains(err.Error(), "сбер банк") {
		t.Fatalf("ошибка не называет неверное имя: %v", err)
	}
	if c.sites.Route("online.sberbank.ru", ip) != tunbridge.RouteAny {
		t.Fatal("неверная правка всё же применилась")
	}

	good := []string{"*.sberbank.ru"}
	view, err := c.UpdateSettings(SettingsPatch{SitesDirect: &good})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.SitesDirect) != 1 || view.SitesDirect[0] != "sberbank.ru" {
		t.Fatalf("окно видит %v", view.SitesDirect)
	}
	if c.sites.Route("online.sberbank.ru", ip) != tunbridge.RouteDirect {
		t.Fatal("правило не применилось без переподключения")
	}
	dir, _ := settingsDir()
	if got := loadSettings(dir).SitesDirect; len(got) != 1 || got[0] != "sberbank.ru" {
		t.Fatalf("сохранилось %v", got)
	}
}
