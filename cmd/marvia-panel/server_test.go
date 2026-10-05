package main

import (
	"bytes"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/transport"
)

// TestPanelJournalDoesNotKeepVisitorAddresses — сорванное рукопожатие не
// записывает в журнал панели, с какого адреса приходили.
//
// К панели за подпиской ходят покупатели, и у кого-то из них связь рвётся
// посреди TLS. Стандартный журнал net/http писал на это «TLS handshake error
// from <адрес покупателя>», и journald панели становился списком их адресов.
func TestPanelJournalDoesNotKeepVisitorAddresses(t *testing.T) {
	var buf syncBuffer
	was, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(was); log.SetFlags(flags) })

	cert, err := transport.SelfSignedCertificate("panel.example")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newServer("", http.NotFoundHandler())
	go func() {
		_ = server.Serve(tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}}))
	}()
	t.Cleanup(func() { _ = server.Close() })

	// Вместо приветствия TLS — мусор, как от оборванного мобильного.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	visitor, _, _ := net.SplitHostPort(conn.LocalAddr().String())
	_, _ = conn.Write([]byte("это не приветствие TLS\r\n\r\n"))
	_ = conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(buf.String(), "TLS handshake error") {
		if time.Now().After(deadline) {
			t.Fatalf("сервер так и не сообщил о сорванном рукопожатии — проверять нечего: %q", buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := buf.String(); strings.Contains(got, visitor) {
		t.Fatalf("в журнале панели адрес того, кто приходил: %q", got)
	}
}

// syncBuffer — журнал пишет из горутины сервера, а читает тест.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
