package securedns

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

const (
	// mediaType — тип тела запроса и ответа по RFC 8484.
	mediaType = "application/dns-message"

	// attemptTimeout — сколько ждём один адрес, прежде чем перейти к
	// следующему. Системный резолвер телефона сам повторяет вопрос примерно
	// через пять секунд; три — чтобы успеть сходить к запасному до того, как
	// он сдастся.
	attemptTimeout = 3 * time.Second

	// retryChosen — через сколько после ухода на запасной снова пробуем
	// выбранный. Ходить к нему на каждый вопрос нельзя — каждый платил бы
	// таймаут, — а остаться на запасном навсегда значит молча сменить
	// человеку резолвер.
	retryChosen = 2 * time.Minute
)

// Config описывает резолвер.
type Config struct {
	// Servers — по порядку: выбранный первым, дальше запасные. См. For.
	Servers []Server

	// Dial открывает поток до адреса через туннель. Только через туннель:
	// соединение с резолвером мимо него — это DoH, видный цензору, а его
	// блокируют.
	Dial func(ctx context.Context, addr netip.AddrPort) (net.Conn, error)

	// RootCAs — кому верить. nil — системное хранилище; своё нужно только
	// проверкам.
	RootCAs *x509.CertPool

	// Attempt — сколько ждать один адрес; ноль — attemptTimeout.
	Attempt time.Duration
}

// Resolver отвечает на запросы имён через DoH.
type Resolver struct {
	ends    []*endpoint
	attempt time.Duration
	cache   cache

	mu      sync.Mutex
	current int
	movedAt time.Time

	// now — часы; подменяются проверками кэша.
	now func() time.Time
}

// endpoint — один адрес одного резолвера со своим HTTP-клиентом.
//
// Клиент на адрес, а не общий: соединение HTTP/2 привязано к адресу, и
// запасной адрес должен поднимать своё, а не ждать, пока умрёт чужое.
type endpoint struct {
	server Server
	addr   netip.Addr
	url    string
	tr     *http.Transport
	client *http.Client
}

// New собирает резолвер. Соединений не открывает: первое поднимется на
// первом же вопросе.
func New(cfg Config) (*Resolver, error) {
	if cfg.Dial == nil {
		return nil, errors.New("не задан способ дозвона до резолвера")
	}
	r := &Resolver{attempt: cfg.Attempt, now: time.Now}
	if r.attempt <= 0 {
		r.attempt = attemptTimeout
	}
	for _, s := range cfg.Servers {
		for _, a := range s.Addrs {
			r.ends = append(r.ends, newEndpoint(s, a, cfg))
		}
	}
	if len(r.ends) == 0 {
		return nil, errors.New("не задано ни одного резолвера")
	}
	return r, nil
}

func newEndpoint(s Server, addr netip.Addr, cfg Config) *endpoint {
	target := netip.AddrPortFrom(addr, 443)
	tr := &http.Transport{
		// Адрес из URL не слушаем и имя не разрешаем: идём ровно на
		// зашитый адрес и только через туннель. Proxy остаётся пустым
		// намеренно — системный прокси увёл бы запрос мимо туннеля.
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return cfg.Dial(ctx, target)
		},
		TLSClientConfig: &tls.Config{
			ServerName: s.Host,
			RootCAs:    cfg.RootCAs,
			MinVersion: tls.VersionTLS12,
		},
		ForceAttemptHTTP2:   true,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
	}
	return &endpoint{
		server: s,
		addr:   addr,
		url:    "https://" + s.Host + s.Path,
		tr:     tr,
		client: &http.Client{
			Transport: tr,
			// Резолвер не перенаправляет; перенаправление куда-то ещё — это
			// новое имя, которое пришлось бы разрешать, а разрешать его не у
			// кого.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Exchange отвечает на запрос имени приложения.
//
// udp — ответ поедет датаграммой и обязан влезть в размер, который объявило
// приложение; не влезет — уйдёт с флагом TC, и приложение спросит по TCP.
//
// Отказ всех резолверов — ошибка, а не откат на открытый DNS. Это решение, а
// не недосмотр: нода, которая хочет видеть имена, может просто перестать
// пропускать соединения к резолверам или подменить сертификат, и тихий откат
// отдал бы ей ровно то, что мы прячем. Человек видит ошибку и сам выбирает
// «без шифрования», если ему так нужно.
func (r *Resolver) Exchange(ctx context.Context, raw []byte, udp bool) ([]byte, error) {
	q, err := parseQuery(raw)
	if err != nil {
		return nil, err
	}
	if answer, ok := r.cache.get(q.key, r.now()); ok {
		return q.reply(answer, udp)
	}

	wire, err := q.outgoing()
	if err != nil {
		return nil, err
	}
	resp, err := r.ask(ctx, wire)
	if err != nil {
		return nil, err
	}
	answer, err := parseAnswer(resp, q)
	if err != nil {
		return nil, err
	}
	r.cache.put(q.key, answer, r.now())
	return q.reply(answer, udp)
}

// Reset забывает всё, что относилось к прежней ноде: кэш ответов и открытые
// соединения. Соединения жили в потоках через неё и всё равно мертвы, а
// ждать, пока HTTP/2 заметит это сам, — это первый вопрос после переезда,
// повисший на таймауте.
func (r *Resolver) Reset() {
	r.cache.reset()
	for _, e := range r.ends {
		e.tr.CloseIdleConnections()
	}
	r.mu.Lock()
	r.current = 0
	r.mu.Unlock()
}

// ask отправляет запрос по очереди адресам, начиная с последнего, который
// отвечал.
func (r *Resolver) ask(ctx context.Context, wire []byte) ([]byte, error) {
	var errs []error
	for _, i := range r.order() {
		e := r.ends[i]
		attempt, cancel := context.WithTimeout(ctx, r.attempt)
		resp, err := e.do(attempt, wire)
		cancel()
		if err == nil {
			r.settle(i)
			return resp, nil
		}
		errs = append(errs, fmt.Errorf("%s %s: %w", e.server.Name, e.addr, err))
		if ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("ни один резолвер не ответил: %w", errors.Join(errs...))
}

// order — в каком порядке спрашивать: с того, кто ответил последним.
func (r *Resolver) order() []int {
	r.mu.Lock()
	if r.current != 0 && r.now().Sub(r.movedAt) >= retryChosen {
		r.current = 0
	}
	start := r.current
	r.mu.Unlock()

	out := make([]int, 0, len(r.ends))
	for k := range r.ends {
		out = append(out, (start+k)%len(r.ends))
	}
	return out
}

func (r *Resolver) settle(i int) {
	r.mu.Lock()
	if r.current != i {
		r.current = i
		r.movedAt = r.now()
	}
	r.mu.Unlock()
}

// do — один запрос к одному адресу.
func (e *endpoint) do(ctx context.Context, wire []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(wire))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mediaType)
	req.Header.Set("Accept", mediaType)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("резолвер ответил %s", resp.Status)
	}
	if kind, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || kind != mediaType {
		return nil, fmt.Errorf("резолвер ответил не сообщением DNS: %q", resp.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMessage+1))
	if err != nil {
		return nil, fmt.Errorf("чтение ответа резолвера: %w", err)
	}
	if len(body) > maxMessage {
		return nil, errors.New("ответ резолвера больше, чем бывает в DNS")
	}
	return body, nil
}
