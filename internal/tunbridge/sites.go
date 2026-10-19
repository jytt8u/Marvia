package tunbridge

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/idna"
)

// Правила по сайтам: «этот сайт — мимо туннеля», «этот — всегда через него».
//
// Мост решает по адресу назначения (Split), а человек думает именами. Связь
// имени с адресом мост узнаёт двумя путями.
//
// Первый — ответы на запросы имён. Все они проходят через мост (Config.Names
// или открытый DNS через туннель), и адрес из ответа запоминается вместе с
// именем на срок жизни записи. Соединение к этому адресу решается по
// правилу имени — на любом порту и по UDP тоже.
//
// Второй — имя из TLS-приветствия (SNI) в начале соединения на порт 443.
// Без него правила не работали бы в браузере: Chrome, увидев у адаптера
// резолвер Cloudflare или Google, сам переходит на их DoH, и мост не видит ни
// одного имени — только соединение с резолвером. SNI к тому же точнее
// памяти: один адрес CDN обслуживает тысячи сайтов, а приветствие называет
// ровно тот, к которому идут.
//
// Цена SNI — ожидание первого пакета приложения перед дозвоном. Для TLS это
// доли миллисекунды: приветствие уходит сразу за рукопожатием TCP, а оно
// здесь, внутри машины. Для редкого протокола, где на 443 первым говорит
// сервер, — helloWait тишины, после чего решаем по адресу. Ждём только
// тогда, когда правила есть: без них решение по имени ничего не меняет.
//
// Чего правила не умеют, и это честно сказано в docs/guide.md:
//   - ECH. Браузер с DoH может зашифровать приветствие и показать наружу
//     общее имя вроде cloudflare-ech.com. Тогда имени сайта мосту не узнать.
//   - QUIC (HTTP/3 по UDP). Приветствие там зашифровано ключами из первого
//     пакета, и разбирать его значит собирать QUIC по частям. Вместо этого
//     адрес, узнанный из SNI соединения TCP, тоже запоминается: браузер
//     начинает сайт с TCP и переходит на QUIC, уже побывав там, — к этому
//     времени адрес известен. Самый первый QUIC к сайту, имя которого мост
//     не видел ни в DNS, ни в SNI, решается по адресу, как без правил.

const (
	// helloWait — сколько ждём TLS-приветствия от приложения на порту 443.
	// Приветствие уходит сразу за рукопожатием; секунда — с запасом на
	// занятую машину, а протокол, где первым говорит сервер, подождёт её
	// один раз на соединение.
	helloWait = time.Second

	// maxHello — сколько байт начала соединения готовы держать ради имени.
	// Приветствие с постквантовым ключом весит около двух килобайт; запись
	// TLS не длиннее 16 КиБ, а разрезанное на несколько записей (так делают
	// zapret и GoodbyeDPI) приветствие всё равно укладывается в две.
	maxHello = 2 * (16<<10 + 5)

	// seenFloor и seenCeiling — сколько помним адрес из ответа, какой бы
	// срок ни назвал резолвер. Снизу — потому что приложение подключается к
	// адресу и после конца TTL: браузер держит имя минуту, Windows — до
	// суток, соединения из пула живут долго. Забудь мы адрес раньше, сайт
	// посреди работы переехал бы в туннель и увидел другую страну. Сверху —
	// дольше суток Windows ответ не хранит, и через сутки адрес может
	// принадлежать уже другому сайту.
	seenFloor   = 10 * time.Minute
	seenCeiling = 24 * time.Hour

	// maxSeen — сколько адресов помним разом. Обычный день в браузере — пара
	// тысяч имён; больше держать в памяти незачем.
	maxSeen = 16384

	// maxSites — сколько правил в одном списке. Это ручной список человека,
	// а не база блокировок: тысяча — с запасом, а дальше начинается
	// подсунутый кем-то файл.
	maxSites = 1000
)

// Route — куда правила ведут соединение.
type Route int

const (
	// RouteAny — правила о цели не говорят: решает адрес (российский
	// список, домашняя сеть).
	RouteAny Route = iota
	// RouteDirect — мимо туннеля.
	RouteDirect
	// RouteTunnel — через туннель, даже если адрес в российском списке.
	RouteTunnel
)

// ErrBadSite — строка не похожа на имя сайта.
var ErrBadSite = errors.New("не похоже на имя сайта")

// ErrTooManySites — в списке больше правил, чем разумно держать руками.
var ErrTooManySites = errors.New("слишком много сайтов в списке")

