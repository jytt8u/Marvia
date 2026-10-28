package tunnel

import (
	"crypto/tls"
	"net"
	"testing"

	"github.com/jytt8u/marvia/internal/transport"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Без NetConn у vp1.Conn отклик ноды снова мерился бы в очереди туннеля.
var _ netConner = (*vp1.Conn)(nil)

// opaque прячет соединение под собой, как WebSocket.
type opaque struct{ net.Conn }

func TestSocketUnderTLSAndFragmenterIsFound(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			defer c.Close()
			_, _ = c.Read(make([]byte, 1))
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// tls.Client ничего не шлёт до первого чтения или записи — для проверки
	// обёрток хендшейк не нужен.
	layered := tls.Client(transport.FragmentHello(conn), &tls.Config{ServerName: "example.com"})
	if got := rawTCP(layered); got != conn.(*net.TCPConn) {
		t.Fatalf("под TLS и дробилкой найден %v, а не сокет", got)
	}
	if got := rawTCP(opaque{conn}); got != nil {
		t.Fatalf("обёртка, не отдающая соединение, всё равно вскрыта: %v", got)
	}
}
