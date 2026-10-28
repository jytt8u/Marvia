package client

import (
	"testing"

	"github.com/jytt8u/marvia/internal/tunnel"
	"github.com/jytt8u/marvia/internal/vp1"
)

// TestVideoOverQUICStaysOutOfTheGameLane: UDP на 443 и 80 — это QUIC, то есть
// видео. В игровую полосу ему нельзя: туда переехала бы закачка со своими
// скачками, ради которых полоса и заведена.
func TestVideoOverQUICStaysOutOfTheGameLane(t *testing.T) {
	d := &Dialer{pool: tunnel.NewPool(nil, 0, 0), game: true}
	defer d.Close()

	if d.laneFor(vp1.Address{Port: 443}, vp1.KindUDP) != d.pool {
		t.Error("QUIC на 443 ушёл в игровую полосу")
	}
	if d.laneFor(vp1.Address{Port: 80}, vp1.KindUDP) != d.pool {
		t.Error("UDP на 80 ушёл в игровую полосу")
	}
	if d.laneFor(vp1.Address{Port: 27015}, vp1.KindTCP) != d.pool {
		t.Error("TCP ушёл в игровую полосу")
	}
	game := d.laneFor(vp1.Address{Port: 27015}, vp1.KindUDP)
	if game == d.pool {
		t.Fatal("игровой UDP остался в общем пуле")
	}
	if d.laneFor(vp1.Address{Port: 3074}, vp1.KindUDP) != game {
		t.Error("второй игре завели ещё одну полосу")
	}
}