// NormalizeSite приводит то, что человек вписал, к имени для правила.
//
// Вписывают по-разному: адрес из строки браузера целиком, с «www», с
// звёздочкой, кириллицей. Правило действует на имя и все его поддомены,
// поэтому звёздочка в начале не нужна, но и не мешает — «*.gosuslugi.ru» и
// «gosuslugi.ru» значат одно. Кириллица переводится в punycode: в DNS и SNI
// имя приходит именно так.
//
// Адрес вместо имени не принимается: правило по адресу молча сломалось бы,
// как только сайт сменит сервер, а человек об этом не узнал бы.
func NormalizeSite(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimPrefix(s, "*.")
	s = strings.Trim(s, ".")
	if s == "" {
		return "", ErrBadSite
	}
	if _, err := netip.ParseAddr(s); err == nil {
		return "", ErrBadSite
	}
	ascii, err := idna.Lookup.ToASCII(s)
	if err != nil || ascii == "" || len(ascii) > 253 {
		return "", ErrBadSite
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || len(label) > 63 {
			return "", ErrBadSite
		}
	}
	return ascii, nil
}

// DisplaySite — имя правила таким, каким его вписывал человек: кириллицей,
// а не punycode. Обратно NormalizeSite приводит его к тому же правилу.
func DisplaySite(name string) string {
	if u, err := idna.Display.ToUnicode(name); err == nil {
		return u
	}
	return name
}

// NormalizeSites приводит весь список и убирает повторы, сохраняя порядок:
// человек видит свой список таким, каким его вписывал.
func NormalizeSites(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	have := make(map[string]bool, len(raw))
	for _, r := range raw {
		name, err := NormalizeSite(r)
		if err != nil {
			return nil, &BadSiteError{Site: strings.TrimSpace(r)}
		}
		if !have[name] {
			have[name] = true
			out = append(out, name)
		}
	}
	if len(out) > maxSites {
		return nil, ErrTooManySites
	}
	return out, nil
}

// BadSiteError называет строку, которая не прошла: в списке из десяти имён
// «одно неверно» без указания какое — загадка.
type BadSiteError struct{ Site string }

func (e *BadSiteError) Error() string { return ErrBadSite.Error() + ": " + e.Site }

func (e *BadSiteError) Unwrap() error { return ErrBadSite }

// Sites — правила по именам и память «адрес → имена». Одна на программу, а
// не на подключение: Windows помнит ответы и после переподключения, и
// приложение пойдёт к адресу, не спрашивая имени заново.
//
// Память — только в оперативной памяти и только та, что и так лежит в кэше
// имён Windows; на диск она не пишется.
type Sites struct {
	rules atomic.Pointer[siteRules]

	mu   sync.Mutex
	seen map[netip.Addr][]seenName

	// now подменяется в проверках срока памяти.
	now func() time.Time
}

type seenName struct {
	name  string
	until time.Time
}

// NewSites заводит пустые правила.
func NewSites() *Sites {
	return &Sites{seen: make(map[netip.Addr][]seenName), now: time.Now}
}

// Set ставит правила. Действуют они сразу, на новые соединения: память
// хранит имена, а не готовые решения, поэтому новое правило применяется и к
// адресам, узнанным до него.
//
// Имена ожидаются уже приведёнными (NormalizeSites); неприводимые молча
// пропускаются — испорченный файл настроек не должен ронять туннель.
func (s *Sites) Set(direct, tunnel []string) {
	if s == nil {
		return
	}
	r := &siteRules{direct: nameSet(direct), tunnel: nameSet(tunnel)}
	if len(r.direct) == 0 && len(r.tunnel) == 0 {
		r = nil
	}
	s.rules.Store(r)
}

func nameSet(list []string) map[string]bool {
	set := make(map[string]bool, len(list))
	for _, raw := range list {
		if name, err := NormalizeSite(raw); err == nil {
			set[name] = true
		}
	}
	return set
}

// active — есть ли хоть одно правило. Без правил мост не ждёт приветствия.
func (s *Sites) active() bool { return s != nil && s.rules.Load() != nil }

