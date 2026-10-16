//go:build windows

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/wintun"
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

func TestWindowJournalAndStateDoNotShowAccessSecrets(t *testing.T) {
	const secret = "windows-access-secret"
	links := []string{
		"marvia://" + secret + "@panel.example/sub/" + secret,
		"vless://" + secret + "@node.example:443",
	}
	for _, link := range links {
		journal := newJournal()
		journal.add("подключение: %s", link)
		if got := strings.Join(journal.snapshot(), "\n"); strings.Contains(got, secret) {
			t.Errorf("секрет остался в журнале окна: %q", got)
		}

		ctx := context.Background()
		controller := &Controller{connectCtx: ctx, log: journal}
		controller.finish(ctx, StateFailed, "подключение: "+link)
		if strings.Contains(controller.reason, secret) {
			t.Errorf("секрет остался в ошибке на экране: %q", controller.reason)
		}
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

func TestAProxySetAfterConnectingStillReachesTheWindow(t *testing.T) {
	// Hiddify ставит системный прокси и после нашего подключения; браузер
	// тогда теряет сайты, и окно обязано об этом сказать, а не молчать до
	// следующего включения.
	old := systemProxy
	t.Cleanup(func() { systemProxy = old })
	proxy := wintun.Proxy{}
	systemProxy = func() wintun.Proxy { return proxy }

	c := &Controller{state: StateConnected, node: client.Node{ID: 2}, log: newJournal(), proxyAt: time.Now()}
	if s := c.Status(); s.Proxy != "" {
		t.Fatalf("прокси нет, а окно его показывает: %q", s.Proxy)
	}
	proxy = wintun.Proxy{Server: "http://127.0.0.1:12334", Owner: "Hiddify"}
	c.proxyAt = time.Now().Add(-proxyRecheck)
	s := c.Status()
	if s.Proxy == "" || s.ProxyOwner != "Hiddify" || s.ProxyServer != "http://127.0.0.1:12334" {
		t.Fatalf("прокси, поставленный после подключения, не дошёл до окна: %+v", s)
	}
}
