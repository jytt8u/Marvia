package killswitch

import (
	"net/netip"
	"testing"
)

// flow — одно соединение глазами фильтра.
type flow struct {
	out, v6  bool
	iface    uint64
	loopback bool
	app      bool
	proto    uint8
	lport    uint16
	rport    uint16
	icmp     uint8
	remote   netip.Addr
	local    netip.Addr
	whatItIs string
}

// allowed решает так же, как WFP внутри нашего подслоя: из подошедших
// правил побеждает самое тяжёлое; не подошло ни одно — фильтру всё равно.
func allowed(rules []Rule, f flow) bool {
	best := -1
	permit := true
	for _, r := range rules {
		if !matches(r, f) {
			continue
		}
		if int(r.Weight) > best {
			best, permit = int(r.Weight), r.Permit
		}
	}
	return permit
}

func matches(r Rule, f flow) bool {
	if r.Out != f.out || r.V6 != f.v6 {
		return false
	}
	if r.Interface != 0 && r.Interface != f.iface {
		return false
	}
	if (r.Loopback && !f.loopback) || (r.ThisApp && !f.app) {
		return false
	}
	if r.Proto != 0 && r.Proto != f.proto {
		return false
	}
	if r.Proto == ProtoICMPv6 && r.ICMPType != 0 && r.ICMPType != f.icmp {
		return false
	}
	if (r.LocalPort != 0 && r.LocalPort != f.lport) || (r.RemotePort != 0 && r.RemotePort != f.rport) {
		return false
	}
	if r.Local.IsValid() && !r.Local.Contains(f.local) {
		return false
	}
	if len(r.Remote) == 0 {
		return true
	}
	for _, p := range r.Remote {
		if p.Contains(f.remote) {
			return true
		}
	}
	return false
}

const (
	tunnelLUID = 0x1234
	wifiLUID   = 0x77
)

var (
	web4 = netip.MustParseAddr("142.250.74.14")
	web6 = netip.MustParseAddr("2a00:1450:4010:c05::64")
	node = netip.MustParseAddr("203.0.113.7")
	ru   = netip.MustParseAddr("77.88.55.242")
	nas  = netip.MustParseAddr("192.168.1.20")
	ll6  = netip.MustParseAddr("fe80::1")
)

func check(t *testing.T, rules []Rule, want bool, flows ...flow) {
	t.Helper()
	for _, f := range flows {
		if got := allowed(rules, f); got != want {
			t.Errorf("%s: пропущено = %v, а должно быть %v", f.whatItIs, got, want)
		}
	}
}

// Главное обещание: при включённом kill switch ни одна программа не уходит
// мимо туннеля — ни по IPv4, ни по IPv6, ни наружу, ни внутрь.
func TestNothingButMarviaLeavesPastTheTunnel(t *testing.T) {
	for _, home := range []bool{false, true} {
		rules := Plan(Config{Tunnel: tunnelLUID, Home: home})
		check(t, rules, false,
			flow{out: true, iface: wifiLUID, proto: 6, rport: 443, remote: web4, whatItIs: "браузер на сайт через Wi-Fi"},
			flow{out: true, v6: true, iface: wifiLUID, proto: 6, rport: 443, remote: web6, whatItIs: "браузер по IPv6 через Wi-Fi"},
			flow{out: true, iface: wifiLUID, proto: 17, rport: 443, remote: ru, whatItIs: "QUIC на российский сайт мимо моста"},
			flow{out: true, iface: wifiLUID, proto: 6, rport: 443, remote: node, whatItIs: "чужая программа на адрес ноды"},
			flow{out: false, iface: wifiLUID, proto: 6, lport: 3389, remote: web4, whatItIs: "входящее из интернета"},
		)
	}
}