// Route решает по правилам. name — имя из SNI этого соединения или пусто.
//
// Имя из SNI, если оно есть, — окончательно: оно называет сайт этого
// соединения, а память говорит лишь, чьим адрес бывал. Нет имени — решает
// память: правило «через туннель» любого из имён адреса сильнее правила
// «мимо». Один адрес CDN бывает общим у сайта из обоих списков, и тогда
// ошибиться в сторону туннеля безопаснее: сайт откроется, только из другой
// страны, — а не уйдёт мимо туннеля то, что человек велел прятать.
func (s *Sites) Route(name string, ip netip.Addr) Route {
	if s == nil {
		return RouteAny
	}
	r := s.rules.Load()
	if r == nil {
		return RouteAny
	}
	if name != "" {
		return r.route(name)
	}
	now := s.now()
	s.mu.Lock()
	names := s.seen[ip.Unmap()]
	verdict := RouteAny
	for _, n := range names {
		if now.After(n.until) {
			continue
		}
		switch r.route(n.name) {
		case RouteTunnel:
			s.mu.Unlock()
			return RouteTunnel
		case RouteDirect:
			verdict = RouteDirect
		}
	}
	s.mu.Unlock()
	return verdict
}

// learn запоминает, что адрес принадлежит имени.
func (s *Sites) learn(name string, ip netip.Addr, ttl time.Duration) {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if name == "" || !ip.IsValid() {
		return
	}
	ttl = min(max(ttl, seenFloor), seenCeiling)
	now := s.now()
	until := now.Add(ttl)
	ip = ip.Unmap()

	s.mu.Lock()
	defer s.mu.Unlock()
	names := s.seen[ip]
	for i := range names {
		if names[i].name == name {
			names[i].until = until
			return
		}
	}
	if len(s.seen) >= maxSeen {
		s.forget(now)
	}
	// Живых имён у адреса немного, а мёртвые копить незачем.
	alive := names[:0]
	for _, n := range names {
		if !now.After(n.until) {
			alive = append(alive, n)
		}
	}
	s.seen[ip] = append(alive, seenName{name: name, until: until})
}

// forget освобождает место: сперва истёкшее, а если и живого слишком
// много — половину наугад (порядок обхода карты случаен). Забытый адрес
// просто решится по адресу до следующего вопроса о его имени.
func (s *Sites) forget(now time.Time) {
	for ip, names := range s.seen {
		dead := true
		for _, n := range names {
			if !now.After(n.until) {
				dead = false
				break
			}
		}
		if dead {
			delete(s.seen, ip)
		}
	}
	for ip := range s.seen {
		if len(s.seen) < maxSeen/2 {
			break
		}
		delete(s.seen, ip)
	}
}

// learnAnswer запоминает адреса из ответа на запрос имени. Имя — то, что
// спрашивало приложение, а не последнее в цепочке CNAME: правило человек
// пишет на имя сайта, а не его CDN.
func (s *Sites) learnAnswer(msg []byte) {
	if s == nil {
		return
	}
	var p dnsmessage.Parser
	head, err := p.Start(msg)
	if err != nil || !head.Response || head.RCode != dnsmessage.RCodeSuccess {
		return
	}
	q, err := p.Question()
	if err != nil {
		return
	}
	if err := p.SkipAllQuestions(); err != nil {
		return
	}
	asked := q.Name.String()
	for {
		rh, err := p.AnswerHeader()
		if err != nil {
			return
		}
		ttl := time.Duration(rh.TTL) * time.Second
		switch rh.Type {
		case dnsmessage.TypeA:
			a, err := p.AResource()
			if err != nil {
				return
			}
			s.learn(asked, netip.AddrFrom4(a.A), ttl)
		case dnsmessage.TypeAAAA:
			a, err := p.AAAAResource()
			if err != nil {
				return
			}
			s.learn(asked, netip.AddrFrom16(a.AAAA), ttl)
		default:
			if err := p.SkipAnswer(); err != nil {
				return
			}
		}
	}
}

// siteRules — два списка имён. Неизменяемы после сборки: их читает каждое
// соединение без замка.
type siteRules struct {
	direct, tunnel map[string]bool
}

// route ищет правило для имени, от самого точного к общему: правило на
// «vk.ru» сильнее правила на «ru». На одном уровне туннель сильнее обхода —
// по той же причине, что в Route.
func (r *siteRules) route(name string) Route {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	for name != "" {
		if r.tunnel[name] {
			return RouteTunnel
		}
		if r.direct[name] {
			return RouteDirect
		}
		i := strings.IndexByte(name, '.')
		if i < 0 {
			break
		}
		name = name[i+1:]
	}
	return RouteAny
}

// Имя сайта от моста к развилке передаётся в контексте дозвона: интерфейс
// Dialer принимает адрес, и менять его ради одной подсказки — значит
// менять всех, кто его реализует.
type siteKey struct{}

func withSite(ctx context.Context, name string) context.Context {
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, siteKey{}, name)
}

func siteOf(ctx context.Context) string {
	name, _ := ctx.Value(siteKey{}).(string)
	return name
}
