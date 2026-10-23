//go:build windows

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/foreign"
	"github.com/jytt8u/marvia/internal/netpath"
	"github.com/jytt8u/marvia/internal/transport"
)

// Шаги «Проверить всё» на живой системе; что из замеров следует — в diag.go.

// diagDialTimeout — сколько ждать одно TCP-соединение. Дольше четырёх
// секунд адрес для человека уже «не отвечает», а шагов восемь.
const diagDialTimeout = 4 * time.Second

// diagNodeLimit — сколько нод стучать в шаге «Серверы отвечают». Все разом
// незачем: у продавца их бывает три десятка, а вывод — «отвечают ли вообще и
// какая ближе» — ясен по первым.
const diagNodeLimit = 6

// diagNetTargets — адреса для «интернет без VPN»: два разных хозяина и
// голые IP, чтобы шаг не зависел от DNS — его проверяет следующий.
var diagNetTargets = []string{"1.1.1.1:443", "8.8.8.8:443"}

// diagNames — имена для шага DNS. Те, что держат всегда, у двух хозяев.
var diagNames = []string{"www.google.com", "www.cloudflare.com"}

// outside — соединение мимо туннеля, как у самого дозвона до ноды
// (netpath). Без привязки проверка «интернет без VPN» при поднятом туннеле
// ушла бы в туннель и проверяла бы его.
func outside(ctx context.Context, address string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, diagDialTimeout)
	defer cancel()
	start := time.Now()
	conn, err := netpath.Dialer().DialContext(ctx, "tcp", address)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start), nil
}

