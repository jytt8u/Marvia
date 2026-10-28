package tunbridge

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Правила по сайтам целиком, через мост: ответ на запрос имени, потом
// соединение приложения к адресу из ответа, и смотрим, какой дорогой оно
// ушло.

// tapeDialer — сторона развилки, которая помнит, куда её просили, и отдаёт
// дальний конец соединения в far, чтобы проверка видела, что дошло.
type tapeDialer struct {
	mu  sync.Mutex
	got []string
	far chan net.Conn
}

func newTape() *tapeDialer { return &tapeDialer{far: make(chan net.Conn, 8)} }

func (d *tapeDialer) note(kind string, t vp1.Address) {
	d.mu.Lock()
	d.got = append(d.got, kind+" "+t.String())
	d.mu.Unlock()
}

func (d *tapeDialer) seen() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.got...)
}

func (d *tapeDialer) DialTarget(_ context.Context, t vp1.Address) (net.Conn, error) {
	d.note("tcp", t)
	near, far := net.Pipe()
	d.far <- far
	return near, nil
}

func (d *tapeDialer) DialDatagrams(_ context.Context, t vp1.Address) (net.Conn, error) {
	d.note("udp", t)
	return nil, errors.New("датаграммы в проверке не нужны")
}

// zoneNames — резолвер с готовыми ответами: имя → адрес IPv4.
type zoneNames map[string][4]byte

func (z zoneNames) Exchange(_ context.Context, query []byte, _ bool) ([]byte, error) {
	var p dnsmessage.Parser
	head, err := p.Start(query)
	if err != nil {
		return nil, err
	}
	q, err := p.Question()
	if err != nil {
		return nil, err
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: head.ID, Response: true})
	_ = b.StartQuestions()
	_ = b.Question(q)
	_ = b.StartAnswers()
	if ip, ok := z[q.Name.String()]; ok {
		_ = b.AResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 60}, dnsmessage.AResource{A: ip})
	}
	return b.Finish()
}

func (zoneNames) Reset() {}

func askFor(t *testing.T, name string) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 7, RecursionDesired: true})
	_ = b.StartQuestions()
	_ = b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName(name), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

// bridgeFor поднимает мост с развилкой и правилами так же, как это делает
// программа на компьютере: правила мост берёт у развилки.
func bridgeFor(t *testing.T, sites *Sites, zone zoneNames) (*handler, *tapeDialer, *tapeDialer) {
	t.Helper()
	tunnel, local := newTape(), newTape()
	device, peer := net.Pipe()
	b, err := Start(Config{
		Device: device,
		Dialer: Split(tunnel, local, nil, sites),
		DNS:    "1.1.1.1:53",
		Names:  zone,
	})
	if err != nil {
		t.Fatalf("запуск моста: %v", err)
	}
	t.Cleanup(func() { _ = b.Close(); _ = peer.Close() })
	return b.handler, tunnel, local
}

// resolve задаёт мосту вопрос об имени, как его задал бы резолвер Windows.
func resolve(t *testing.T, h *handler, name string) {
	t.Helper()
	conn := newFakeUDPConn(dnsPort, string(askFor(t, name)))
	h.serveUDP(conn)
	select {
	case <-conn.written:
	case <-time.After(2 * time.Second):
		t.Fatalf("на вопрос о %s мост не ответил", name)
	}
}

// openTCP открывает соединение приложения к адресу через мост и отдаёт
// ближний конец — со стороны приложения.
func openTCP(h *handler, ip [4]byte, port uint16) net.Conn {
	app, bridge := net.Pipe()
	go h.HandleTCP(fakeTCPConn{Conn: bridge, id: stack.TransportEndpointID{
		LocalAddress: tcpip.AddrFrom4(ip),
		LocalPort:    port,
	}})
	return app
}

// waitFar ждёт, пока соединение дойдёт до одной из сторон развилки.
func waitFar(t *testing.T, tunnel, local *tapeDialer) (net.Conn, string) {
	t.Helper()
	select {
	case c := <-tunnel.far:
		return c, "туннель"
	case c := <-local.far:
		return c, "мимо"
	case <-time.After(3 * time.Second):
		t.Fatal("соединение не дошло ни до туннеля, ни мимо него")
		return nil, ""
	}
}

// Сайт из списка «мимо туннеля», узнанный по ответу на запрос имени, идёт
// мимо туннеля и на поддомене; сайт не из списка — через туннель.
func TestSiteRuleSendsAResolvedSiteAroundTheTunnelAndTheRestThroughIt(t *testing.T) {
	sites := NewSites()
	sites.Set([]string{"sberbank.ru"}, nil)
	bank, other := [4]byte{203, 0, 113, 7}, [4]byte{198, 51, 100, 9}
	h, tunnel, local := bridgeFor(t, sites, zoneNames{"online.sberbank.ru.": bank, "example.com.": other})

	resolve(t, h, "online.sberbank.ru.")
	resolve(t, h, "example.com.")

	app := openTCP(h, bank, 80)
	defer app.Close()
	far, way := waitFar(t, tunnel, local)
	defer far.Close()
	if way != "мимо" {
		t.Fatalf("сайт из списка ушёл: %s", way)
	}

	app2 := openTCP(h, other, 80)
	defer app2.Close()
	far2, way2 := waitFar(t, tunnel, local)
	defer far2.Close()
	if way2 != "туннель" {
		t.Fatalf("сайт не из списка ушёл: %s", way2)
	}
}

