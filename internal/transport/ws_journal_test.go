package transport

import (
	"bytes"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestWSNodeJournalDoesNotKeepVisitorAddresses — нода на WebSocket поверх
// TLS не пишет в журнал, с какого адреса сорвалось рукопожатие.
//
// Стандартный журнал net/http отвечает на это строкой «TLS handshake error
// from <адрес>». У ноды, принимающей покупателей напрямую, такая строка —
// запись об адресе покупателя, которых нода обещает не хранить.
func TestWSNodeJournalDoesNotKeepVisitorAddresses(t *testing.T) {
	var buf lockedBuffer
	was, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(was); log.SetFlags(flags) })

	cert, err := SelfSignedCertificate("node.example")
	if err != nil {
		t.Fatal(err)
	}
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := ListenWS(inner, WSConfig{Path: "/tunnel", Certificate: &cert})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	conn, err := net.Dial("tcp", inner.Addr().String())
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
		t.Fatalf("в журнале ноды адрес того, кто приходил: %q", got)
	}
}

// lockedBuffer — журнал пишет из горутины сервера, а читает тест.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
