// Package killswitch не пускает трафик компьютера мимо туннеля, пока туннель
// включён человеком, — даже если сам туннель лёг.
//
// Маршруты /1 забирают трафик в адаптер, но это договорённость, а не запрет:
// исчез адаптер — исчезли и маршруты, и программы тут же идут напрямую. Своя
// подсеть, программа, привязавшая сокет к Wi-Fi, рассылка запросов имён по
// всем адаптерам сразу — всё это обходит маршруты и при живом туннеле. Запрет
// умеет ставить только сетевой фильтр системы: на Windows это Windows
// Filtering Platform (WFP).
//
// Пакет делится на две половины. Здесь — чистая функция Plan: какие правила
// стоят и в каком порядке, без единого системного вызова, чтобы обещание «что
// пропускается» проверялось тестом на любой машине. В wfp_windows.go — их
// установка в WFP. Образец — tunnel/firewall из wireguard-windows (MIT); взять
// его библиотекой нельзя: он пропускает свой процесс по SID службы, а окно
// Marvia — обычная программа, не служба.
package killswitch

import (
	"net/netip"
	"strconv"
)

// Config — то, от чего зависит набор правил.
type Config struct {
	// Tunnel — LUID адаптера туннеля: всё, что идёт через него, пропускается.
	Tunnel uint64

	// Home — человек вывел домашнюю сеть мимо туннеля. Тогда частные адреса,
	// локальные адреса канала и рассылки пропускаются для всех программ:
	// принтер и сетевой диск не должны пропадать вместе с нодой. Без этого
	// закрыта и своя подсеть, которую Windows без фильтра ведёт мимо туннеля
	// маршрутом, — kill switch на то и запрет, а не маршрут.
	Home bool
}

// Rule — одно правило фильтра. Условия внутри правила складываются через «и»,
// адреса в Remote — через «или», как условия с одним полем в самом WFP.
type Rule struct {
	Name   string
	Permit bool

	// Weight — кто прав при споре: из подошедших правил решает самое тяжёлое.
	Weight uint8

	// Hard — разрешение, которое не могут отменить запреты чужих фильтров
	// ниже нашего (FWPM_FILTER_FLAG_CLEAR_ACTION_RIGHT). Только для своего
	// процесса: без связи с нодой туннель не поднимется вовсе.
	Hard bool

	// Out — исходящее соединение (ALE connect), иначе входящее (recv/accept).
	Out bool
	V6  bool

	// Interface — LUID адаптера, через который идёт соединение; 0 — любой.
	Interface uint64
	Loopback  bool // петля компьютера
	ThisApp   bool // сам процесс Marvia

	Proto      uint8  // 0 — любой
	LocalPort  uint16 // 0 — любой
	RemotePort uint16 // 0 — любой

	// ICMPType — тип ICMPv6 при Proto == ProtoICMPv6; 0 — любой.
	ICMPType uint8

	Remote []netip.Prefix // пусто — любой адрес
	Local  netip.Prefix   // нулевой — любой
}

const (
	ProtoUDP    = 17
	ProtoICMPv6 = 58
)

// Веса. Свой процесс выше всего: его соединения с нодой — то, ради чего
// туннель вообще есть. Запрет — ниже всего и без условий: он ловит всё, что не
// подошло ни под одно разрешение.
const (
	weightApp      = 15
	weightLoopback = 13
	weightSystem   = 12
	weightNames    = 11
	weightHome     = 10
	weightBlock    = 0
)

// Plan собирает правила kill switch.
//
// Пропускается:
//   - сам Marvia — куда угодно. Соединения с нодой, российские сайты и
//     домашняя сеть по развилке моста, запросы к панели — всё это делает наш
//     процесс, и разрешать их по адресам незачем: адрес ноды меняется при
//     переезде, а разрешение по адресу открыло бы его и для всех остальных
//     программ. Российский список (десятки тысяч подсетей) по той же причине
//     в фильтр не попадает: чужие программы доходят до этих сайтов через
//     адаптер и мост, то есть опять нашим процессом;
//   - адаптер туннеля — всё, что через него;
//   - петля — окно говорит с программой через 127.0.0.1;
//   - DHCP и NDP — иначе компьютер, пока закрыт, теряет адрес в сети, и
//     туннель потом подниматься не на чем;
//   - домашняя сеть, если человек вывел её мимо туннеля, — кроме запросов
//     имён: они закрыты отдельно.
//
// Всё остальное запрещено в обе стороны, IPv4 и IPv6.
func Plan(cfg Config) []Rule {
	var rules []Rule
	each := func(r Rule) {
		for _, v6 := range []bool{false, true} {
			for _, out := range []bool{true, false} {
				r.V6, r.Out = v6, out
				rules = append(rules, r)
			}
		}
	}

	each(Rule{Name: "Marvia: свой процесс", Permit: true, Weight: weightApp, Hard: true, ThisApp: true})
	each(Rule{Name: "Marvia: петля", Permit: true, Weight: weightLoopback, Loopback: true})
	// Без адаптера правило «через туннель» стало бы правилом «через любой
	// интерфейс» — его просто не ставим, и закрыто всё, кроме остального.
	if cfg.Tunnel != 0 {
		each(Rule{Name: "Marvia: адаптер туннеля", Permit: true, Weight: weightSystem, Interface: cfg.Tunnel})
	}
	rules = append(rules, dhcpAndNDP()...)
	// Запросы имён мимо туннеля не уходят никогда — так обещает и мост. Без
	// этого правила «домашняя сеть» пропустила бы их к роутеру, а он отдал бы
	// провайдеру список всех сайтов. Петля и адаптер весят больше: локальный
	// резолвер на 127.0.0.1 и резолвер туннеля работают.
	each(Rule{Name: "Marvia: имена мимо туннеля закрыты", Weight: weightNames, RemotePort: 53})
	if cfg.Home {
		rules = append(rules, home()...)
	}
	each(Rule{Name: "Marvia: всё прочее закрыто", Weight: weightBlock})

	return rules
}

