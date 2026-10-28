package client_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

// echoTarget — сайт, который возвращает всё, что получил.
func echoTarget(t *testing.T) vp1.Address {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	host, port := splitHostPort(t, ln.Addr().String())
	addr, err := vp1.AddressFromHostPort(host, port)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

// Каждое соединение приложения — страница, картинка, запрос API — раньше
// ждало ответа ноды, прежде чем отправить первый байт: лишний круг до ноды
// на каждое соединение. VLESS так не делает. Теперь поток отдаётся сразу, а
// данные уходят вслед за запросом, не дожидаясь, пока нода дозвонится до
// сайта.
func TestConnectionDoesNotWaitForNodeBeforeSendingData(t *testing.T) {
	node := startTestNode(t)
	node.statusDelay.Store(int64(400 * time.Millisecond))
	dialer, err := client.NewDialer(node.info, node.clientKey, node.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer dialer.Close()
	target := echoTarget(t)

	// Прогрев: первое соединение платит за рукопожатие с нодой, оно не
	// про то, что проверяем.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	warm, err := dialer.DialTarget(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	_ = warm.Close()

	start := time.Now()
	stream, err := dialer.DialTarget(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if took := time.Since(start); took > 200*time.Millisecond {
		t.Fatalf("поток открывался %v — ждал ответа ноды", took)
	}
	if _, err := stream.Write([]byte("привет")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len("привет"))
	if _, err := io.ReadFull(stream, got); err != nil || string(got) != "привет" {
		t.Fatalf("эхо: %q %v", got, err)
	}
}

// Отказ ноды по цели не теряется: он приходит первым же чтением, тем же
// типом, что раньше приходил из DialTarget, — и узнаётся тем, кто слушает.
func TestRefusalArrivesOnFirstReadAndIsReported(t *testing.T) {
	node := startTestNode(t)
	node.refuse.Store(int32(vp1.StatusUnreachable))
	dialer, err := client.NewDialer(node.info, node.clientKey, node.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer dialer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stream, err := dialer.DialTarget(ctx, echoTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	reported := make(chan error, 1)
	if early, ok := stream.(interface{ OnStatus(func(error)) }); ok {
		early.OnStatus(func(err error) { reported <- err })
	} else {
		t.Fatal("поток не сообщает об ответе ноды")
	}

	_, readErr := stream.Read(make([]byte, 16))
	var refused *vp1.RefusedError
	if !errors.As(readErr, &refused) || refused.Status != vp1.StatusUnreachable {
		t.Fatalf("чтение после отказа: %v", readErr)
	}
	select {
	case err := <-reported:
		if !errors.As(err, &refused) {
			t.Fatalf("сообщено не то: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("об отказе никто не узнал")
	}
}

// delayProxy — TCP-прокси, задерживающий каждый кусок данных на delay в
// каждую сторону: так на одном ПК выглядит нода за 50 мс сети.
func delayProxy(t *testing.T, upstream string, delay time.Duration) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	// Как в настоящей сети: каждый кусок приходит через delay после отправки,
	// а не после предыдущего куска. Иначе два куска подряд стоили бы два
	// delay, и задержка мерилась бы неверно.
	type chunk struct {
		data []byte
		at   time.Time
	}
	pipe := func(dst, src net.Conn) {
		queue := make(chan chunk, 1024)
		go func() {
			defer close(queue)
			buf := make([]byte, 32<<10)
			for {
				n, err := src.Read(buf)
				if n > 0 {
					queue <- chunk{append([]byte(nil), buf[:n]...), time.Now().Add(delay)}
				}
				if err != nil {
					return
				}
			}
		}()
		for c := range queue {
			time.Sleep(time.Until(c.at))
			if _, err := dst.Write(c.data); err != nil {
				return
			}
		}
		if cw, ok := dst.(*net.TCPConn); ok {
			_ = cw.CloseWrite()
		}
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			u, err := net.Dial("tcp", upstream)
			if err != nil {
				_ = c.Close()
				continue
			}
			go pipe(u, c)
			go pipe(c, u)
		}
	}()
	return ln.Addr().String()
}

// Новое соединение через ноду за пингом 100 мс: запрос, данные и ответ
// сайта укладываются в один круг до ноды, а не в два.
func TestNewConnectionCostsOneRoundTripNotTwo(t *testing.T) {
	const oneWay = 50 * time.Millisecond
	node := startTestNode(t)
	node.info.Address = delayProxy(t, node.info.Address, oneWay)
	dialer, err := client.NewDialer(node.info, node.clientKey, node.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer dialer.Close()
	target := echoTarget(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	warm, err := dialer.DialTarget(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	_ = warm.Close()

	best := time.Hour
	for i := 0; i < 3; i++ {
		start := time.Now()
		stream, err := dialer.DialTarget(ctx, target)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Write([]byte("GET /")); err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadFull(stream, make([]byte, 5)); err != nil {
			t.Fatal(err)
		}
		_ = stream.Close()
		best = min(best, time.Since(start))
	}
	t.Logf("до первого ответа сайта: %v при пинге %v", best, 2*oneWay)
	if best > 3*oneWay {
		t.Fatalf("новое соединение заняло %v — больше одного круга до ноды (%v)", best, 2*oneWay)
	}
}
