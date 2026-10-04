package tunbridge

import (
	"context"
	"net"
	"net/netip"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Split — дозвон, который часть адресов ведёт мимо туннеля.
//
// На Android мимо туннеля адреса уводит система: VpnService умеет исключать
// маршруты, и до моста такие пакеты просто не доходят. На Windows так не
// выйдет без правки таблицы маршрутов всей машины — восемь тысяч записей,
// которые переживут падение программы и сломаются при первой смене Wi-Fi.
// Поэтому там все пакеты приходят в мост, а решение принимается здесь, на
// каждое соединение: адрес, для которого direct ответил true, получает
// соединение через local — обычную сеть компьютера, — остальные идут в
// туннель.
//
// Цена: обходящий трафик проходит через стек моста в пространстве
// пользователя, а не мимо него. Для сайтов банков и госуслуг это не заметно,
// а выигрыш — ни одного следа в системе после выхода.
//
// direct спрашивается на каждое соединение, поэтому его можно менять на ходу:
// включённый человеком обход действует на новые соединения сразу, без
// переподключения.
func Split(tunnel, local Dialer, direct func(netip.Addr) bool) Dialer {
	return &splitDialer{tunnel: tunnel, local: local, direct: direct}
}

type splitDialer struct {
	tunnel, local Dialer
	direct        func(netip.Addr) bool
}

func (d *splitDialer) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	if d.around(target) {
		return d.local.DialTarget(ctx, target)
	}
	return d.tunnel.DialTarget(ctx, target)
}

func (d *splitDialer) DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error) {
	if d.around(target) {
		return d.local.DialDatagrams(ctx, target)
	}
	return d.tunnel.DialDatagrams(ctx, target)
}

// around — идти ли к цели мимо туннеля.
//
// Запросы имён — никогда, даже к российскому резолверу: запрос имени в
// открытую выдаёт цензору весь список посещённых сайтов, и ради этого
// обещания мост и существует (см. Config.DNS). Человек, выбравший резолвер
// Яндекса, получит его ответы — но через туннель.
func (d *splitDialer) around(target vp1.Address) bool {
	if d.direct == nil || target.Port == dnsPort {
		return false
	}
	ip, err := netip.ParseAddr(target.Host)
	if err != nil {
		// Имя, а не адрес: так в мост цели не приходят, но решать по имени
		// здесь нечем — пусть идёт в туннель, как всё неизвестное.
		return false
	}
	return d.direct(ip.Unmap())
}

// DialerFunc собирает Dialer из двух функций — для обычной сети компьютера,
// которую мосту даёт тот, кто знает, как к ней привязаться.
type DialerFunc struct {
	Target    func(ctx context.Context, target vp1.Address) (net.Conn, error)
	Datagrams func(ctx context.Context, target vp1.Address) (net.Conn, error)
}

func (f DialerFunc) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	return f.Target(ctx, target)
}

func (f DialerFunc) DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error) {
	return f.Datagrams(ctx, target)
}
