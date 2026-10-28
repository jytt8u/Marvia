package securedns

import (
	"bytes"
	"context"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// fakeResolver — публичный резолвер с DoH, каким его видит клиент: HTTPS,
// HTTP/2, сертификат на имя.
type fakeResolver struct {
	srv *httptest.Server

	asked   atomic.Int32
	ttl     uint32
	records int

	mu    sync.Mutex
	sizes []int
	ids   []uint16
}

func newFakeResolver(t *testing.T) *fakeResolver {
	t.Helper()
	f := &fakeResolver{ttl: 300, records: 1}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	f.srv = srv
	return f
}

func (f *fakeResolver) serve(w http.ResponseWriter, r *http.Request) {
	f.asked.Add(1)
	if r.Method != http.MethodPost || r.URL.Path != "/dns-query" || r.Header.Get("Content-Type") != mediaType {
		http.Error(w, "не по RFC 8484", http.StatusBadRequest)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var m dnsmessage.Message
	if err := m.Unpack(body); err != nil || len(m.Questions) != 1 {
		http.Error(w, "не разобрался", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.sizes = append(f.sizes, len(body))
	f.ids = append(f.ids, m.Header.ID)
	f.mu.Unlock()

	out := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: m.Header.ID, Response: true, RecursionDesired: true, RecursionAvailable: true},
		Questions: m.Questions,
	}
	for i := range f.records {
		var body dnsmessage.ResourceBody = &dnsmessage.AResource{A: [4]byte{93, 184, 216, byte(i)}}
		if m.Questions[0].Type == dnsmessage.TypeHTTPS {
			// Запись HTTPS (RFC 9460) так, как её кладёт на провод резолвер:
			// приоритет 1, цель «.», alpn=h2.
			body = &dnsmessage.UnknownResource{Type: dnsmessage.TypeHTTPS, Data: []byte{0, 1, 0, 0, 1, 0, 3, 2, 'h', '2'}}
		}
		out.Answers = append(out.Answers, dnsmessage.Resource{
			Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Type: m.Questions[0].Type, Class: dnsmessage.ClassINET, TTL: f.ttl},
			Body:   body,
		})
	}
	// Ответ с добивкой, как у настоящих резолверов: приложению она не нужна.
	var opt dnsmessage.Resource
	_ = opt.Header.SetEDNS0(1232, dnsmessage.RCodeSuccess, false)
	opt.Body = &dnsmessage.OPTResource{Options: []dnsmessage.Option{{Code: optPadding, Data: make([]byte, 40)}}}
	out.Additionals = append(out.Additionals, opt)

	raw, _ := out.Pack()
	w.Header().Set("Content-Type", mediaType)
	_, _ = w.Write(raw)
}

func (f *fakeResolver) roots() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(f.srv.Certificate())
	return pool
}

// fakeTunnel — туннель глазами ноды: куда просят соединиться и все байты, что
// проходят через неё в обе стороны.
type fakeTunnel struct {
	target string

	mu     sync.Mutex
	dialed []netip.AddrPort
	seen   bytes.Buffer

	// dead — адреса, до которых нода не дозванивается: соединение висит,
	// пока не кончится терпение.
	dead map[netip.Addr]bool
}

func (n *fakeTunnel) dial(ctx context.Context, addr netip.AddrPort) (net.Conn, error) {
	n.mu.Lock()
	n.dialed = append(n.dialed, addr)
	dead := n.dead[addr.Addr()]
	n.mu.Unlock()
	if dead {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", n.target)
	if err != nil {
		return nil, err
	}
	return &spyConn{Conn: c, node: n}, nil
}

func (n *fakeTunnel) dials(addr netip.Addr) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	count := 0
	for _, d := range n.dialed {
		if d.Addr() == addr {
			count++
		}
	}
	return count
}

type spyConn struct {
	net.Conn
	node *fakeTunnel
}

func (c *spyConn) Read(p []byte) (int, error) {
	k, err := c.Conn.Read(p)
	c.node.mu.Lock()
	c.node.seen.Write(p[:k])
	c.node.mu.Unlock()
	return k, err
}

func (c *spyConn) Write(p []byte) (int, error) {
	c.node.mu.Lock()
	c.node.seen.Write(p)
	c.node.mu.Unlock()
	return c.Conn.Write(p)
}