// fastest стучится по всем адресам разом и отдаёт, сколько ответило и за
// сколько ответил самый быстрый.
func fastest(ctx context.Context, addresses []string) (int, time.Duration) {
	var (
		mu       sync.Mutex
		answered int
		best     time.Duration
		wg       sync.WaitGroup
	)
	for _, a := range addresses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			took, err := outside(ctx, a)
			if err != nil {
				return
			}
			mu.Lock()
			answered++
			if best == 0 || took < best {
				best = took
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return answered, best
}

func millis(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return max(1, d.Milliseconds())
}

// Diagnose проходит один шаг «Проверить всё».
func (c *Controller) Diagnose(ctx context.Context, id string) (DiagStep, error) {
	c.mu.Lock()
	up := c.dialer != nil && (c.state == StateConnected || c.state == StateStalled)
	state, reason, fragment := c.state, c.reason, c.settings.Fragment
	if c.disconnecting || c.leaving > 0 {
		state = StateDisconnecting
	}
	d := c.dialer
	c.mu.Unlock()

	switch id {
	case "net":
		n, best := fastest(ctx, diagNetTargets)
		return judgeNet(n > 0, millis(best)), nil

	case "dns":
		lctx, cancel := context.WithTimeout(ctx, diagDialTimeout)
		defer cancel()
		start := time.Now()
		ok := false
		for _, name := range diagNames {
			if _, err := net.DefaultResolver.LookupHost(lctx, name); err == nil {
				ok = true
				break
			}
		}
		return judgeDNS(ok, up, millis(time.Since(start))), nil

	case "node":
		var addresses []string
		c.mu.Lock()
		nodes := c.subscriptionLocked().Nodes
		if d != nil {
			// Текущая — первой: о ней человек и спрашивает.
			nodes = append([]client.Node{d.Node()}, d.Nodes()...)
		}
		c.mu.Unlock()
		for _, n := range nodes {
			if n.Address != "" && !slices.Contains(addresses, n.Address) {
				addresses = append(addresses, n.Address)
			}
		}
		addresses = addresses[:min(len(addresses), diagNodeLimit)]
		n, best := fastest(ctx, addresses)
		return judgeNode(n, len(addresses), millis(best)), nil

	case "tunnel":
		return judgeTunnel(string(state), fragment, reason), nil

	case "system":
		if !up {
			return judgeSystem(false, false, false), nil
		}
		pctx, cancel := context.WithTimeout(ctx, probeTimeout)
		defer cancel()
		if probeSystem(pctx) == nil {
			return judgeSystem(true, true, false), nil
		}
		tctx, tcancel := context.WithTimeout(ctx, probeTimeout)
		defer tcancel()
		return judgeSystem(true, false, probeTunnel(tctx, d) == nil), nil

	case "where":
		if !up {
			return judgeWhere(false, "", ""), nil
		}
		w := c.Where(ctx)
		return judgeWhere(true, w.IP, w.Country), nil

	case "leak":
		if !up {
			return judgeLeak(false, "", "", false), nil
		}
		c.mu.Lock()
		v6Off := c.settings.IPv6Off
		c.mu.Unlock()
		tunnel := c.Where(ctx)
		// Обычным путём Windows, как браузер. Пока утечки нет, этот запрос
		// уходит в туннель, и Cloudflare видит адрес ноды; виден настоящий —
		// только если утечка есть, а её мы и ищем.
		system := fetchWhere(ctx, &http.Client{Transport: &http.Transport{Proxy: nil}})
		return judgeLeak(true, tunnel.IP, system.IP, v6Off && v6Outside(ctx)), nil

	case "sites":
		if !up {
			return judgeSites(false, 0, 0), nil
		}
		sites, err := c.CheckSites(ctx)
		if err != nil {
			return judgeSites(false, 0, 0), nil
		}
		opened := 0
		for _, s := range sites {
			if s.OK {
				opened++
			}
		}
		return judgeSites(true, opened, len(sites)), nil

	case "interference":
		// Здесь — сразу, а не по расписанию сторожа: человек нажал «Проверить»
		// и ждёт ответа про сейчас, а не про четверть минуты назад.
		found := conflictsAmong(processList())
		if i := slices.Index(found, "conflictWarp"); i >= 0 && !warpConnected() {
			found = slices.Delete(found, i, i+1)
		}
		p := systemProxy()
		c.mu.Lock()
		c.conflicts, c.conflictsAt = found, time.Now()
		c.proxy, c.proxyAt = p, time.Now()
		c.mu.Unlock()
		return judgeInterference(conflictTexts(found), p.Describe(), p.FromEnv), nil
	}
	return DiagStep{}, errors.New(say("badRequest"))
}

// v6Target — адрес для проверки IPv6: DNS Cloudflare по v6, порт 443.
// Голый адрес — чтобы шаг не зависел от запросов имён.
const v6Target = "[2606:4700:4700::1111]:443"

// v6Outside — открывается ли соединение по IPv6 обычным путём Windows.
// Туннель с выключенным IPv6 такие соединения сбрасывает сам (tunbridge),
// так что открывшееся ушло мимо него.
func v6Outside(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, diagDialTimeout)
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp6", v6Target)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Reconnect — «Переподключиться» из «Диагностики»: туннель заново, с тем,
// что сейчас выбрано. Без него дробление или другой DNS, включённые кнопкой
// рядом, ждали бы, пока человек сам догадается нажать «Отключить» и
// «Подключить».
func (c *Controller) Reconnect() error {
	c.mu.Lock()
	up := c.state != StateIdle && c.state != StateFailed
	c.mu.Unlock()
	if up {
		c.teardown()
	}
	return c.Connect()
}

// ClipboardOffer — ключ доступа из буфера обмена, если он там есть; см.
// KeyOffer.
func (c *Controller) ClipboardOffer() (KeyOffer, error) {
	text, err := pasteText()
	if err != nil {
		return KeyOffer{}, err
	}
	c.mu.Lock()
	saved := append([]savedKey(nil), c.keys...)
	c.mu.Unlock()
	return clipboardOffer(text, saved), nil
}

// RouteView — как идём до ноды, для «Диагностики». Только то, с чем
// дозванивается ядро: чего оно не знает, того здесь нет.
type RouteView struct {
	// Protocol — vp1 у ключа Marvia, иначе протокол чужой ссылки.
	Protocol string `json:"protocol"`
	// Transport — у VP1: tls, reality, ws или quic; у чужих — транспорт
	// Xray: raw, ws, grpc, xhttp…
	Transport string `json:"transport,omitempty"`
	// Security — у чужих: tls, reality или none.
	Security string `json:"security,omitempty"`
	// SNI — имя прикрытия; SpareSNI — сколько запасных у REALITY.
	SNI      string `json:"sni,omitempty"`
	SpareSNI int    `json:"spare_sni,omitempty"`
	// Fingerprint — чьё TLS-приветствие изображается. Выбирает его продавец
	// для ноды; у QUIC своего приветствия uTLS нет, и поле пустое.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// routeLocked — описание текущего соединения; зовётся под c.mu.
func (c *Controller) routeLocked() *RouteView {
	if c.dialer == nil || c.node.Address == "" {
		return nil
	}
	if s, ok := c.dialer.(*foreign.Supervisor); ok {
		r := s.Route()
		return &RouteView{Protocol: r.Protocol, Transport: r.Network, Security: r.Security, SNI: r.SNI, Fingerprint: r.Fingerprint}
	}
	return vp1Route(c.node, c.tcpOnlyInUse != nil && *c.tcpOnlyInUse)
}

// vp1Route — то же для ноды Marvia, теми же правилами, что у дозвона
// (client.NewDialer): без SNI имя — хост адреса, незнакомый отпечаток —
// Chrome.
func vp1Route(n client.Node, tcpOnly bool) *RouteView {
	r := &RouteView{Protocol: "vp1", Transport: transportOf(n, tcpOnly), SNI: n.SNI, SpareSNI: len(n.SNIExtra)}
	if r.SNI == "" {
		r.SNI, _, _ = net.SplitHostPort(n.Address)
	}
	if n.Transport() != client.TransportReality {
		r.SpareSNI = 0
	}
	if r.Transport != "quic" {
		r.Fingerprint = "chrome"
		if _, known := transport.Fingerprint(n.Fingerprint); known {
			r.Fingerprint = n.Fingerprint
		}
	}
	return r
}
