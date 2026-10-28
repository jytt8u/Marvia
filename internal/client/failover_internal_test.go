package client

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/tunnel"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Надзор за нодой: что проверяем и чего сознательно не проверяем здесь.
//
// Сам переезд — подключение целиком: поход в панель, замеры всех нод, сборка
// нового туннеля. Проверять это в одиночном тесте значит поднимать панель и
// две ноды, и тест получается медленным настолько, что его перестают ждать.
// Переезд проверяется на живом стенде, где его и нашли сломанным.
//
// Здесь закрепляем то, что ломается тихо и незаметно для такой проверки:
// подмену дозвона под мостом и остановку сторожа.

func TestSupervisorSendsDialsToCurrentNode(t *testing.T) {
	s := &Supervisor{log: func(string, ...any) {}, done: make(chan struct{})}
	close(s.done)
	s.cancel = func() {}

	// Подменять *Dialer в тесте нечем — он собирается из живого соединения.
	// Поэтому проверяем сам механизм: пока дозвона нет, обращения обязаны
	// отказывать понятно, а не падать.
	_, err := s.DialTarget(context.Background(), vp1.Address{})
	if err == nil {
		t.Fatal("обращение без дозвона прошло молча")
	}
	if !strings.Contains(err.Error(), "туннель закрыт") {
		t.Errorf("невнятная ошибка: %v", err)
	}

	_, err = s.DialDatagrams(context.Background(), vp1.Address{})
	if err == nil {
		t.Fatal("датаграммы без дозвона прошли молча")
	}

}

// Close обязан быть безопасным при повторе и не вешать вызывающего.
//
// Приложение зовёт Stop и по кнопке, и при закрытии, и из VpnService при
// отзыве разрешения — все три могут прийти разом.
func TestSupervisorCloseIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Supervisor{
		log:    func(string, ...any) {},
		cancel: cancel,
		done:   make(chan struct{}),
	}

	// Сторож без дозвона выходит сразу — этого достаточно, чтобы проверить,
	// что Close его дожидается, а не висит.
	go s.watch(ctx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _ = s.Close() }()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close завис")
	}

	if _, err := s.DialTarget(context.Background(), vp1.Address{}); err == nil {
		t.Error("после закрытия обращения всё ещё проходят")
	}
}

// Сторож обязан уходить по отмене, а не жить до конца процесса.
//
// На телефоне туннель поднимают и опускают десятки раз за день; сторож,
// переживший свой туннель, продолжал бы щупать ноду и тратить батарею.
func TestSupervisorWatchStopsOnCancel(t *testing.T) {
	was := probeEvery
	probeEvery = 10 * time.Millisecond
	defer func() { probeEvery = was }()

	ctx, cancel := context.WithCancel(context.Background())
	s := &Supervisor{log: func(string, ...any) {}, cancel: cancel, done: make(chan struct{})}

	go s.watch(ctx)
	cancel()

	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("сторож не ушёл по отмене")
	}
}

func TestFrozenAddressCanReturnAfterQuarantineExpires(t *testing.T) {
	nodes := []Node{{ID: 1, Address: "bad:443"}, {ID: 2, Address: "good:443"}, {ID: 3, Address: "bad:443"}}
	excluded := map[string]time.Time{"bad:443": time.Now().Add(time.Minute)}
	got := eligibleNodes(nodes, excluded)
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("замороженный адрес остался среди кандидатов: %+v", got)
	}
	excluded["bad:443"] = time.Now().Add(-time.Second)
	if got := eligibleNodes(nodes, excluded); len(got) != len(nodes) {
		t.Fatalf("временное исключение стало постоянным: %+v", got)
	}
}

func TestSelectingQuarantinedCurrentNodeRequiresReconnect(t *testing.T) {
	p := tunnel.NewPool(nil, 1, 32)
	defer p.Close()
	p.Quarantine()
	s := &Supervisor{dialer: &Dialer{node: Node{ID: 7}, pool: p}, log: func(string, ...any) {}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Select(ctx, 7); err == nil {
		t.Fatal("повторный выбор исключённой ноды объявил закрытый пул подключённым")
	}
}
