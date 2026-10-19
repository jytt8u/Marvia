//go:build windows

package main

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

// slowBackend — ядро, у которого вторая нода отвечает, только когда тест
// разрешит: так видно, что окно показывает, пока замер ещё идёт.
type slowBackend struct {
	nodes   []client.Node
	release chan struct{}
}

func (b *slowBackend) DialTarget(context.Context, vp1.Address) (net.Conn, error) {
	return nil, errors.New("нет")
}
func (b *slowBackend) DialDatagrams(context.Context, vp1.Address) (net.Conn, error) {
	return nil, errors.New("нет")
}
func (b *slowBackend) Node() client.Node    { return b.nodes[0] }
func (b *slowBackend) Nodes() []client.Node { return b.nodes }
func (b *slowBackend) Measurement() client.Measurement {
	return client.Measurement{Node: b.nodes[0], Latency: 30 * time.Millisecond, RTT: 30 * time.Millisecond}
}
func (b *slowBackend) Measure(context.Context) []client.Measurement { return nil }
func (b *slowBackend) Ping(context.Context) (time.Duration, error)  { return 0, nil }
func (b *slowBackend) Select(context.Context, int64) error          { return nil }
func (b *slowBackend) Selected() int64                              { return 0 }
func (b *slowBackend) Subscription() client.Subscription            { return client.Subscription{Nodes: b.nodes} }
func (b *slowBackend) Close() error                                 { return nil }
func (b *slowBackend) MeasureEach(ctx context.Context, each func(client.Measurement)) {
	each(b.Measurement())
	select {
	case <-b.release:
		each(client.Measurement{Node: b.nodes[1], Latency: 90 * time.Millisecond, RTT: 90 * time.Millisecond})
	case <-ctx.Done():
	}
}

// TestRemeasureKeepsTheOldNumberUntilANewOneArrives: «Перемерить» не стирает
// числа — пока нода не ответила заново, в списке её прежний отклик с
// пометкой «меряю…», а не пустое место. Раньше список мигал так, будто
// ничего не мерилось.
func TestRemeasureKeepsTheOldNumberUntilANewOneArrives(t *testing.T) {
	b := &slowBackend{nodes: []client.Node{{ID: 1, Name: "fi-1"}, {ID: 2, Name: "de-1"}}, release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &Controller{state: StateConnected, dialer: b, connectCtx: ctx, log: newJournal(),
		measured: map[int64]client.Measurement{2: {Node: b.nodes[1], Latency: 80 * time.Millisecond, RTT: 80 * time.Millisecond}}}

	c.StartMeasure()
	deadline := time.Now().Add(2 * time.Second)
	for {
		v := c.Nodes()
		var de NodeView
		for _, n := range v.Nodes {
			if n.ID == 2 {
				de = n
			}
		}
		if de.Pending {
			if de.MS == 0 {
				t.Fatal("прежнее число стёрто на время замера")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("вторая нода так и не стала «меряю…»")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(b.release)
	for c.Nodes().Measuring {
		time.Sleep(10 * time.Millisecond)
	}
	for _, n := range c.Nodes().Nodes {
		if n.ID == 2 && (n.Pending || n.MS != 90) {
			t.Fatalf("после замера: %+v", n)
		}
	}
}

// TestAutoMoveIsExplainedButAChoiceIsNot: надзор сам ушёл с замолчавшей
// ноды — окно получает, откуда; человек выбрал ноду сам — «перестала
// отвечать» было бы неправдой.
func TestAutoMoveIsExplainedButAChoiceIsNot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &slowBackend{nodes: []client.Node{{ID: 1, Name: "fi-1", Address: "1.1.1.1:443"}, {ID: 2, Name: "de-1", Address: "2.2.2.2:443"}}}
	c := &Controller{state: StateConnected, dialer: b, connectCtx: ctx, log: newJournal(), node: b.nodes[0], conflictsAt: time.Now()}
	c.quality.add(30)

	c.moved(ctx, b.nodes[1])
	s := c.Status()
	if s.MovedFrom != "fi-1" {
		t.Fatalf("причина переезда: %q", s.MovedFrom)
	}
	if len(s.Quality.Samples) != 0 {
		t.Fatal("отклик прежней ноды остался на графике новой")
	}

	c.mu.Lock()
	c.manualAt = time.Now()
	c.movedFrom = ""
	c.mu.Unlock()
	c.moved(ctx, b.nodes[0])
	if s := c.Status(); s.MovedFrom != "" {
		t.Fatalf("ручной выбор назван бедой: %q", s.MovedFrom)
	}
}
