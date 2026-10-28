package fallback

import (
	"bytes"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// visitor — соединение с заданным адресом собеседника: net.Pipe называет
// оба конца «pipe», а проверять надо настоящий адрес.
type visitor struct {
	net.Conn
	remote net.Addr
}

func (v visitor) RemoteAddr() net.Addr { return v.remote }

// TestCoverSiteWritesNothingAboutVisitors — сайт-прикрытие не оставляет в
// журнале ноды адреса тех, кто на него попал.
//
// Сюда приходят не только сканеры, но и свои покупатели с истёкшей подпиской.
// Пустой ErrorLog у http.Server молчанием не был: net/http писал в
// стандартный журнал «http: panic serving <адрес>», и строка о сбое прикрытия
// становилась записью о том, кто к ноде приходил.
func TestCoverSiteWritesNothingAboutVisitors(t *testing.T) {
	var buf bytes.Buffer
	was, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(was); log.SetFlags(flags) })

	h := &Handler{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("сбой прикрытия")
	})}

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Serve(visitor{Conn: server, remote: &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 51234}})
	}()

	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = client.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
	_, _ = io.Copy(io.Discard, client)
	_ = client.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("прикрытие не отпустило соединение")
	}

	if got := buf.String(); strings.Contains(got, "203.0.113.7") {
		t.Fatalf("в журнале ноды адрес посетителя прикрытия: %q", got)
	}
}
