//go:build windows

package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Проверки через туннель, которые человек видит на главном экране.
//
// «Подключено» говорит только, что туннель поднят. Человеку же нужно другое:
// какой у него теперь адрес, открывается ли YouTube и насколько быстро. Раньше
// за этим шли на сторонние сайты — и не понимали, виноват VPN, сайт или
// браузер. Все три проверки идут через ядро туннеля, мимо адаптера и
// браузера: ответ — про сам туннель.

// whereURL отдаёт адрес и страну того, кто спрашивает, обычным текстом.
// Через туннель спрашивающий — нода, и Cloudflare видит её адрес, а не
// адрес человека.
var whereURL = "https://www.cloudflare.com/cdn-cgi/trace"

// speedURL — сколько попросить байт, сервер отдаёт ровно столько.
var speedURL = "https://speed.cloudflare.com/__down?bytes=" + strconv.Itoa(speedBytes)

// speedBytes и speedBudget — потолок замера скорости: трафик продавца не
// бесконечен, а за пять секунд скорость уже понятна.
const (
	speedBytes  = 30 << 20
	speedBudget = 5 * time.Second
)

// siteChecks — что проверять. То, без чего человек считает интернет
// сломанным: поиск, видео, мессенджеры, соцсети, ChatGPT и GitHub. Игры
// здесь были, но игра, не открывшаяся через VPN, ничего не говорит о связи —
// только о том, что её сервер не любит адреса хостеров. Домен — тот, что
// открывает браузер.
// siteScheme — подменяется в тестах: у тестового сервера нет сертификата.
var siteScheme = "https://"

var siteChecks = []struct{ Name, Host string }{
	{"Google", "www.google.com"},
	{"YouTube", "www.youtube.com"},
	{"Telegram", "web.telegram.org"},
	{"WhatsApp", "web.whatsapp.com"},
	{"Instagram", "www.instagram.com"},
	{"ChatGPT", "chatgpt.com"},
	{"X", "x.com"},
	{"GitHub", "github.com"},
}

// WhereView — адрес и страна, под которыми человек виден в интернете.
type WhereView struct {
	IP      string `json:"ip,omitempty"`
	Country string `json:"country,omitempty"`
	Error   string `json:"error,omitempty"`
}

// SiteView — открылся ли сайт через туннель и за сколько.
type SiteView struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	MS   int64  `json:"ms,omitempty"`
}

// SpeedView — скорость приёма через туннель.
type SpeedView struct {
	Mbps  float64 `json:"mbps"`
	Bytes int64   `json:"bytes"`
}

// tunnelHTTP — клиент, который ходит через ядро туннеля.
func tunnelHTTP(d client.Backend) *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			host, portText, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			port, err := strconv.ParseUint(portText, 10, 16)
			if err != nil {
				return nil, err
			}
			target, err := vp1.AddressFromHostPort(host, uint16(port))
			if err != nil {
				return nil, err
			}
			return d.DialTarget(ctx, target)
		},
		DisableKeepAlives: true,
	}}
}

// tunnelDialer — ядро поднятого туннеля или ошибка, понятная человеку.
func (c *Controller) tunnelDialer() (client.Backend, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dialer == nil || (c.state != StateConnected && c.state != StateStalled) {
		return nil, errors.New(say("netNoTunnel"))
	}
	return c.dialer, nil
}

// Where спрашивает адрес и страну через туннель. Ответ запоминается до
// конца сессии: адрес ноды за минуту не меняется, а окно спрашивает часто.
func (c *Controller) Where(ctx context.Context) WhereView {
	c.mu.Lock()
	cached := c.where
	c.mu.Unlock()
	if cached.IP != "" {
		return cached
	}
	d, err := c.tunnelDialer()
	if err != nil {
		return WhereView{Error: err.Error()}
	}
	v := fetchWhere(ctx, tunnelHTTP(d))
	if v.IP != "" {
		c.mu.Lock()
		if c.dialer == d {
			c.where = v
		}
		c.mu.Unlock()
	}
	return v
}

func fetchWhere(ctx context.Context, hc *http.Client) WhereView {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, whereURL, nil)
	if err != nil {
		return WhereView{Error: err.Error()}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return WhereView{Error: say("netWhereFailed")}
	}
	defer resp.Body.Close()
	var v WhereView
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 4096))
	for sc.Scan() {
		k, val, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch k {
		case "ip":
			v.IP = val
		case "loc":
			v.Country = strings.ToLower(val)
		}
	}
	if v.IP == "" {
		v.Error = say("netWhereFailed")
	}
	return v
}

// CheckSites проверяет сайты через туннель разом: TLS до сайта и ответ на
// запрос главной. Любой HTTP-ответ — «открывается»: редирект на вход или
// капча — это уже сайт, а не блокировка.
func (c *Controller) CheckSites(ctx context.Context) ([]SiteView, error) {
	d, err := c.tunnelDialer()
	if err != nil {
		return nil, err
	}
	hc := tunnelHTTP(d)
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	out := make([]SiteView, len(siteChecks))
	var wg sync.WaitGroup
	for i, s := range siteChecks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = checkSite(ctx, hc, s.Name, s.Host)
		}()
	}
	wg.Wait()
	return out, nil
}

func checkSite(ctx context.Context, hc *http.Client, name, host string) SiteView {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, siteScheme+host+"/", nil)
	if err != nil {
		return SiteView{Name: name}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	start := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		return SiteView{Name: name}
	}
	resp.Body.Close()
	return SiteView{Name: name, OK: true, MS: max(1, time.Since(start).Milliseconds())}
}

// SpeedTest качает через туннель до speedBytes, но не дольше speedBudget, и
// считает скорость по тому, что успело прийти. Только по кнопке: трафик
// продавца тратится настоящий.
func (c *Controller) SpeedTest(ctx context.Context) (SpeedView, error) {
	d, err := c.tunnelDialer()
	if err != nil {
		return SpeedView{}, err
	}
	return measureSpeed(ctx, tunnelHTTP(d))
}

func measureSpeed(ctx context.Context, hc *http.Client) (SpeedView, error) {
	ctx, cancel := context.WithTimeout(ctx, speedBudget+probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, speedURL, nil)
	if err != nil {
		return SpeedView{}, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return SpeedView{}, errors.New(say("netSpeedFailed"))
	}
	defer resp.Body.Close()
	// Время считаем от первого байта: рукопожатие до сервера замера — это
	// отклик, а не скорость.
	buf := make([]byte, 64<<10)
	var got int64
	var start time.Time
	deadline := time.Now().Add(speedBudget)
	for time.Now().Before(deadline) {
		n, err := resp.Body.Read(buf)
		if n > 0 && start.IsZero() {
			start = time.Now()
		}
		got += int64(n)
		if err != nil {
			break
		}
	}
	// Часы Windows тикают крупно: короткий приём мог уложиться в один тик.
	took := max(time.Since(start), time.Millisecond)
	if start.IsZero() || got < 256<<10 {
		return SpeedView{}, errors.New(say("netSpeedFailed"))
	}
	return SpeedView{Mbps: float64(got*8) / took.Seconds() / 1e6, Bytes: got}, nil
}
