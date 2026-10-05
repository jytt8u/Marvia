package client

import (
	"context"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/vp1"
)

func TestNetworkChangeDiscardsOldSessionWithoutChangingChosenNode(t *testing.T) {
	key, _ := vp1.GenerateKeyPair()
	node := Node{ID: 7, Address: "127.0.0.1:1", PublicKey: vp1.EncodeKey(key.Public)}
	d, err := NewDialer(node, key, Options{})
	if err != nil {
		t.Fatal(err)
	}
	d.sub = Subscription{Nodes: []Node{node}}
	d.recordRTT(39 * time.Millisecond)
	s := &Supervisor{dialer: d, cfg: ConnectConfig{Key: key, Prefer: 7}}
	t.Cleanup(func() {
		if s.networkCancel != nil {
			s.networkCancel()
		}
		if s.dialer != nil {
			_ = s.dialer.Close()
		}
		_ = d.Close()
	})
	changed, ok := any(s).(interface{ NetworkChanged(bool) })
	if !ok {
		t.Fatal("смена сети не передаётся надзору: прежняя сессия ждёт плановой проверки")
	}
	changed.NetworkChanged(false)
	if s.Measurement().RTT != 0 {
		t.Fatal("после смены сети остался старый отклик")
	}
	if s.Node().ID != 7 || s.Selected() != 7 || len(s.Nodes()) != 1 {
		t.Fatal("смена сети потеряла ноду или подписку")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := d.pool.Open(ctx); err == nil || err.Error() != "пул закрыт" {
		t.Fatalf("прежний дозвон остался доступен: %v", err)
	}
	changed.NetworkChanged(true)
	if s.dialer == d {
		t.Fatal("новая сеть использует прежний дозвон")
	}
}

func TestOfflineSupervisorDoesNotStartMeasurements(t *testing.T) {
	key, _ := vp1.GenerateKeyPair()
	node := Node{ID: 7, Address: "127.0.0.1:1", PublicKey: vp1.EncodeKey(key.Public)}
	d, err := NewDialer(node, key, Options{})
	if err != nil {
		t.Fatal(err)
	}
	d.sub = Subscription{Nodes: []Node{node}}
	s := &Supervisor{dialer: d, cfg: ConnectConfig{Key: key}}
	defer d.Close()
	s.NetworkChanged(false)
	defer s.dialer.Close()
	defer s.networkCancel()
	if results := s.Measure(context.Background()); len(results) != 0 {
		t.Fatal("при отключённой сети надзор продолжил мерить ноды")
	}
}
