//go:build windows

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/jytt8u/marvia/internal/client"
)

func TestCancelledAttemptCannotOverwriteLaterConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &Controller{state: StateConnected, node: client.Node{ID: 2}, log: newJournal()}
	c.connect(ctx, "marvia://неверная-ссылка", pcSettings{})
	if c.state != StateConnected || c.node.ID != 2 {
		t.Fatalf("отменённая попытка затёрла новое подключение: %s", c.state)
	}
}

type rejectedBackend struct {
	client.Backend
	closed bool
}

func (b *rejectedBackend) Close() error { b.closed = true; return nil }

func TestCancelledAttemptNeverCreatesAnAdapter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &Controller{connectCtx: ctx, state: StateConnecting, log: newJournal()}
	backend := &rejectedBackend{}
	if err := c.raiseTunnel(ctx, backend, nil, pcSettings{}); !errors.Is(err, context.Canceled) || !backend.closed {
		t.Fatalf("отменённая попытка продолжила создание адаптера: %v", err)
	}
}

func TestOldSupervisorCannotChangeANewSession(t *testing.T) {
	old, cancelOld := context.WithCancel(context.Background())
	defer cancelOld()
	current, cancelCurrent := context.WithCancel(context.Background())
	defer cancelCurrent()
	c := &Controller{connectCtx: current, state: StateConnected, node: client.Node{ID: 2}, log: newJournal()}
	c.moved(old, client.Node{ID: 3})
	c.stall(old, client.TroubleNoNode)
	c.finish(old, StateFailed, "старая ошибка")
	if c.state != StateConnected || c.node.ID != 2 || c.reason != "" || c.connectCtx != current {
		t.Fatal("старый надзор изменил новую сессию")
	}
	c.state = StateStalled
	c.recovered(old)
	if c.state != StateStalled {
		t.Fatal("старый надзор скрыл потерю связи новой сессии")
	}
}
