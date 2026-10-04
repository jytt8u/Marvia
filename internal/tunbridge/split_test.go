package tunbridge_test

import (
	"context"
	"net"
	"net/netip"
	"testing"

	"github.com/jytt8u/marvia/internal/tunbridge"
	"github.com/jytt8u/marvia/internal/vp1"
)

// recordDialer помнит, куда его просили позвонить.
type recordDialer struct{ got []string }

func (d *recordDialer) DialTarget(_ context.Context, t vp1.Address) (net.Conn, error) {
	d.got = append(d.got, "tcp "+t.String())
	return nil, nil
}

func (d *recordDialer) DialDatagrams(_ context.Context, t vp1.Address) (net.Conn, error) {
	d.got = append(d.got, "udp "+t.String())
	return nil, nil
}

// Российский адрес идёт мимо туннеля и по TCP, и по UDP; всё прочее — в
// туннель.
func TestRussianAddressGoesAroundTheTunnelAndTheRestThroughIt(t *testing.T) {
	ru := netip.MustParsePrefix("77.88.0.0/18")
	var tunnel, local recordDialer
	d := tunbridge.Split(&tunnel, &local, ru.Contains)

	ctx := context.Background()
	_, _ = d.DialTarget(ctx, vp1.Address{Type: vp1.AtypIPv4, Host: "77.88.55.242", Port: 443})
	_, _ = d.DialDatagrams(ctx, vp1.Address{Type: vp1.AtypIPv4, Host: "77.88.55.242", Port: 443})
	_, _ = d.DialTarget(ctx, vp1.Address{Type: vp1.AtypIPv4, Host: "8.8.4.4", Port: 443})
	_, _ = d.DialTarget(ctx, vp1.Address{Type: vp1.AtypDomain, Host: "ya.ru", Port: 443})

	if len(local.got) != 2 || local.got[0] != "tcp 77.88.55.242:443" || local.got[1] != "udp 77.88.55.242:443" {
		t.Errorf("мимо туннеля: %v", local.got)
	}
	if len(tunnel.got) != 2 {
		t.Errorf("в туннель: %v", tunnel.got)
	}
}

// Запрос имени к российскому резолверу всё равно идёт в туннель: в открытую
// он выдал бы список посещённых сайтов.
func TestNameQueriesNeverGoAroundTheTunnel(t *testing.T) {
	var tunnel, local recordDialer
	d := tunbridge.Split(&tunnel, &local, func(netip.Addr) bool { return true })
	_, _ = d.DialDatagrams(context.Background(), vp1.Address{Type: vp1.AtypIPv4, Host: "77.88.8.8", Port: 53})
	_, _ = d.DialTarget(context.Background(), vp1.Address{Type: vp1.AtypIPv4, Host: "77.88.8.8", Port: 53})
	if len(local.got) != 0 || len(tunnel.got) != 2 {
		t.Errorf("мимо: %v, в туннель: %v", local.got, tunnel.got)
	}
}

// Решение спрашивается на каждое соединение: включённый на ходу обход
// действует сразу, без переподключения.
func TestSplitDecisionCanChangeWhileTheTunnelIsUp(t *testing.T) {
	on := false
	var tunnel, local recordDialer
	d := tunbridge.Split(&tunnel, &local, func(netip.Addr) bool { return on })
	target := vp1.Address{Type: vp1.AtypIPv4, Host: "77.88.55.242", Port: 443}
	_, _ = d.DialTarget(context.Background(), target)
	on = true
	_, _ = d.DialTarget(context.Background(), target)
	if len(tunnel.got) != 1 || len(local.got) != 1 {
		t.Errorf("в туннель: %v, мимо: %v", tunnel.got, local.got)
	}
}
