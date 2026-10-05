package tunbridge

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/jytt8u/marvia/internal/vp1"
)

// fakeNames — резолвер, отвечающий без сети. Запоминает, что спросили и
// каким путём ответ поедет к приложению.
type fakeNames struct {
	mu      sync.Mutex
	queries []string
	udp     []bool
	resets  atomic.Int32
}

func (f *fakeNames) Exchange(_ context.Context, query []byte, udp bool) ([]byte, error) {
	f.mu.Lock()
	f.queries = append(f.queries, string(query))
	f.udp = append(f.udp, udp)
	f.mu.Unlock()
	return append([]byte("ответ на "), query...), nil
}

func (f *fakeNames) Reset() { f.resets.Add(1) }

// spyDialer — нода, которая запоминает каждую просьбу о соединении.
type spyDialer struct{ calls atomic.Int32 }

func (d *spyDialer) DialTarget(context.Context, vp1.Address) (net.Conn, error) {
	d.calls.Add(1)
	return nil, errors.New("к ноде ходить не должны")
}

func (d *spyDialer) DialDatagrams(context.Context, vp1.Address) (net.Conn, error) {
	d.calls.Add(1)
	return nil, errors.New("к ноде ходить не должны")
}

// TestNameLookupOverUDPNeverReachesTheNodeInTheOpen — перехваченный запрос
// имени отвечается через резолвер, а к ноде не уходит ни открытой
// датаграммой, ни открытым TCP, какой бы адрес приложение ни спросило.
func TestNameLookupOverUDPNeverReachesTheNodeInTheOpen(t *testing.T) {
	names := &fakeNames{}
	node := &spyDialer{}
	h := &handler{dialer: node, dns: "1.1.1.1:53", names: names}
	conn := newFakeUDPConn(dnsPort, "вопрос про имя")

	h.serveUDP(conn)

	select {
	case got := <-conn.written:
		if string(got) != "ответ на вопрос про имя" {
			t.Fatalf("приложению ушло %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("приложение осталось без ответа")
	}
	if n := node.calls.Load(); n != 0 {
		t.Fatalf("к ноде ушло %d открытых запросов", n)
	}
	names.mu.Lock()
	defer names.mu.Unlock()
	if len(names.udp) != 1 || !names.udp[0] {
		t.Fatal("резолвер не знает, что ответ поедет датаграммой и должен в неё влезть")
	}
}

// fakeTCPConn — соединение от стека на порт 53.
type fakeTCPConn struct {
	net.Conn
	id stack.TransportEndpointID
}

func (c fakeTCPConn) ID() stack.TransportEndpointID { return c.id }

// TestNameLookupOverTCPNeverReachesTheNodeInTheOpen — вопрос по TCP, которым
// приложение переспрашивает обрезанный ответ, тоже отвечает резолвер, и
// ответ целиком, без ограничения датаграммы.
func TestNameLookupOverTCPNeverReachesTheNodeInTheOpen(t *testing.T) {
	names := &fakeNames{}
	node := &spyDialer{}
	h := &handler{dialer: node, dns: "1.1.1.1:53", names: names}

	app, bridge := net.Pipe()
	defer app.Close()
	go h.HandleTCP(fakeTCPConn{Conn: bridge, id: stack.TransportEndpointID{
		LocalAddress: tcpip.AddrFrom4([4]byte{8, 8, 8, 8}),
		LocalPort:    dnsPort,
	}})

	_ = app.SetDeadline(time.Now().Add(2 * time.Second))
	query := []byte("вопрос по TCP про имя")
	if err := writeFramed(app, query); err != nil {
		t.Fatal(err)
	}
	got, err := readFramed(app)
	if err != nil {
		t.Fatalf("ответа по TCP нет: %v", err)
	}
	if string(got) != "ответ на "+string(query) {
		t.Fatalf("приложению ушло %q", got)
	}
	if n := node.calls.Load(); n != 0 {
		t.Fatalf("к ноде ушло %d открытых запросов", n)
	}
	names.mu.Lock()
	defer names.mu.Unlock()
	if len(names.udp) != 1 || names.udp[0] {
		t.Fatal("ответ по TCP ограничен размером датаграммы")
	}
}

// TestNodeChangeResetsResolver — после переезда резолвер забывает прежнюю
// ноду: её соединения мертвы, а кэш подбирался под её страну.
func TestNodeChangeResetsResolver(t *testing.T) {
	names := &fakeNames{}
	b := &Bridge{handler: &handler{names: names}}
	b.NodeChanged()
	if names.resets.Load() != 1 {
		t.Fatal("переезд не дошёл до резолвера")
	}
}