// Сам туннель и то, что через него, работает: иначе kill switch был бы
// просто выключателем интернета.
func TestTunnelAndMarviaItselfStillWork(t *testing.T) {
	rules := Plan(Config{Tunnel: tunnelLUID})
	check(t, rules, true,
		flow{out: true, iface: tunnelLUID, proto: 6, rport: 443, remote: web4, whatItIs: "браузер через адаптер туннеля"},
		flow{out: true, v6: true, iface: tunnelLUID, proto: 6, rport: 443, remote: web6, whatItIs: "IPv6 через адаптер туннеля"},
		flow{out: true, iface: tunnelLUID, proto: 17, rport: 53, remote: netip.MustParseAddr("1.1.1.1"), whatItIs: "запрос имени к резолверу туннеля"},
		flow{out: true, iface: wifiLUID, app: true, proto: 6, rport: 443, remote: node, whatItIs: "Marvia к ноде"},
		flow{out: true, iface: wifiLUID, app: true, proto: 6, rport: 443, remote: ru, whatItIs: "мост Marvia на российский сайт"},
		flow{out: true, iface: wifiLUID, app: true, proto: 17, rport: 443, remote: node, whatItIs: "Marvia к ноде по QUIC"},
		flow{out: true, loopback: true, proto: 6, rport: 8080, remote: netip.MustParseAddr("127.0.0.1"), whatItIs: "окно к программе по петле"},
		flow{out: false, loopback: true, proto: 6, lport: 8080, remote: netip.MustParseAddr("127.0.0.1"), whatItIs: "программа принимает окно по петле"},
	)
}

// Пока закрыто, компьютер не теряет адрес в сети: иначе туннель потом
// подниматься не на чем, и человек остаётся без сети и после «Отключить».
func TestComputerKeepsItsAddressWhileClosed(t *testing.T) {
	rules := Plan(Config{Tunnel: tunnelLUID})
	check(t, rules, true,
		flow{out: true, iface: wifiLUID, proto: 17, lport: 68, rport: 67, remote: netip.MustParseAddr("255.255.255.255"), whatItIs: "DHCP: поиск сервера"},
		flow{out: true, iface: wifiLUID, proto: 17, lport: 68, rport: 67, remote: netip.MustParseAddr("192.168.1.1"), whatItIs: "DHCP: продление аренды у сервера"},
		flow{out: false, iface: wifiLUID, proto: 17, lport: 68, rport: 67, remote: netip.MustParseAddr("192.168.1.1"), whatItIs: "DHCP: ответ сервера"},
		flow{out: true, v6: true, iface: wifiLUID, proto: 17, lport: 546, rport: 547, local: ll6, remote: netip.MustParseAddr("ff02::1:2"), whatItIs: "DHCPv6: запрос"},
		flow{out: false, v6: true, iface: wifiLUID, proto: 17, lport: 546, rport: 547, local: ll6, remote: netip.MustParseAddr("fe80::2"), whatItIs: "DHCPv6: ответ"},
		flow{out: true, v6: true, iface: wifiLUID, proto: ProtoICMPv6, icmp: 133, remote: netip.MustParseAddr("ff02::2"), whatItIs: "NDP: поиск роутера"},
		flow{out: false, v6: true, iface: wifiLUID, proto: ProtoICMPv6, icmp: 134, remote: netip.MustParseAddr("fe80::2"), whatItIs: "NDP: объявление роутера"},
		flow{out: true, v6: true, iface: wifiLUID, proto: ProtoICMPv6, icmp: 135, remote: netip.MustParseAddr("fe80::2"), whatItIs: "NDP: поиск соседа"},
		flow{out: false, v6: true, iface: wifiLUID, proto: ProtoICMPv6, icmp: 136, remote: netip.MustParseAddr("fe80::2"), whatItIs: "NDP: ответ соседа"},
	)
	check(t, rules, false,
		flow{out: false, v6: true, iface: wifiLUID, proto: ProtoICMPv6, icmp: 134, remote: netip.MustParseAddr("2001:db8::1"), whatItIs: "объявление роутера из интернета"},
		flow{out: true, iface: wifiLUID, proto: 17, lport: 50000, rport: 67, remote: web4, whatItIs: "UDP на порт DHCP не с порта клиента"},
	)
}

