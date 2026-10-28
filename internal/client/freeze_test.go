package client_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/transport"
	"github.com/jytt8u/marvia/internal/vp1"
)

type freezeEvent struct {
	Connection int64
	Bytes      int64
	At         time.Time
}

// Считаем байты внешнего TCP, включая рукопожатия и добивку. Если считать
// полезные данные VP1, порог блокировки на проводе получился бы другим.
// endpoint дополнительно глушит новые соединения после первого срабатывания:
// это контроль блокировки адреса, а не модель порога на каждом соединении.
type freezingProxy struct {
	Address string
	Events  chan freezeEvent
	armed   atomic.Bool
	blocked atomic.Bool
	accepts atomic.Int64
	rate    atomic.Int64
}

func freezeProxy(t *testing.T, upstream string, limit int64, endpoint bool) *freezingProxy {
	t.Helper()
	if limit < 0 {
		t.Fatal("порог заморозки отрицателен")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &freezingProxy{Address: ln.Addr().String(), Events: make(chan freezeEvent, 32)}
	stop := make(chan struct{})
	accepted := make(chan struct{})
	var mu sync.Mutex
	active := make(map[net.Conn]struct{})
	var workers sync.WaitGroup
	track := func(c net.Conn) {
		mu.Lock()
		active[c] = struct{}{}
		mu.Unlock()
	}
	t.Cleanup(func() {
		_ = ln.Close()
		close(stop)
		<-accepted
		mu.Lock()
		for c := range active {
			_ = c.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	go func() {
		defer close(accepted)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			id := p.accepts.Add(1)
			track(c)
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer c.Close()
				u, err := net.DialTimeout("tcp", upstream, time.Second)
				if err != nil {
					return
				}
				track(u)
				defer u.Close()
				defer func() {
					mu.Lock()
					delete(active, c)
					delete(active, u)
					mu.Unlock()
				}()
				clientDone := make(chan struct{})
				go func() {
					defer close(clientDone)
					_, _ = io.Copy(u, c)
					// Закрытие клиентом — единственное штатное завершение
					// замороженной пары; EOF сервера клиент видеть не должен.
					_ = u.Close()
				}()
				defer func() { _ = c.Close(); <-clientDone }()

				var forwarded int64
				buf := make([]byte, 32<<10)
				for {
					if p.armed.Load() && (forwarded >= limit || (endpoint && p.blocked.Load())) {
						p.blocked.Store(true)
						select {
						case p.Events <- freezeEvent{id, forwarded, time.Now()}:
						case <-stop:
							return
						}
						// Читаем и выбрасываем: TCP остаётся открытым и не
						// создаёт случайный FIN/RST или заполнение окна вместо
						// проверяемого молчания сервера. Обратный путь жив.
						_, _ = io.Copy(io.Discard, u)
						select {
						case <-clientDone:
						case <-stop:
						}
						return
					}
					n, readErr := u.Read(buf)
					if n > 0 {
						if rate := p.rate.Load(); rate > 0 {
							timer := time.NewTimer(time.Duration(n) * time.Second / time.Duration(rate))
							select {
							case <-timer.C:
							case <-stop:
								timer.Stop()
								return
							}
						}
						allowed := n
						if p.armed.Load() {
							allowed = int(min(int64(n), max(int64(0), limit-forwarded)))
						}
						written, err := c.Write(buf[:allowed])
						forwarded += int64(written)
						if err != nil {
							return
						}
					}
					// Срабатывание важнее EOF, пришедшего вместе с последним
					// куском: иначе стенд сам оборвал бы соединение на пороге.
					if readErr != nil && !(p.armed.Load() && forwarded >= limit) {
						return
					}
				}
			}()
		}
	}()
	return p
}

func TestFreezeProxyStopsAtExactByteWithoutClosingTCP(t *testing.T) {
	for _, limit := range []int64{0, 1, 31, 15 * 1024, 20 * 1024} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			server := make(chan net.Conn, 1)
			go func() {
				c, err := ln.Accept()
				if err == nil {
					server <- c
					_, _ = c.Write(bytes.Repeat([]byte{0x5a}, int(limit)+257))
				}
			}()
			p := freezeProxy(t, ln.Addr().String(), limit, false)
			p.armed.Store(true)
			c, err := net.DialTimeout("tcp", p.Address, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(3 * time.Second))
			var u net.Conn
			select {
			case u = <-server:
			case <-time.After(3 * time.Second):
				t.Fatal("прокси не дозвонился до сервера")
			}
			defer u.Close()
			got := make([]byte, limit)
			if _, err := io.ReadFull(c, got); err != nil || !bytes.Equal(got, bytes.Repeat([]byte{0x5a}, int(limit))) {
				t.Fatalf("до порога данные изменились: %v", err)
			}
			select {
			case event := <-p.Events:
				if event.Bytes != limit || event.Connection != 1 {
					t.Fatalf("неверный порог: %+v", event)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("нет события заморозки")
			}
			// Дедлайн здесь принадлежит только проверке прокси. В замере
			// VP1 его нет: иначе мерили бы собственный таймер теста.
			_ = c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, err := c.Read(make([]byte, 1))
			if n != 0 || !isTimeout(err) {
				t.Fatalf("после порога пришли данные или обрыв: n=%d err=%v", n, err)
			}
			if _, err := c.Write([]byte{0x42}); err != nil {
				t.Fatal(err)
			}
			_ = u.SetReadDeadline(time.Now().Add(time.Second))
			back := make([]byte, 1)
			if _, err := io.ReadFull(u, back); err != nil || back[0] != 0x42 {
				t.Fatalf("обратное направление перестало работать: %v", err)
			}
			_ = u.Close()
			_ = c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			if n, err := c.Read(back); n != 0 || !isTimeout(err) {
				t.Fatalf("прокси передал закрытие сервера после заморозки: %d %v", n, err)
			}
		})
	}
}

