package transport

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/xtls/reality"
)

// REALITY — камуфляж, при котором нода не предъявляет собственный сертификат.
//
// Как это работает. Нода слушает :443 и на каждое соединение открывает своё
// соединение к настоящему чужому сайту, зеркаля туда ClientHello. Дальше
// развилка:
//
//   - гость не наш — нода просто сшивает два сокета. Сканер цензора проходит
//     полноценный TLS-хендшейк с настоящим сайтом через нас и видит его
//     подлинный сертификат, его ServerHello, его содержимое. Отличить нашу
//     ноду от зеркала того сайта нельзя, потому что она им и является;
//
//   - гость наш — он доказал это меткой, спрятанной в поле session_id
//     собственного ClientHello, и нода перехватывает разговор.
//
// Чем это лучше обычного TLS с нашим сертификатом:
//
//   - исчезает отпечаток ответа. Раньше ServerHello генерировала стандартная
//     библиотека Go, и её набор расширений отличается от nginx. Теперь ответ
//     собирается по образцу настоящего сайта;
//   - не нужен ни свой домен, ни сертификат, ни его продление;
//   - SNI указывает на чужой популярный сайт. Заблокировать его цензор не
//     готов, а блокировать по SNI — основной способ в России.
//
// Реализация взята готовой (github.com/xtls/reality) и намеренно не своя:
// это форк стандартного crypto/tls, и писать вместо него собственный TLS 1.3
// означало бы ровно то, что мы себе запретили в самом начале.

const (
	// realityMaxTimeDiff — насколько время в метке клиента может разойтись с
	// часами ноды.
	//
	// Без предела нода узнавала бы и записанное приветствие, повторённое
	// через час: цензор пересылает чужой ClientHello и видит ответ ноды, а не
	// сайта прикрытия. Пять минут — запоздалый повтор отсекается, а телефон,
	// чьи часы ушли на минуту-другую, подключается. Ушли дальше — покупатель
	// не войдёт, пока не включит время по сети.
	realityMaxTimeDiff = 5 * time.Minute

	// shortIDLen — длина короткого идентификатора клиента.
	shortIDLen = 8

	// realityKeyLen — длина ключа X25519.
	realityKeyLen = 32
)

// RealityConfig описывает серверную сторону REALITY.
type RealityConfig struct {
	// Dest — настоящий сайт, которым прикрывается нода, в виде host:port.
	//
	// К нему уходят все неопознанные гости, и от него же берётся образец
	// ответа. Выбирать нужно осмысленно: сайт должен поддерживать TLS 1.3 и
	// HTTP/2, быть популярным, не принадлежать нам и не лежать в той же
	// подсети, что нода.
	Dest string

	// ServerNames — имена, которые нода принимает в SNI. Обычно домен Dest.
	ServerNames []string

	// PrivateKey — приватный ключ ноды, X25519, 32 байта.
	PrivateKey []byte

	// ShortIDs — короткие идентификаторы клиентов в шестнадцатеричном виде,
	// до 16 символов каждый. Пустая строка тоже допустима и означает
	// нулевой идентификатор.
	ShortIDs []string

	// Debug включает подробный разбор каждого хендшейка в стандартный вывод.
	//
	// Нужен ровно для одного случая: клиент не подключается, и надо понять,
	// на чём именно нода его не узнала — ключ, идентификатор или часы.
	// В бою держать включённым нельзя: в вывод попадают ключи.
	Debug bool
}

