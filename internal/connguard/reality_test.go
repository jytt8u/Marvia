package connguard_test

import (
	"bufio"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/connguard"
	"github.com/jytt8u/marvia/internal/transport"
	"github.com/jytt8u/marvia/internal/vp1"
)

// TestRealityWorksOverTheGuard — REALITY поверх обёртки работает: сканер
// получает настоящий сайт прикрытия. Библиотека REALITY приводит соединение
// к интерфейсу с CloseWrite без проверки, и обёртка без него уронила бы
// ноду на первом же госте — ровно здесь это и было бы видно.
func TestRealityWorksOverTheGuard(t *testing.T) {
	const body = "настоящий чужой сайт"
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(site.Close)
	dest, err := url.Parse(site.URL)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := transport.ListenReality(connguard.Listen(tcp, connguard.Config{}), transport.RealityConfig{
		Dest:        dest.Host,
		ServerNames: []string{"example.com"},
		PrivateKey:  pair.Private,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				if strings.Contains(err.Error(), "use of closed") {
					return
				}
				continue
			}
			_ = c.Close()
		}
	}()

	conn, err := net.DialTimeout("tcp", tcp.Addr().String(), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	probe := tls.Client(conn, &tls.Config{ServerName: "example.com", InsecureSkipVerify: true})
	if _, err := probe.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatalf("сканер не прошёл рукопожатие: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(probe), nil)
	if err != nil {
		t.Fatalf("ответ: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if !strings.Contains(string(got), body) {
		t.Fatalf("сканер получил не сайт прикрытия: %q", got)
	}
}