var (
	broadcast4 = netip.MustParsePrefix("255.255.255.255/32")
	linkLocal6 = netip.MustParsePrefix("fe80::/10")
	// Получатели DHCPv6: все серверы и агенты в канале и на площадке.
	dhcp6Servers = []netip.Prefix{netip.MustParsePrefix("ff02::1:2/128"), netip.MustParsePrefix("ff05::1:3/128")}
	allRouters6  = netip.MustParsePrefix("ff02::2/128")
)

// dhcpAndNDP — то, без чего компьютер не держит адрес в сети.
//
// DHCPv4 наружу — на любой адрес, а не только широковещательно, как у
// wireguard-windows: продление аренды идёт серверу напрямую, и раз в
// несколько часов закрытый компьютер иначе остался бы без адреса. Цена —
// открытая пара портов 68→67, по которой ничего, кроме DHCP, не ходит.
func dhcpAndNDP() []Rule {
	sys := func(r Rule) Rule {
		r.Permit, r.Weight = true, weightSystem
		return r
	}
	rules := []Rule{
		sys(Rule{Name: "Marvia: DHCP, запрос", Out: true, Proto: ProtoUDP, LocalPort: 68, RemotePort: 67}),
		sys(Rule{Name: "Marvia: DHCP, ответ", Proto: ProtoUDP, LocalPort: 68, RemotePort: 67}),
		sys(Rule{Name: "Marvia: DHCPv6, запрос", Out: true, V6: true, Proto: ProtoUDP, LocalPort: 546, RemotePort: 547, Local: linkLocal6, Remote: dhcp6Servers}),
		sys(Rule{Name: "Marvia: DHCPv6, ответ", V6: true, Proto: ProtoUDP, LocalPort: 546, RemotePort: 547, Local: linkLocal6, Remote: []netip.Prefix{linkLocal6}}),
		// NDP: поиск роутера, его объявление и переадресация — только в
		// пределах канала; поиск соседа и ответ ему — в обе стороны.
		sys(Rule{Name: "Marvia: NDP 133", Out: true, V6: true, Proto: ProtoICMPv6, ICMPType: 133, Remote: []netip.Prefix{allRouters6}}),
		sys(Rule{Name: "Marvia: NDP 134", V6: true, Proto: ProtoICMPv6, ICMPType: 134, Remote: []netip.Prefix{linkLocal6}}),
		sys(Rule{Name: "Marvia: NDP 137", V6: true, Proto: ProtoICMPv6, ICMPType: 137, Remote: []netip.Prefix{linkLocal6}}),
	}
	for _, t := range []uint8{135, 136} {
		for _, out := range []bool{true, false} {
			rules = append(rules, sys(Rule{Name: "Marvia: NDP " + strconv.Itoa(int(t)), Out: out, V6: true, Proto: ProtoICMPv6, ICMPType: t}))
		}
	}
	return rules
}

// HomePrefixes — что считается домашней сетью для фильтра.
//
// Шире, чем развилка моста (только частные адреса): там решается, куда везти
// соединение, которое уже пришло в туннель, а здесь — что пропустить мимо
// него. Локальные адреса канала и рассылки маршрутами в туннель и не
// попадают — это обнаружение принтеров и камер (mDNS, SSDP), и без них
// «домашняя сеть мимо туннеля» под kill switch работала бы наполовину.
var HomePrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/4"),
	broadcast4,
	netip.MustParsePrefix("fc00::/7"),
	linkLocal6,
	netip.MustParsePrefix("ff00::/8"),
}

// home — домашняя сеть в обе стороны: сетевой диск открывают и с него.
func home() []Rule {
	var v4, v6 []netip.Prefix
	for _, p := range HomePrefixes {
		if p.Addr().Is4() {
			v4 = append(v4, p)
		} else {
			v6 = append(v6, p)
		}
	}
	var rules []Rule
	for _, out := range []bool{true, false} {
		rules = append(rules,
			Rule{Name: "Marvia: домашняя сеть", Permit: true, Weight: weightHome, Out: out, Remote: v4},
			Rule{Name: "Marvia: домашняя сеть", Permit: true, Weight: weightHome, Out: out, V6: true, Remote: v6},
		)
	}
	return rules
}
