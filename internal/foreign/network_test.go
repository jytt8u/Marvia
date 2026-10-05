package foreign

import (
	"context"
	"github.com/jytt8u/marvia/internal/client"
	"testing"
	"time"
)

func TestRestoredNetworkRecreatesSameForeignNodeImmediately(t *testing.T) {
	probeHere(t)
	link, _ := vlessNode(t, "Выбранная")
	sub := ParseList([]byte(link))
	id := NodeID(sub.Links[0])
	recovered := make(chan struct{}, 4)
	s, _, err := Supervise(context.Background(), sub, id, client.Events{OnRecovered: func() { recovered <- struct{}{} }}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 3; i++ {
		old := s.engine
		s.NetworkChanged(false)
		if s.Measurement().RTT != 0 {
			t.Fatal("отклик прежней сети остался на экране")
		}
		if _, err := s.now(); err == nil {
			t.Fatal("нет сети, но дозвон разрешён")
		}
		s.NetworkChanged(true)
		select {
		case <-recovered:
		case <-time.After(3 * time.Second):
			t.Fatal("новая сеть ждёт полминуты до проверки")
		}
		if s.Node().ID != id || s.Selected() != id || s.engine == old {
			t.Fatal("пересоздание потеряло выбранную ноду или оставило старый движок")
		}
	}
	_ = s.Close()
	s.NetworkChanged(true)
	if s.engine != nil {
		t.Fatal("событие после остановки подняло движок заново")
	}
}

func TestForeignSupervisorDoesNotSelectOrMeasureWhileOffline(t *testing.T) {
	probeHere(t)
	first, _ := vlessNode(t, "Первая")
	second, _ := vlessNode(t, "Вторая")
	sub := ParseList([]byte(first + "\n" + second))
	id := NodeID(sub.Links[0])
	s, _, err := Supervise(context.Background(), sub, id, client.Events{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.NetworkChanged(false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if results := s.Measure(ctx); len(results) != 0 {
		t.Error("надзор мерит ноды при отключённой сети")
	}
	if err := s.Select(ctx, NodeID(sub.Links[1])); err == nil || s.Node().ID != id {
		t.Error("ручной выбор заменил движок после потери сети")
	}
	if s.Measurement().RTT != 0 {
		t.Error("замер вернул устаревший пинг отключённой сети")
	}
}
