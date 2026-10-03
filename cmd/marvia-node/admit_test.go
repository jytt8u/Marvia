package main

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/users"
	"github.com/jytt8u/marvia/internal/vp1"
)

// recorder запоминает всё, что клиент отправил ноде.
type recorder struct {
	net.Conn
	sent bytes.Buffer
}

func (r *recorder) Write(p []byte) (int, error) {
	r.sent.Write(p)
	return r.Conn.Write(p)
}

// serveOnce отдаёт ноде один конец трубы и ждёт, пока она его обслужит.
func serveOnce(d deps) (client net.Conn, done <-chan struct{}) {
	client, server := net.Pipe()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		serve(server, d)
	}()
	return client, finished
}

func wait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("нода не закончила с соединением")
	}
}

// TestRejectedHandshakeDoesNotHoldBuyersSlot — неудачное рукопожатие не
// занимает место покупателя.
//
// Ключ проверяется раньше, чем метка времени и повтор, и проверка ключа
// сразу засчитывает соединение. Если дальше рукопожатие срывалось — повтор
// записанного приветствия, сбитые часы, обрыв до ответа, — засчитанное
// соединение так и оставалось висеть до перезапуска ноды. Покупатель с
// лимитом в одно соединение после одного такого случая не входил больше
// никогда, а панель показывала его «на связи».
func TestRejectedHandshakeDoesNotHoldBuyersSlot(t *testing.T) {
	node, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	buyer, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := users.NewRegistry([]users.User{{
		Kind: users.KindVP1, Secret: vp1.EncodeKey(buyer.Public),
		Enabled: true, MaxConns: 1, Account: "1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	d := deps{static: node, guard: vp1.NewReplayGuard(vp1.ClockSkew), registry: registry}

	// Честное подключение, которое кто-то подслушал.
	conn, done := serveOnce(d)
	rec := &recorder{Conn: conn}
	if _, err := vp1.ClientHandshake(rec, buyer, node.Public); err != nil {
		t.Fatalf("первое подключение: %v", err)
	}
	_ = conn.Close()
	wait(t, done)

	// Повтор того же приветствия: нода обязана отказать и ничего не занять.
	conn, done = serveOnce(d)
	go func() {
		_, _ = conn.Write(rec.sent.Bytes())
		buf := make([]byte, 512)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	wait(t, done)
	_ = conn.Close()

	// Сам покупатель снова входит.
	conn, done = serveOnce(d)
	defer func() { _ = conn.Close(); wait(t, done) }()
	if _, err := vp1.ClientHandshake(conn, buyer, node.Public); err != nil {
		t.Fatalf("после отвергнутого повтора покупатель не вошёл: %v", err)
	}
}
