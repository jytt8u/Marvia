package panel

import (
	"log"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Защита панели от потока запросов.
//
// Панель — одна на продавца, и через неё идут все подписки: положить её
// дешевле, чем любую ноду. Для этого не нужно знать ни одного токена —
// хватает тысяч запросов к /sub/ со случайными токенами: каждый — поход в
// базу. Три предела:
//
//   - частота запросов с одного адреса. Щедрая: страница панели спрашивает
//     обновления раз в полторы секунды, при загрузке шлёт десяток запросов
//     разом, ноды сверяются раз в 15 секунд, приложения присылают отчёты о
//     нодах. Всё это на порядок ниже предела;
//   - неудачи с одного адреса: 401 — неизвестный токен, 404 вне /api/v1/ —
//     неизвестная подписка, приглашение или путь. Сотня за десять минут —
//     и адрес на десять минут получает 429. 403 неудачей не считается: его
//     получает тот, чей токен настоящий, — бот без нужного права или
//     выключенная нода, — и это не перебор. 404 внутри /api/v1/ тоже: туда
//     без настоящего токена не попасть, сначала будет 401, а бот законно
//     спрашивает «есть ли такой покупатель» и слышит «нет»;
//   - общее число запросов в работе. Сверх него — сразу 503, без похода в
//     базу: под флудом с тысяч адресов лучше быстро отказать части, чем
//     медленно не ответить никому.
//
// Чем это не поможет. Перед панелью за Cloudflare все запросы приходят с
// его адресов, и адресом считается узел Cloudflare, а не покупатель:
// заголовкам от публичных адресов панель не верит (callerIP), иначе любой
// подставлял бы себе новый адрес на каждый запрос. Пределы рассчитаны так,
// чтобы и тогда обычная нагрузка через один узел в них укладывалась, но
// флудер, пришедший через тот же узел, делит предел с покупателями —
// защищать от него при этом должен сам Cloudflare. Обратный прокси на той
// же машине должен заменять X-Forwarded-For, а не дописывать к нему:
// callerIP берёт первый адрес из заголовка.
//
// Адреса держатся только в памяти: не больше tracked штук и не дольше
// одиннадцати минут после последнего запроса с адреса — окно неудач или
// блокировка плюс минута до чистки. В журнал они не попадают: туда идёт
// одно число отклонённых.

// floodLimits — пределы. Нулевая частота выключает предел частоты, нулевое
// число неудач — блокировку, нулевой concurrent — предел запросов в работе.
type floodLimits struct {
	rate  float64 // запросов в секунду с адреса, в среднем
	burst float64 // запас на пик: загрузка страницы, переподключение толпы

	failures int           // неудач за окно до блокировки
	window   time.Duration // окно неудач
	ban      time.Duration // сколько длится блокировка

	concurrent int // запросов в работе всего
	tracked    int // адресов в памяти
}

// productionFlood — пределы боевой панели.
//
// Частота 30 в секунду с запасом 120 — это сто тысяч запросов в час с одного
// адреса: столько не набирает даже узел Cloudflare перед продавцом с
// тысячами покупателей, а флудер в простейшем цикле выдаёт тысячи в секунду.
// Сотня неудач за десять минут — не помеха живым: приложение удалённого
// покупателя стучится раз в несколько часов, нода с отозванным токеном —
// четыре раза в минуту, и отзыв она узнаёт с первого же 401. А перебору
// токенов подписки сто попыток за десять минут не дают ничего — даже
// коротких, перенесённых с чужих панелей. 512 запросов в работе — далеко за
// обычной нагрузкой, и очередь к базе при этом остаётся короткой: кто сверх,
// получает отказ сразу, а не ответ через минуту.
var productionFlood = floodLimits{
	rate:       30,
	burst:      120,
	failures:   100,
	window:     10 * time.Minute,
	ban:        10 * time.Minute,
	concurrent: 512,
	tracked:    16384,
}

// floodDefaults — с какими пределами собирается каждый API. Тесты пакета
// ставят здесь нули (export_test.go): они шлют сотни запросов с одного
// адреса, проверяя совсем другое, и пределы сделали бы их зависимыми от
// скорости машины.
var floodDefaults = productionFlood

// floodGuard — промежуточный слой перед маршрутизатором.
type floodGuard struct {
	next   http.Handler
	limits floodLimits
	now    func() time.Time
	slots  chan struct{}

	mu       sync.Mutex
	callers  map[netip.Addr]caller
	swept    time.Time
	rejected int // отклонено с последней строки журнала
	reported time.Time

	// forget — отложенная чистка таблицы; nil, пока таблица пуста. Чистка
	// при запросах есть и так, но обещание «адрес забывается за минуты»
	// (docs/privacy.md) должно держаться и тогда, когда запросов нет.
	// Таймер, а не горутина: у обработчика нет Close, и остановить
	// горутину было бы некому, а таймер заводится, только пока есть кого
	// забывать.
	forget *time.Timer
}

// caller — что панель помнит об адресе.
type caller struct {
	tokens float64
	at     time.Time

	fails       int
	failsFrom   time.Time
	bannedUntil time.Time
}

func newFloodGuard(next http.Handler, limits floodLimits, now func() time.Time) *floodGuard {
	if now == nil {
		now = time.Now
	}
	g := &floodGuard{next: next, limits: limits, now: now, callers: make(map[netip.Addr]caller)}
	if limits.concurrent > 0 {
		g.slots = make(chan struct{}, limits.concurrent)
	}
	return g
}

func (g *floodGuard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key, tracked := g.keyOf(r)
	if tracked {
		if wait, ok := g.admit(key); !ok {
			g.reject()
			w.Header().Set("Retry-After", seconds(wait))
			fail(w, http.StatusTooManyRequests, "слишком много запросов с этого адреса: повтори позже")
			return
		}
	}
	if g.slots != nil {
		select {
		case g.slots <- struct{}{}:
			defer func() { <-g.slots }()
		default:
			g.reject()
			w.Header().Set("Retry-After", "1")
			fail(w, http.StatusServiceUnavailable, "панель перегружена: повтори через секунду")
			return
		}
	}
	if !tracked || g.limits.failures <= 0 {
		g.next.ServeHTTP(w, r)
		return
	}
	sw := &statusWriter{ResponseWriter: w}
	g.next.ServeHTTP(sw, r)
	if failure(sw.status, r.URL.Path) {
		g.failed(key)
	}
}

// failure — похож ли ответ на промах того, кто подбирает токены.
func failure(status int, path string) bool {
	switch status {
	case http.StatusUnauthorized:
		return true
	case http.StatusNotFound:
		return !strings.HasPrefix(path, "/api/v1/")
	}
	return false
}

// keyOf — чей запрос. Свои адреса — эта же машина и частная сеть — не
// ограничиваются: оттуда приходят бот продавца, стоящий рядом с панелью, и
// обратный прокси, не передавший адрес. Резать их — значит резать всех
// покупателей за прокси разом. IPv6 считается по сети /64, как и у ноды
// (internal/connguard): внутри неё абонент меняет адреса когда угодно.
func (g *floodGuard) keyOf(r *http.Request) (netip.Addr, bool) {
	if g.limits.rate <= 0 && g.limits.failures <= 0 {
		return netip.Addr{}, false
	}
	host := callerIP(r)
	if trustedForwarder(host) {
		return netip.Addr{}, false
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	a = a.Unmap().WithZone("")
	if a.Is6() {
		p, err := a.Prefix(64)
		if err != nil {
			return netip.Addr{}, false
		}
		a = p.Addr()
	}
	return a, true
}

// admit решает, пускать ли запрос, и если нет — через сколько повторить.
func (g *floodGuard) admit(key netip.Addr) (time.Duration, bool) {
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Sub(g.swept) >= time.Minute {
		g.sweep(now)
	}
	c, ok := g.callers[key]
	if !ok {
		if len(g.callers) >= g.limits.tracked {
			// Таблица полна: чистим не чаще раза в секунду, а не уместившийся
			// адрес пропускаем. Отказ означал бы, что владелец тысяч адресов
			// запирает панель от всех; пропуск оставляет его пределу запросов
			// в работе.
			if now.Sub(g.swept) >= time.Second {
				g.sweep(now)
			}
			if len(g.callers) >= g.limits.tracked {
				return 0, true
			}
		}
		c = caller{tokens: g.limits.burst, at: now}
	}
	if now.Before(c.bannedUntil) {
		return c.bannedUntil.Sub(now), false
	}
	if g.limits.rate > 0 {
		c.tokens = math.Min(g.limits.burst, c.tokens+now.Sub(c.at).Seconds()*g.limits.rate)
		c.at = now
		if c.tokens < 1 {
			g.callers[key] = c
			return time.Duration((1 - c.tokens) / g.limits.rate * float64(time.Second)), false
		}
		c.tokens--
	}
	g.callers[key] = c
	g.scheduleForget()
	return 0, true
}

// scheduleForget заводит чистку через минуту, если она ещё не заведена.
// Вызывается под g.mu.
func (g *floodGuard) scheduleForget() {
	if g.forget != nil || len(g.callers) == 0 {
		return
	}
	g.forget = time.AfterFunc(time.Minute, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.forget = nil
		g.sweep(g.now())
		g.scheduleForget()
	})
}

// failed засчитывает адресу неудачу и блокирует его на сотой за окно.
func (g *floodGuard) failed(key netip.Addr) {
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	c, ok := g.callers[key]
	if !ok {
		return // не уместился в таблицу — его держит только общий предел
	}
	if now.Sub(c.failsFrom) >= g.limits.window {
		c.fails, c.failsFrom = 0, now
	}
	c.fails++
	if c.fails >= g.limits.failures {
		c.bannedUntil = now.Add(g.limits.ban)
		c.fails, c.failsFrom = 0, now
	}
	g.callers[key] = c
}

// sweep забывает адреса, о которых помнить уже нечего: ведро полное, неудачи
// за окном, блокировка кончилась. Такая запись ничем не отличается от
// отсутствующей.
func (g *floodGuard) sweep(now time.Time) {
	for key, c := range g.callers {
		full := g.limits.rate <= 0 || c.tokens+now.Sub(c.at).Seconds()*g.limits.rate >= g.limits.burst
		calm := c.fails == 0 || now.Sub(c.failsFrom) >= g.limits.window
		if full && calm && !now.Before(c.bannedUntil) {
			delete(g.callers, key)
		}
	}
	g.swept = now
}

// reject считает отказ и не чаще раза в минуту пишет в журнал, сколько их
// набралось. Только число: адрес в журнал не идёт (docs/privacy.md). Без
// отдельной горутины с таймером: обработчик живёт, пока жив сервер, и
// останавливать её было бы некому.
func (g *floodGuard) reject() {
	now := g.now()
	g.mu.Lock()
	g.rejected++
	if now.Sub(g.reported) < time.Minute {
		g.mu.Unlock()
		return
	}
	n := g.rejected
	g.rejected, g.reported = 0, now
	g.mu.Unlock()
	log.Printf("панель отклонила запросов: %d — слишком часто с адреса, после перебора токенов или сверх предела в работе; следующая такая строка не раньше чем через минуту", n)
}

// trackedCount — сколько адресов сейчас в памяти.
func (g *floodGuard) trackedCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.callers)
}

// seconds — значение Retry-After: целые секунды, не меньше одной.
func seconds(d time.Duration) string {
	s := int64(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return strconv.FormatInt(s, 10)
}

// statusWriter запоминает код ответа: неудачу видно только по нему.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap отдаёт исходный ответ http.ResponseController: через него
// обработчики сбрасывают буфер и двигают сроки, и обёртка не должна у них
// это отнимать.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
