package main

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/killswitch"
)

// Настройки переживают перезапуск, а испорченный файл даёт умолчания, а не
// отказ подключаться.
func TestSettingsSurviveRestartAndBrokenFileGivesDefaults(t *testing.T) {
	dir := t.TempDir()
	if got := loadSettings(dir); got != (pcSettings{}) {
		t.Fatalf("без файла: %+v", got)
	}
	want := pcSettings{BypassRussian: true, LANOutside: true, DNS: "9.9.9.9", DNSPlain: true, Fragment: true, IPv6Off: true, ReportsOff: true, KillSwitch: true}
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

// Что мост ведёт мимо туннеля как домашнюю сеть, kill switch тоже пропускает:
// иначе «домашняя сеть мимо туннеля» с ним молча переставала бы работать.
func TestKillSwitchLetsThroughTheWholeHomeNetwork(t *testing.T) {
	for _, s := range []string{"10.0.0.0", "10.255.255.255", "172.16.0.0", "172.31.255.255", "192.168.0.0", "192.168.255.255", "fc00::", "fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"} {
		ip := netip.MustParseAddr(s)
		if !homeNetwork(ip) {
			t.Fatalf("%s уже не домашняя сеть — тест устарел", s)
		}
		covered := false
		for _, p := range killswitch.HomePrefixes {
			covered = covered || p.Contains(ip)
		}
		if !covered {
			t.Errorf("мост ведёт %s мимо туннеля, а kill switch его закроет", s)
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

// Имена к известному резолверу шифруются по умолчанию — и у файла настроек,
// записанного прежней версией, где этого поля нет. Открытыми они идут, только
// если человек выбрал «как раньше» или резолвер — свой адрес.
func TestNamesAreEncryptedByDefaultExceptPlainChoiceOrOwnAddress(t *testing.T) {
	// Флаг -dns объявлен в файле для Windows, а проверка идёт на любой машине.
	const defaultDNS = "1.1.1.1:53"
	dir := t.TempDir()
	if err := os.WriteFile(settingsFile(dir), []byte(`{"dns":"94.140.14.14"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := loadSettings(dir)
	if !old.encrypted(old.resolver(defaultDNS)) {
		t.Fatal("файл прежней версии выключил шифрование имён")
	}
	if !(pcSettings{}).encrypted(defaultDNS) {
		t.Fatal("резолвер по умолчанию не шифруется")
	}
	if (pcSettings{DNSPlain: true}).encrypted(defaultDNS) {
		t.Fatal("«как раньше» не послушались")
	}
	own := pcSettings{DNS: "76.76.2.0"}
	if own.encrypted(own.resolver(defaultDNS)) {
		t.Fatal("свой адрес выдан за шифрованный")
	}
}

// Свой MTU — только в разумных пределах: меньше 1280 IPv6 не работает
// вовсе, больше 1500 у домашней сети не бывает. Ноль — «как по умолчанию».
// Выбор переживает перезапуск.
func TestOwnMTUStaysInSaneBoundsAndSurvivesRestart(t *testing.T) {
	for _, bad := range []uint32{1, 576, 1279, 1501, 9000} {
		if checkMTU(bad) == nil {
			t.Errorf("MTU %d принят", bad)
		}
	}
	for _, good := range []uint32{0, 1280, 1420, 1500} {
		if err := checkMTU(good); err != nil {
			t.Errorf("MTU %d отвергнут: %v", good, err)
		}
	}
	if got := (pcSettings{}).mtu(1500); got != 1500 {
		t.Errorf("без своего MTU взят %d вместо умолчания", got)
	}
	dir := t.TempDir()
	if err := saveSettings(dir, pcSettings{MTU: 1380}); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(dir).mtu(1500); got != 1380 {
		t.Errorf("после перезапуска MTU %d, а выбран 1380", got)
	}
}
