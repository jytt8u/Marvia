package main

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Настройки переживают перезапуск, а испорченный файл даёт умолчания, а не
// отказ подключаться.
func TestSettingsSurviveRestartAndBrokenFileGivesDefaults(t *testing.T) {
	dir := t.TempDir()
	if got := loadSettings(dir); got != (pcSettings{}) {
		t.Fatalf("без файла: %+v", got)
	}
	want := pcSettings{BypassRussian: true}
	if err := saveSettings(dir, want); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(dir); got != want {
		t.Errorf("после перезапуска: %+v", got)
	}
	if err := os.WriteFile(settingsFile(dir), []byte("{сломано"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(dir); got != (pcSettings{}) {
		t.Errorf("испорченный файл: %+v", got)
	}
}

// Российский список хранится целиком, а через неделю считается устаревшим.
func TestRussianListIsKeptWholeAndAgesInAWeek(t *testing.T) {
	dir := t.TempDir()
	if list, at := loadRuRoutes(dir); list != nil || !at.IsZero() {
		t.Fatalf("без файла: %v %v", list, at)
	}
	if err := saveRuRoutes(dir, []string{"77.88.0.0/18", "87.240.128.0/18"}); err != nil {
		t.Fatal(err)
	}
	list, at := loadRuRoutes(dir)
	if len(list) != 2 || list[1] != "87.240.128.0/18" {
		t.Errorf("прочитано %v", list)
	}
	if ruRoutesStale(at, at.Add(6*24*time.Hour)) {
		t.Error("шестидневный список уже устарел")
	}
	if !ruRoutesStale(at, at.Add(7*24*time.Hour)) {
		t.Error("недельный список ещё свеж")
	}
	if _, err := os.Stat(filepath.Join(dir, ruRoutesFile+".tmp")); !os.IsNotExist(err) {
		t.Error("временный файл остался")
	}
}

// Домашняя сеть — это частные адреса; публичные и петля к ней не относятся.
func TestHomeNetworkIsPrivateAddressesOnly(t *testing.T) {
	for _, s := range []string{"10.0.0.5", "172.16.3.4", "172.31.255.255", "192.168.1.10", "fd00::1"} {
		if !homeNetwork(netip.MustParseAddr(s)) {
			t.Errorf("%s не считается домашней сетью", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "172.32.0.1", "100.64.0.1", "127.0.0.1", "77.88.55.242", "2a00::1"} {
		if homeNetwork(netip.MustParseAddr(s)) {
			t.Errorf("%s считается домашней сетью", s)
		}
	}
}

// Свой резолвер проверяется до сохранения, и локальный адрес отличается от
// опечатки: у них разные подсказки человеку.
func TestOwnResolverIsCheckedAndLocalOneIsNamedAsSuch(t *testing.T) {
	for _, s := range []string{"1.0.0.1", "77.88.8.8", "94.140.14.14"} {
		if err := checkDNS(s); err != nil {
			t.Errorf("%s отвергнут: %v", s, err)
		}
	}
	for _, s := range []string{"", "1.1.1", "1.1.1.1.1", "010.1.1.1", "256.1.1.1", "1.1.1.a", "::1", "1.1.1.1:53"} {
		if err := checkDNS(s); err != errDNSBad {
			t.Errorf("%q: %v, ожидалась опечатка", s, err)
		}
	}
	for _, s := range []string{"192.168.1.1", "10.0.0.1", "172.20.0.1", "127.0.0.1", "100.64.0.1", "169.254.1.1", "224.0.0.1"} {
		if err := checkDNS(s); err != errDNSLocal {
			t.Errorf("%q: %v, ожидался локальный", s, err)
		}
	}
}

// Выбранный резолвер уходит в подключение с портом 53, а испорченный выбор
// в файле откатывается на флаг, а не ломает имена.
func TestChosenResolverReachesTheTunnelAndJunkFallsBack(t *testing.T) {
	if got := (pcSettings{DNS: "9.9.9.9"}).resolver("1.1.1.1:53"); got != "9.9.9.9:53" {
		t.Errorf("выбор: %q", got)
	}
	if got := (pcSettings{}).resolver("1.1.1.1:53"); got != "1.1.1.1:53" {
		t.Errorf("без выбора: %q", got)
	}
	if got := (pcSettings{DNS: "192.168.0.1"}).resolver("1.1.1.1:53"); got != "1.1.1.1:53" {
		t.Errorf("испорченный выбор: %q", got)
	}
}