// ListenReality оборачивает TCP-слушатель в REALITY.
func ListenReality(inner net.Listener, cfg RealityConfig) (net.Listener, error) {
	if cfg.Dest == "" {
		return nil, errors.New("не задан сайт прикрытия (dest)")
	}
	if _, _, err := net.SplitHostPort(cfg.Dest); err != nil {
		return nil, fmt.Errorf("сайт прикрытия должен быть в виде host:port, получено %q", cfg.Dest)
	}
	if len(cfg.PrivateKey) != realityKeyLen {
		return nil, fmt.Errorf("длина приватного ключа %d байт, ожидается %d", len(cfg.PrivateKey), realityKeyLen)
	}

	names := map[string]bool{}
	for _, name := range cfg.ServerNames {
		name = strings.TrimSpace(name)
		if name != "" {
			names[name] = true
		}
	}
	if len(names) == 0 {
		// Не угадываем: имя в SNI должно совпадать с тем, что клиент напишет
		// в конфиге, а из "host:port" его можно вывести неверно (например,
		// когда dest указан IP-адресом).
		return nil, errors.New("не заданы имена для SNI")
	}

	shortIDs, err := parseShortIDs(cfg.ShortIDs)
	if err != nil {
		return nil, err
	}

	config := &reality.Config{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, address)
		},
		Type:        "tcp",
		Dest:        cfg.Dest,
		ServerNames: names,
		PrivateKey:  append([]byte(nil), cfg.PrivateKey...),
		ShortIds:    shortIDs,
		MaxTimeDiff: realityMaxTimeDiff,
		Show:        cfg.Debug,

		// Возобновление сессий выключено намеренно: билет заметно сокращает
		// повторный хендшейк, и такая пара «длинный первый, короткие
		// последующие» — отдельная примета, которой у прикрываемого сайта
		// может не быть.
		SessionTicketsDisabled: true,
	}

	return newRealityListener(inner, config), nil
}

// realityListener — приём соединений REALITY без reality.NewListener.
//
// Библиотечный приёмщик пишет из горутины каждого соединения в общие
// переменные c и err своего цикла. Детектор гонок это ловит, и это не
// формальность: пока одна горутина проходит рукопожатие, цикл кладёт в c
// следующее сырое соединение, и в Accept могло уйти чужое, ещё не
// расшифрованное. Логика та же — рукопожатие в отдельной горутине, чтобы
// медленный гость не держал остальных, — но у каждой свои переменные.
type realityListener struct {
	net.Listener
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
	err   error
}

func newRealityListener(inner net.Listener, config *reality.Config) *realityListener {
	go reality.DetectPostHandshakeRecordsLens(config)
	l := &realityListener{Listener: inner, conns: make(chan net.Conn), done: make(chan struct{})}
	go func() {
		for {
			raw, err := inner.Accept()
			if err != nil {
				l.mu.Lock()
				l.err = err
				l.mu.Unlock()
				l.once.Do(func() { close(l.done) })
				return
			}
			go func(raw net.Conn) {
				// Библиотека паникует на кривом приветствии; гость не
				// должен ронять ноду.
				defer func() { _ = recover() }()
				conn, err := reality.Server(context.Background(), raw, config)
				if err != nil {
					return
				}
				select {
				case l.conns <- conn:
				case <-l.done:
					_ = conn.Close()
				}
			}(raw)
		}
	}()
	return l
}

func (l *realityListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.err == nil {
			return nil, net.ErrClosed
		}
		return nil, l.err
	}
}

func (l *realityListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() { close(l.done) })
	return err
}

// parseShortIDs разбирает короткие идентификаторы из шестнадцатеричного вида.
//
// Идентификатор нужен, чтобы нода могла различать группы клиентов и отзывать
// их по отдельности, не меняя свой ключ. Восемь байт хранятся как есть,
// короткая строка дополняется нулями справа.
func parseShortIDs(list []string) (map[[shortIDLen]byte]bool, error) {
	out := map[[shortIDLen]byte]bool{}

	if len(list) == 0 {
		// Пустой идентификатор — обычная конфигурация: клиент оставляет
		// поле sid пустым. Без единого разрешённого значения не пройдёт никто.
		out[[shortIDLen]byte{}] = true
		return out, nil
	}

	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			out[[shortIDLen]byte{}] = true
			continue
		}
		if len(raw) > shortIDLen*2 {
			return nil, fmt.Errorf("короткий идентификатор %q длиннее %d символов", raw, shortIDLen*2)
		}
		if len(raw)%2 != 0 {
			return nil, fmt.Errorf("короткий идентификатор %q должен состоять из чётного числа символов", raw)
		}
		decoded, err := hex.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("короткий идентификатор %q не шестнадцатеричный: %w", raw, err)
		}

		var id [shortIDLen]byte
		copy(id[:], decoded)
		out[id] = true
	}
	return out, nil
}

// RealityHandshakeTimeout — сколько ждём завершения хендшейка REALITY.
// Внутри он ходит к настоящему сайту, поэтому запас больше обычного.
const RealityHandshakeTimeout = 30 * time.Second