// hello — настоящее TLS-приветствие к имени, каким его пишет Go.
func hello(t *testing.T, name string) []byte {
	t.Helper()
	client, server := net.Pipe()
	defer server.Close()
	go func() {
		_ = tls.Client(client, &tls.Config{ServerName: name, InsecureSkipVerify: true}).Handshake()
		_ = client.Close()
	}()
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	head := make([]byte, 5)
	if _, err := io.ReadFull(server, head); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, int(head[3])<<8|int(head[4]))
	if _, err := io.ReadFull(server, body); err != nil {
		t.Fatal(err)
	}
	return append(head, body...)
}

// Браузер со своим DoH не спрашивает имён у моста — правило срабатывает по
// имени из приветствия, а само приветствие доходит до сайта байт в байт.
// QUIC к тому же адресу потом идёт той же дорогой.
func TestSiteRuleWorksByTLSNameWhenTheBrowserResolvesNamesItself(t *testing.T) {
	sites := NewSites()
	sites.Set([]string{"*.gosuslugi.ru"}, nil)
	h, tunnel, local := bridgeFor(t, sites, zoneNames{})
	ip := [4]byte{198, 51, 100, 20}
	msg := hello(t, "www.gosuslugi.ru")

	app := openTCP(h, ip, 443)
	defer app.Close()
	go func() { _, _ = app.Write(msg) }()
	far, way := waitFar(t, tunnel, local)
	defer far.Close()
	if way != "мимо" {
		t.Fatalf("сайт из списка по SNI ушёл: %s", way)
	}
	_ = far.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(far, got); err != nil {
		t.Fatalf("приветствие не дошло до сайта: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatal("приветствие дошло до сайта изменённым")
	}

	target := vp1.Address{Type: vp1.AtypIPv4, Host: netip.AddrFrom4(ip).String(), Port: 443}
	_, _ = h.dialer.DialDatagrams(context.Background(), target)
	if got := local.seen(); len(got) != 2 || got[1] != "udp "+target.String() {
		t.Fatalf("QUIC к тому же сайту ушёл не мимо туннеля: мимо %v, туннель %v", got, tunnel.seen())
	}
}

// Сайт, чьё приветствие называет другое имя, остаётся в туннеле, даже если
// адрес мост помнит за сайтом из списка: на общем адресе CDN имя из
// приветствия — правда об этом соединении, а память — лишь догадка.
func TestTLSNameOutranksAddressMemory(t *testing.T) {
	sites := NewSites()
	sites.Set([]string{"sberbank.ru"}, nil)
	ip := [4]byte{198, 51, 100, 30}
	h, tunnel, local := bridgeFor(t, sites, zoneNames{"sberbank.ru.": ip})
	resolve(t, h, "sberbank.ru.")

	app := openTCP(h, ip, 443)
	defer app.Close()
	go func() { _, _ = app.Write(hello(t, "news.example.com")) }()
	far, way := waitFar(t, tunnel, local)
	defer far.Close()
	if way != "туннель" {
		t.Fatalf("чужой сайт на общем адресе ушёл: %s", way)
	}
}

// Без правил мост не ждёт приветствия: протокол, где первым говорит
// сервер, не должен платить за возможность, которой человек не пользуется.
func TestWithoutSiteRulesTheBridgeDoesNotWaitForTheHello(t *testing.T) {
	h, tunnel, local := bridgeFor(t, NewSites(), zoneNames{})
	start := time.Now()
	app := openTCP(h, [4]byte{198, 51, 100, 40}, 443)
	defer app.Close()
	far, _ := waitFar(t, tunnel, local)
	defer far.Close()
	if waited := time.Since(start); waited >= helloWait/2 {
		t.Fatalf("без правил мост ждал приветствия %v", waited)
	}
}

// Имя находится и в приветствии, разрезанном на несколько записей TLS, —
// так его режут средства против DPI, и сайт его всё равно видит целиком.
func TestHelloNameSurvivesSplitRecords(t *testing.T) {
	msg := hello(t, "online.sberbank.ru")
	body := msg[5:]
	cut := 60 // посреди случайных байт, до имени
	split := []byte{0x16, msg[1], msg[2], 0, byte(cut)}
	split = append(split, body[:cut]...)
	rest := body[cut:]
	split = append(split, 0x16, msg[1], msg[2], byte(len(rest)>>8), byte(len(rest)))
	split = append(split, rest...)

	name, done := helloName(split)
	if !done || name != "online.sberbank.ru" {
		t.Fatalf("из разрезанного приветствия: %q, %v", name, done)
	}
	if name, done := helloName(split[:20]); done || name != "" {
		t.Fatalf("по обрывку решили раньше времени: %q", name)
	}
	if _, done := helloName([]byte("GET / HTTP/1.1\r\n")); !done {
		t.Fatal("не-TLS ждали как приветствие")
	}
}

// Память об адресе живёт не меньше seenFloor, даже если резолвер назвал
// минуту, — и кончается: через сутки адрес может быть уже чужим.
func TestAddressMemoryOutlivesShortTTLButNotForever(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	s := NewSites()
	s.now = func() time.Time { return now }
	s.Set([]string{"sberbank.ru"}, nil)
	ip := netip.MustParseAddr("203.0.113.7")
	s.learn("sberbank.ru", ip, time.Minute)

	now = now.Add(5 * time.Minute)
	if s.Route("", ip) != RouteDirect {
		t.Fatal("адрес забыт раньше seenFloor")
	}
	now = now.Add(seenCeiling)
	if s.Route("", ip) != RouteAny {
		t.Fatal("адрес помнится вечно")
	}
}