func isTimeout(err error) bool {
	if e, ok := err.(net.Error); ok {
		return e.Timeout()
	}
	return false
}

func startFreezeNode(t *testing.T, kind client.Transport, cert tls.Certificate, key vp1.KeyPair) *testNode {
	t.Helper()
	serverKey, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tcp.Close() })
	n := &testNode{clientKey: key}
	n.info = client.Node{Address: tcp.Addr().String(), SNI: "example.com", PublicKey: vp1.EncodeKey(serverKey.Public)}
	var ln net.Listener
	switch kind {
	case client.TransportReality:
		cover := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("сайт прикрытия")) }))
		// Зеркальный хендшейк сайта прикрытия прерывается штатно, когда
		// REALITY узнаёт клиента. Его журнал мешал бы читать результаты.
		cover.Config.ErrorLog = log.New(io.Discard, "", 0)
		cover.StartTLS()
		t.Cleanup(cover.Close)
		realityKey, keyErr := vp1.GenerateKeyPair()
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		ln, err = transport.ListenReality(tcp, transport.RealityConfig{Dest: strings.TrimPrefix(cover.URL, "https://"), ServerNames: []string{n.info.SNI}, PrivateKey: realityKey.Private})
		n.info.RealityPublicKey = vp1.EncodeKey(realityKey.Public)
	case client.TransportWS:
		n.info.WSPath = "/assets/app.js"
		ln, err = transport.ListenWS(tcp, transport.WSConfig{Path: n.info.WSPath, Certificate: &cert})
		if err != nil {
			t.Fatal(err)
		}
		n.opts.RootCAs, err = transport.CertificatePool(cert)
	default:
		t.Fatalf("неподдерживаемый транспорт: %s", kind)
	}
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	conns := make(map[net.Conn]struct{})
	var workers sync.WaitGroup
	accepted := make(chan struct{})
	t.Cleanup(func() {
		_ = ln.Close()
		<-accepted
		mu.Lock()
		for c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	guard := vp1.NewReplayGuard(vp1.ClockSkew)
	go func() {
		defer close(accepted)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns[c] = struct{}{}
			mu.Unlock()
			workers.Add(1)
			n.handshakes.Add(1)
			go func() {
				defer workers.Done()
				serveNodeConn(c, serverKey, guard, n)
				mu.Lock()
				delete(conns, c)
				mu.Unlock()
			}()
		}
	}()
	return n
}

