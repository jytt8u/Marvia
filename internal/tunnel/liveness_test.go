package tunnel

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/mux"
)

func TestCheckNeedsResponseFromEveryEstablishedSession(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	live, err := mux.Client(a)
	if err != nil {
		t.Fatal(err)
	}
	server, err := mux.Server(b)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	c, d := net.Pipe()
	defer d.Close()
	frozen, err := mux.Client(c)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPool(nil, 2, 32)
	defer p.Close()
	p.sessions = []*pooled{{sess: live}, {sess: frozen}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := p.Check(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ответ одной сессии скрыл молчание второй: %v", err)
	}
	if live.IsClosed() || frozen.IsClosed() {
		t.Fatal("отмена проверки закрыла рабочие потоки")
	}
	previous := p.sessions[1].check
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel2()
	if err := p.Check(ctx2); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("повторная проверка: %v", err)
	}
	if previous != p.sessions[1].check {
		t.Fatal("повтор проверки создал ещё один зависший запрос")
	}
}

func TestOpeningStreamStopsWaitingWhenContextEnds(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	session, err := mux.Client(a)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPool(nil, 1, 32)
	defer p.Close()
	p.sessions = []*pooled{{sess: session, retireAt: time.Now().Add(time.Hour)}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := p.Open(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("открытие потока пережило дедлайн: %v", err)
	}
	if session.IsClosed() {
		t.Fatal("отмена одного открытия закрыла всю сессию")
	}
}

func TestQuarantinedPoolDoesNotRedialFrozenAddress(t *testing.T) {
	p := NewPool(func(context.Context) (net.Conn, error) {
		t.Error("пул повторно дозвонился к исключённому адресу")
		return nil, errors.New("не должен вызываться")
	}, 1, 32)
	defer p.Close()
	p.Quarantine()
	if _, err := p.Open(context.Background()); err == nil {
		t.Fatal("исключённый пул продолжил выдавать потоки")
	}
}

func TestTrafficInOneSessionDoesNotHideStallInAnother(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	first, err := mux.Client(a)
	if err != nil {
		t.Fatal(err)
	}
	c, d := net.Pipe()
	defer d.Close()
	second, err := mux.Client(c)
	if err != nil {
		t.Fatal(err)
	}
	p := NewPool(nil, 2, 32)
	defer p.Close()
	p.sessions = []*pooled{
		{sess: first, activity: activity{pending: 1, received: time.Now()}},
		{sess: second, activity: activity{pending: 1, received: time.Now().Add(-time.Minute)}},
	}
	if p.Flowing(5 * time.Second) {
		t.Fatal("данные первой сессии скрыли молчание второй")
	}
	p.sessions[1].activity.received = time.Now()
	if !p.Flowing(5 * time.Second) {
		t.Fatal("данные обеих сессий не защищают рабочую передачу")
	}
}

func TestWaitingForAnotherHandshakeHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	p := NewPool(func(ctx context.Context) (net.Conn, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}, 1, 32)
	defer p.Close()
	first := make(chan error, 1)
	go func() { _, err := p.Open(context.Background()); first <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	second := make(chan error, 1)
	go func() { _, err := p.Open(ctx); second <- err }()
	select {
	case err := <-second:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ошибка очереди дозвона: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		_ = p.Close()
		<-first
		<-second
		t.Fatal("отмена проверки ждала чужого рукопожатия")
	}
	_ = p.Close()
	<-first
}