var (
	chosenAddr   = netip.MustParseAddr("1.1.1.1")
	fallbackAddr = netip.MustParseAddr("9.9.9.9")
)

// testServers — выбранный и запасной. Сертификат проверочного сервера выдан
// на example.com, поэтому имя у обоих одно.
func testServers() []Server {
	return []Server{
		{Name: "Выбранный", Host: "example.com", Addrs: []netip.Addr{chosenAddr}, Path: "/dns-query"},
		{Name: "Запасной", Host: "example.com", Addrs: []netip.Addr{fallbackAddr}, Path: "/dns-query"},
	}
}

func newTestResolver(t *testing.T, f *fakeResolver, node *fakeTunnel, roots *x509.CertPool) *Resolver {
	t.Helper()
	node.target = f.srv.Listener.Addr().String()
	r, err := New(Config{Servers: testServers(), Dial: node.dial, RootCAs: roots, Attempt: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func question(t *testing.T, name string, id uint16, edns bool) []byte {
	t.Helper()
	m := dnsmessage.Message{
		Header: dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{{
			Name: dnsmessage.MustNewName(name), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET,
		}},
	}
	if edns {
		var opt dnsmessage.Resource
		_ = opt.Header.SetEDNS0(1232, dnsmessage.RCodeSuccess, false)
		opt.Body = &dnsmessage.OPTResource{}
		m.Additionals = append(m.Additionals, opt)
	}
	raw, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func unpack(t *testing.T, raw []byte) dnsmessage.Message {
	t.Helper()
	var m dnsmessage.Message
	if err := m.Unpack(raw); err != nil {
		t.Fatalf("ответ не разобрался: %v", err)
	}
	return m
}

// TestNameLookupGoesEncryptedAndNodeSeesOnlyResolverAddress — запрос имени
// через туннель уходит зашифрованным: нода видит, к какому резолверу
// соединение, но не видит спрошенного имени ни в одну сторону.
func TestNameLookupGoesEncryptedAndNodeSeesOnlyResolverAddress(t *testing.T) {
	f := newFakeResolver(t)
	node := &fakeTunnel{}
	r := newTestResolver(t, f, node, f.roots())

	raw, err := r.Exchange(context.Background(), question(t, "very-private-site.example.", 4242, false), true)
	if err != nil {
		t.Fatal(err)
	}
	m := unpack(t, raw)
	if m.Header.ID != 4242 || len(m.Answers) != 1 {
		t.Fatalf("ответ не тот: ID %d, записей %d", m.Header.ID, len(m.Answers))
	}

	node.mu.Lock()
	dialed := append([]netip.AddrPort(nil), node.dialed...)
	seen := node.seen.String()
	node.mu.Unlock()
	for _, d := range dialed {
		if d != netip.AddrPortFrom(chosenAddr, 443) {
			t.Fatalf("нода видела соединение не с резолвером: %s", d)
		}
	}
	if len(dialed) == 0 || len(seen) == 0 {
		t.Fatal("соединения с резолвером через туннель не было")
	}
	for _, leak := range []string{"very-private-site", "\x11very-private-site"} {
		if strings.Contains(seen, leak) {
			t.Fatalf("нода видит имя в открытую: %q", leak)
		}
	}
	if strings.Contains(seen, "dns-query") {
		t.Fatal("нода видит запрос HTTP, а не TLS")
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ids[0] != 0 {
		t.Errorf("ID на проводе %d, а по RFC 8484 — ноль", f.ids[0])
	}
}

// TestDeadResolverDoesNotLeaveYouOffline — недоступный резолвер не оставляет
// без интернета: берётся запасной, и следующие вопросы идут сразу к нему, не
// платя таймаут каждый раз.
func TestDeadResolverDoesNotLeaveYouOffline(t *testing.T) {
	f := newFakeResolver(t)
	node := &fakeTunnel{dead: map[netip.Addr]bool{chosenAddr: true}}
	r := newTestResolver(t, f, node, f.roots())

	if _, err := r.Exchange(context.Background(), question(t, "one.example.", 1, false), true); err != nil {
		t.Fatalf("при мёртвом выбранном резолвере имя не разрешилось: %v", err)
	}
	start := time.Now()
	if _, err := r.Exchange(context.Background(), question(t, "two.example.", 2, false), true); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 250*time.Millisecond {
		t.Errorf("второй вопрос снова ждал мёртвый резолвер: %v", took)
	}
	if got := node.dials(chosenAddr); got != 1 {
		t.Errorf("к мёртвому резолверу ходили %d раз, ждали один", got)
	}
	if node.dials(fallbackAddr) == 0 {
		t.Error("запасной резолвер не спрашивали")
	}
}

// TestForgedCertificateDoesNotFallBackToPlainDNS — подменённый сертификат не
// превращает запрос в открытый: нода, которая хочет видеть имена, не может
// добиться этого, просто сломав шифрование.
func TestForgedCertificateDoesNotFallBackToPlainDNS(t *testing.T) {
	f := newFakeResolver(t)
	node := &fakeTunnel{}
	r := newTestResolver(t, f, node, x509.NewCertPool())

	if _, err := r.Exchange(context.Background(), question(t, "very-private-site.example.", 7, false), true); err == nil {
		t.Fatal("ответ получен при сертификате, которому нельзя верить")
	}
	node.mu.Lock()
	defer node.mu.Unlock()
	for _, d := range node.dialed {
		if d.Port() != 443 {
			t.Fatalf("после отказа TLS запрос ушёл открытым на %s", d)
		}
	}
	if strings.Contains(node.seen.String(), "very-private-site") {
		t.Fatal("имя ушло ноде в открытую")
	}
}

// TestRepeatedQuestionIsAnsweredFromCacheUntilTTLRunsOut — повторный вопрос не
// идёт к резолверу, пока ответ жив; TTL в ответе из кэша уменьшен на
// прошедшее время, а ID — тот, что в новом вопросе.
func TestRepeatedQuestionIsAnsweredFromCacheUntilTTLRunsOut(t *testing.T) {
	f := newFakeResolver(t)
	node := &fakeTunnel{}
	r := newTestResolver(t, f, node, f.roots())
	clock := time.Now()
	r.now = func() time.Time { return clock }

	if _, err := r.Exchange(context.Background(), question(t, "cached.example.", 1, false), true); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(10 * time.Second)
	raw, err := r.Exchange(context.Background(), question(t, "CACHED.example.", 99, false), true)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.asked.Load(); got != 1 {
		t.Fatalf("резолвер спросили %d раз, ждали один", got)
	}
	m := unpack(t, raw)
	if m.Header.ID != 99 {
		t.Errorf("ответ из кэша с чужим ID %d", m.Header.ID)
	}
	if ttl := m.Answers[0].Header.TTL; ttl != 290 {
		t.Errorf("TTL из кэша %d, ждали 290", ttl)
	}

	clock = clock.Add(300 * time.Second)
	if _, err := r.Exchange(context.Background(), question(t, "cached.example.", 3, false), true); err != nil {
		t.Fatal(err)
	}
	if got := f.asked.Load(); got != 2 {
		t.Fatalf("истёкший ответ отдан из кэша: резолвер спросили %d раз", got)
	}

	// После переезда на другую ноду кэш забывается: адреса CDN подбирались
	// под прежнюю.
	r.Reset()
	if _, err := r.Exchange(context.Background(), question(t, "cached.example.", 4, false), true); err != nil {
		t.Fatal(err)
	}
	if got := f.asked.Load(); got != 3 {
		t.Fatalf("после переезда ответ взят из старого кэша: резолвер спросили %d раз", got)
	}
}

// TestQueryLengthDoesNotGiveAwayTheName — запросы добиты до одной длины:
// нода не угадает имя по размеру зашифрованной записи.
func TestQueryLengthDoesNotGiveAwayTheName(t *testing.T) {
	f := newFakeResolver(t)
	node := &fakeTunnel{}
	r := newTestResolver(t, f, node, f.roots())

	for _, name := range []string{"a.example.", "a-much-longer-name-of-some-site.example."} {
		if _, err := r.Exchange(context.Background(), question(t, name, 1, false), true); err != nil {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sizes[0] != f.sizes[1] || f.sizes[0]%paddingBlock != 0 {
		t.Fatalf("длины запросов %v выдают длину имени", f.sizes)
	}
}

// TestAnswerTooBigForDatagramAsksToRetryOverTCP — ответ, который не влезает в
// датаграмму приложения, приходит пустым с флагом TC, а по TCP — целиком; и
// приложению без EDNS не достаётся записи OPT, которой оно не просило.
func TestAnswerTooBigForDatagramAsksToRetryOverTCP(t *testing.T) {
	f := newFakeResolver(t)
	f.records = 40
	node := &fakeTunnel{}
	r := newTestResolver(t, f, node, f.roots())

	raw, err := r.Exchange(context.Background(), question(t, "big.example.", 5, false), true)
	if err != nil {
		t.Fatal(err)
	}
	m := unpack(t, raw)
	if !m.Header.Truncated || len(m.Answers) != 0 || len(raw) > classicUDP {
		t.Fatalf("большой ответ по UDP: TC=%v, записей %d, %d байт", m.Header.Truncated, len(m.Answers), len(raw))
	}

	raw, err = r.Exchange(context.Background(), question(t, "big.example.", 6, false), false)
	if err != nil {
		t.Fatal(err)
	}
	m = unpack(t, raw)
	if m.Header.Truncated || len(m.Answers) != 40 {
		t.Fatalf("по TCP ответ не целиком: TC=%v, записей %d", m.Header.Truncated, len(m.Answers))
	}
	if len(m.Additionals) != 0 {
		t.Fatalf("приложению без EDNS пришла запись OPT")
	}
}

// TestEDNSClientGetsOPTWithoutResolverPadding — приложение с EDNS получает
// OPT обратно, но без чужой добивки.
func TestEDNSClientGetsOPTWithoutResolverPadding(t *testing.T) {
	f := newFakeResolver(t)
	node := &fakeTunnel{}
	r := newTestResolver(t, f, node, f.roots())

	raw, err := r.Exchange(context.Background(), question(t, "edns.example.", 8, true), true)
	if err != nil {
		t.Fatal(err)
	}
	m := unpack(t, raw)
	if len(m.Additionals) != 1 || m.Additionals[0].Header.Type != dnsmessage.TypeOPT {
		t.Fatalf("приложению с EDNS не вернулась запись OPT: %+v", m.Additionals)
	}
	if opts := m.Additionals[0].Body.(*dnsmessage.OPTResource).Options; len(opts) != 0 {
		t.Fatalf("в ответе осталась добивка резолвера: %+v", opts)
	}
}

// TestKnownResolverComesFirstAndOwnAddressStaysAsBefore — выбранный резолвер
// спрашивается первым с выбранным адресом впереди, остальные известные — в
// запас; свой адрес человека не известен, и для него шифрования нет.
func TestKnownResolverComesFirstAndOwnAddressStaysAsBefore(t *testing.T) {
	got, ok := For("149.112.112.112:53")
	if !ok || got[0].Name != "Quad9" || got[0].Addrs[0] != netip.MustParseAddr("149.112.112.112") {
		t.Fatalf("выбран Quad9, а первым идёт %+v", got)
	}
	if len(got) != len(Known) || len(got[0].Addrs) != 2 {
		t.Fatalf("запасных не столько, сколько известных: %d", len(got))
	}
	for _, s := range got[1:] {
		if s.Name == "Quad9" {
			t.Fatal("выбранный повторился среди запасных")
		}
	}
	if _, ok := For("76.76.2.0"); ok {
		t.Fatal("свой адрес принят за известный резолвер")
	}
	if _, ok := For("не адрес"); ok {
		t.Fatal("мусор принят за резолвер")
	}
}

// TestHTTPSRecordReachesTheApp — запись HTTPS, которую браузеры и Android
// спрашивают рядом с A и AAAA, проходит через разбор и сборку целой: сломай
// её разбор — и браузер ждал бы ответа, которого не будет.
func TestHTTPSRecordReachesTheApp(t *testing.T) {
	f := newFakeResolver(t)
	node := &fakeTunnel{}
	r := newTestResolver(t, f, node, f.roots())

	m := dnsmessage.Message{
		Header: dnsmessage.Header{ID: 11, RecursionDesired: true},
		Questions: []dnsmessage.Question{{
			Name: dnsmessage.MustNewName("svc.example."), Type: dnsmessage.TypeHTTPS, Class: dnsmessage.ClassINET,
		}},
	}
	query, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := r.Exchange(context.Background(), query, true)
	if err != nil {
		t.Fatal(err)
	}
	got := unpack(t, raw)
	if len(got.Answers) != 1 || got.Answers[0].Header.Type != dnsmessage.TypeHTTPS {
		t.Fatalf("запись HTTPS не дошла: %+v", got.Answers)
	}
}