// Штатные таймеры оставляем целыми. Этот стенд занимает минуты, поэтому
// обычный go test проверяет прокси, а длительный замер включается явно.
func TestFrozenTCPIsDetectedAndNewRequestRecovers(t *testing.T) {
	if os.Getenv("MARVIA_FREEZE_TEST") != "1" {
		t.Skip("длительный стенд: MARVIA_FREEZE_TEST=1 go test ./internal/client -run TestFrozenTCP -v -count=1 -timeout=5m")
	}
	for _, kind := range []client.Transport{client.TransportReality, client.TransportWS} {
		for _, limit := range []int64{15 * 1024, 20 * 1024} {
			for _, endpoint := range []bool{false, true} {
				mode := "connection"
				if endpoint {
					mode = "endpoint"
				}
				t.Run(fmt.Sprintf("%s/%d/%s", kind, limit, mode), func(t *testing.T) {
					t.Parallel()
					measureFreezeRecovery(t, kind, limit, endpoint)
				})
			}
		}
	}
}

func measureFreezeRecovery(t *testing.T, kind client.Transport, limit int64, endpoint bool) {
	t.Helper()
	cert, err := transport.SelfSignedCertificate("example.com")
	if err != nil {
		t.Fatal(err)
	}
	key, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	bad := startFreezeNode(t, kind, cert, key)
	good := startFreezeNode(t, kind, cert, key)
	bad.info.ID, good.info.ID = 1, 2
	bad.info.Name, good.info.Name = "замораживаемая", "запасная"
	p := freezeProxy(t, bad.info.Address, limit, endpoint)
	bad.info.Address = p.Address
	const subURL = "https://panel.example.invalid/sub/freeze"
	cache := filepath.Join(t.TempDir(), "subscription.json")
	if err := client.SaveCache(cache, subURL, client.Subscription{Nodes: []client.Node{bad.info, good.info}}); err != nil {
		t.Fatal(err)
	}
	switches := make(chan time.Time, 8)
	declared := make(chan time.Time, 8)
	legacy := make(chan time.Time, 8)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	s, _, err := client.Supervise(ctx, client.ConnectConfig{
		Account: client.Account{SubscriptionURL: subURL}, Key: key, Dial: bad.opts, CachePath: cache, Prefer: 1,
		Log: func(format string, args ...any) {
			msg := fmt.Sprintf(format, args...)
			if strings.Contains(msg, "не отвечает на") {
				declared <- time.Now()
			}
			if strings.Contains(msg, "старая версия") {
				legacy <- time.Now()
			}
		},
	}, client.Events{OnSwitch: func(n client.Node) {
		if endpoint && n.ID != 2 {
			t.Errorf("переезд вернул замороженный адрес: %d", n.ID)
		}
		switches <- time.Now()
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	target := echoTarget(t)
	stream, err := s.DialTarget(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Write([]byte{0x42}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(stream, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	p.armed.Store(true)
	payload := bytes.Repeat([]byte{0x5a}, 64<<10)
	writeDone := make(chan error, 1)
	go func() { _, err := stream.Write(payload); writeDone <- err }()
	type readResult struct {
		at  time.Time
		n   int
		err error
	}
	readDone := make(chan readResult, 1)
	go func() {
		n, err := io.ReadFull(stream, make([]byte, len(payload)))
		readDone <- readResult{time.Now(), n, err}
	}()
	var frozen freezeEvent
	select {
	case frozen = <-p.Events:
		if frozen.Bytes != limit {
			t.Fatalf("заморозка не на пороге: %+v", frozen)
		}
	case <-ctx.Done():
		t.Fatal("заморозка не сработала")
	}
	var detected readResult
	select {
	case detected = <-readDone:
		if detected.err == nil || detected.n >= len(payload) {
			t.Fatalf("замороженная загрузка завершилась: %+v", detected)
		}
		if detected.at.Sub(frozen.At) < time.Second {
			t.Fatalf("соединение оборвалось сразу вместо молчания: %v", detected.err)
		}
	case <-ctx.Done():
		t.Fatal("клиент не обнаружил заморозку за 150 секунд")
	case <-time.After(15 * time.Second):
		t.Fatal("клиент не обнаружил заморозку за 15 секунд")
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("запись запроса: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("запись не закончилась")
	}
	var switched time.Time
	{
		remaining := 30*time.Second - time.Since(frozen.At)
		select {
		case switched = <-switches:
		case <-ctx.Done():
			t.Fatal("надзор не переехал на запасную ноду за 150 секунд")
		case <-time.After(max(remaining, time.Nanosecond)):
			t.Fatal("надзор не переехал на запасную ноду за 30 секунд")
		}
		if s.Node().ID != 2 {
			t.Fatalf("надзор повторно выбрал замороженный адрес: %d", s.Node().ID)
		}
	}
	fresh, err := s.DialTarget(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	const answer = "данные после заморозки"
	if _, err := fresh.Write([]byte(answer)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(answer))
	if _, err := io.ReadFull(fresh, got); err != nil || string(got) != answer {
		t.Fatalf("новый запрос не восстановился: %q %v", got, err)
	}
	recovered := time.Now()
	if !endpoint {
		select {
		case switched = <-switches:
		default:
		}
	}
	t.Logf("FREEZE threshold=%d transport=%s endpoint=%t received=%d detect=%.3fs recover=%.3fs new_tcp=%d node=%d read_error=%v", limit, kind, endpoint, detected.n, detected.at.Sub(frozen.At).Seconds(), recovered.Sub(frozen.At).Seconds(), p.accepts.Load(), s.Node().ID, detected.err)
	if !switched.IsZero() {
		t.Logf("FREEZE switch=%.3fs", switched.Sub(frozen.At).Seconds())
	}
	select {
	case at := <-declared:
		t.Logf("FREEZE supervisor_declared=%.3fs", at.Sub(frozen.At).Seconds())
	default:
	}
	select {
	case at := <-legacy:
		t.Fatalf("молчание ошибочно названо старой версией: %.3fs", at.Sub(frozen.At).Seconds())
	default:
	}
	if !endpoint {
		// Маленький успешный запрос ещё не означает исправления: тот же
		// адрес снова глохнет на большой загрузке в новой TCP-сессии.
		// Если клиент научится уходить на запасную ноду, проверяем уже
		// полную загрузку: тест не должен требовать сохранения дефекта.
		if s.Node().ID == 2 {
			go func() { _, _ = fresh.Write(payload) }()
			got := make([]byte, len(payload))
			if _, err := io.ReadFull(fresh, got); err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("загрузка на запасной ноде не восстановилась: %v", err)
			}
			t.Logf("FREEZE repeats_on_new_tcp=false")
			return
		}
		go func() { _, _ = fresh.Write(payload) }()
		go func() { _, _ = io.Copy(io.Discard, fresh) }()
		select {
		case event := <-p.Events:
			if event.Connection <= frozen.Connection || event.Bytes != limit {
				t.Fatalf("неверная повторная заморозка: %+v", event)
			}
			t.Logf("FREEZE repeats_on_new_tcp=true")
		case <-ctx.Done():
			t.Fatal("новое TCP-соединение не повторило порог")
		}
	}
}