// Домашняя сеть открыта, только когда человек сам вывел её мимо туннеля.
func TestHomeNetworkPassesOnlyWhenChosen(t *testing.T) {
	lan := []flow{
		{out: true, iface: wifiLUID, proto: 6, rport: 445, remote: nas, whatItIs: "сетевой диск"},
		{out: false, iface: wifiLUID, proto: 6, lport: 445, remote: nas, whatItIs: "сосед открывает папку компьютера"},
		{out: true, iface: wifiLUID, proto: 17, rport: 5353, remote: netip.MustParseAddr("224.0.0.251"), whatItIs: "поиск принтера (mDNS)"},
		{out: true, iface: wifiLUID, proto: 17, rport: 1900, remote: netip.MustParseAddr("239.255.255.250"), whatItIs: "поиск телевизора (SSDP)"},
		{out: true, v6: true, iface: wifiLUID, proto: 6, rport: 80, remote: ll6, whatItIs: "роутер по локальному IPv6"},
	}
	check(t, Plan(Config{Tunnel: tunnelLUID}), false, lan...)
	check(t, Plan(Config{Tunnel: tunnelLUID, Home: true}), true, lan...)
}

// Запросы имён мимо туннеля не уходят и при открытой домашней сети: роутер
// отдал бы их провайдеру.
func TestNamesNeverLeavePastTheTunnel(t *testing.T) {
	rules := Plan(Config{Tunnel: tunnelLUID, Home: true})
	check(t, rules, false,
		flow{out: true, iface: wifiLUID, proto: 17, rport: 53, remote: netip.MustParseAddr("192.168.1.1"), whatItIs: "имя у роутера по UDP"},
		flow{out: true, iface: wifiLUID, proto: 6, rport: 53, remote: netip.MustParseAddr("192.168.1.1"), whatItIs: "имя у роутера по TCP"},
		flow{out: true, v6: true, iface: wifiLUID, proto: 17, rport: 53, remote: ll6, whatItIs: "имя у роутера по IPv6"},
	)
	check(t, rules, true,
		flow{out: true, loopback: true, proto: 17, rport: 53, remote: netip.MustParseAddr("127.0.0.1"), whatItIs: "свой резолвер на петле"},
	)
}

// Без адаптера правила «через туннель» нет вовсе: условие с пустым LUID
// пропустило бы любой интерфейс.
func TestWithoutAdapterEverythingElseIsClosed(t *testing.T) {
	rules := Plan(Config{})
	for _, r := range rules {
		if r.Permit && r.Interface == 0 && !r.ThisApp && !r.Loopback && r.Proto == 0 && len(r.Remote) == 0 {
			t.Errorf("%q пропускает всё", r.Name)
		}
	}
	check(t, rules, false,
		flow{out: true, iface: 0, proto: 6, rport: 443, remote: web4, whatItIs: "сайт через неизвестный интерфейс"},
	)
}

// Запрет — последнее слово, и спорить ему не с кем: на одном уровне веса
// разрешение и запрет не стоят, иначе порядок решала бы система, а не мы.
func TestBlockIsTheLastWordAndNoWeightIsContested(t *testing.T) {
	rules := Plan(Config{Tunnel: tunnelLUID, Home: true})
	blocks := 0
	weights := map[[3]any]bool{}
	for _, r := range rules {
		if !r.Permit && r.Weight == weightBlock {
			blocks++
		}
		key := [3]any{r.Out, r.V6, r.Weight}
		if seen, ok := weights[key]; ok && seen != r.Permit {
			t.Errorf("вес %d: и разрешение, и запрет", r.Weight)
		}
		weights[key] = r.Permit
	}
	if blocks != 4 {
		t.Errorf("общих запретов %d, нужно 4: наружу и внутрь, IPv4 и IPv6", blocks)
	}
	for _, r := range rules {
		if r.Permit && r.Weight <= weightBlock {
			t.Errorf("%q не тяжелее общего запрета", r.Name)
		}
		if r.Hard && !r.ThisApp {
			t.Errorf("%q — неотменяемое разрешение не для своего процесса", r.Name)
		}
	}
}
