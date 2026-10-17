//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Проверка после подключения.
//
// «Подключено» в окне значит, что нода ответила на хендшейк. Человеку же
// важно другое — открываются ли сайты, — и между этими двумя вещами на
// компьютере стоит много чужого: прокси, второй VPN, удалённый адаптер. Окно
// писало «Подключено», а браузер не открывал ничего, и виноватым выглядела
// Marvia.
//
// Поэтому после подключения программа сама открывает короткую страницу
// двумя путями: как обычная программа — через адаптер, маршруты и DNS
// Windows, — и напрямую через ядро туннеля, мимо всего этого. Разница
// между ответами и есть диагноз: не открылось первым путём, а вторым
// открылось — мешает что-то на компьютере; не открылось обоими — нода.

// probeURLs — страницы с пустым ответом 204: их держат ровно для проверки
// связи, они крошечные и не ставят куки. Два разных хозяина — чтобы
// недоступность одного не выглядела поломкой у человека.
var probeURLs = []string{
	"http://connectivitycheck.gstatic.com/generate_204",
	"http://cp.cloudflare.com/generate_204",
}

// probeTimeout — сколько ждать одну страницу. Нода на другом конце света
// отвечает за секунду; дольше пяти — для человека уже «не открывается».
const probeTimeout = 8 * time.Second

// checkDelay — пауза перед проверкой: Windows применяет маршруты и DNS
// нового адаптера не мгновенно, и первая секунда дала бы ложную тревогу.
const checkDelay = 2 * time.Second

// probeSystem открывает страницу так, как её открыл бы браузер без прокси:
// через маршруты и DNS Windows.
var probeSystem = func(ctx context.Context) error {
	return probeWith(ctx, &http.Transport{Proxy: nil})
}

// probeTunnel открывает страницу через ядро туннеля, мимо адаптера.
var probeTunnel = func(ctx context.Context, d client.Backend) error {
	if d == nil {
		return errors.New("туннеля нет")
	}
	return probeWith(ctx, &http.Transport{
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
	})
}

func probeWith(ctx context.Context, transport *http.Transport) error {
	defer transport.CloseIdleConnections()
	hc := &http.Client{Transport: transport, Timeout: probeTimeout}
	var last error
	for _, url := range probeURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := hc.Do(req)
		if err != nil {
			last = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNoContent {
			return nil
		}
		last = fmt.Errorf("ответ %d", resp.StatusCode)
	}
	return last
}

// selfCheck проверяет подключение и запоминает вывод для окна.
func (c *Controller) selfCheck(ctx context.Context, d client.Backend) {
	if d == nil {
		return
	}
	select {
	case <-time.After(checkDelay):
	case <-ctx.Done():
		return
	}
	verdict, err := diagnose(ctx, d)
	if ctx.Err() != nil || !c.keepVerdict(ctx, verdict) {
		return
	}
	if verdict == "" {
		c.log.add("%s", say("logCheckOK"))
		return
	}
	c.log.add("%s", sayf("logCheckFailed", err))
	c.log.add("%s", c.checkTextFor(verdict, d.Node().Title()))
}

// diagnose — пусто, если страница открылась как у обычной программы;
// иначе checkPC или checkNode и ошибка первого пути.
func diagnose(ctx context.Context, d client.Backend) (string, error) {
	err := probeSystem(ctx)
	if err == nil {
		return "", nil
	}
	if probeTunnel(ctx, d) != nil {
		return "checkNode", err
	}
	return "checkPC", err
}

// keepVerdict запоминает вывод, если туннель всё тот же.
func (c *Controller) keepVerdict(ctx context.Context, verdict string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connectCtx != ctx {
		return false
	}
	c.check, c.checkNode = verdict, c.node.Title()
	return true
}

// Recheck — «Проверить ещё раз»: та же проверка, но сразу и с ответом.
func (c *Controller) Recheck() error {
	c.mu.Lock()
	ctx, d := c.connectCtx, c.dialer
	connected := c.state == StateConnected || c.state == StateStalled
	c.mu.Unlock()
	if !connected || ctx == nil {
		return errors.New(say("notConnected"))
	}
	verdict, _ := diagnose(ctx, d)
	c.keepVerdict(ctx, verdict)
	return nil
}

func (c *Controller) currentDialer() client.Backend {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dialer
}

// checkText — вывод проверки словами; зовётся под замком.
func (c *Controller) checkText() string {
	if c.state != StateConnected && c.state != StateStalled {
		return ""
	}
	return c.checkTextFor(c.check, c.checkNode)
}

func (c *Controller) checkTextFor(verdict, node string) string {
	switch verdict {
	case "checkPC":
		return say("checkPC")
	case "checkNode":
		return sayf("checkNode", node)
	}
	return ""
}

// closable — программы, которые «Починить» закрывает целиком. WARP среди
// них нет: его достаточно отключить, и после Marvia он вернётся сам.
func closable() []string {
	var names []string
	for _, rule := range conflictRules {
		if rule.key != "conflictWarp" {
			names = append(names, rule.names...)
		}
	}
	return names
}

// closePrograms — закрытие процессов по имени; подменяется в тестах.
var closePrograms = terminateByName

// Fix — кнопка «Починить».
//
// Делает по нажатию то, чего программа сама не делает: закрывает чужие
// VPN-клиенты — вернуть их в прежнее
// состояние нельзя, поэтому только с согласия человека. Остальное —
// отключение WARP на время подключения, снятие брошенного прокси —
// делает и без настройки «Убирать помехи сам»: нажатие и есть согласие.
// В конце туннель поднимается заново: адаптер, который удалил закрытый
// сейчас Hiddify, сам не вернётся.
func (c *Controller) Fix() ([]string, error) {
	var done []string
	c.mu.Lock()
	up := c.state != StateIdle
	c.conflictsAt = time.Time{}
	c.mu.Unlock()
	// Без туннеля останавливать нечего: вернуть было бы некому до
	// следующего «Отключить», а без VPN человеку нужен и WARP.
	if up {
		c.fixer.pause()
	}
	if n := closePrograms(closable()); n > 0 {
		done = append(done, sayf("fixClosed", n))
		// Закрытая программа отпускает порт прокси не мгновенно: без паузы
		// прокси выглядел бы живым и остался бы на месте.
		time.Sleep(time.Second)
	}
	if c.dropDeadProxy() {
		done = append(done, say("fixProxy"))
	}

	if up {
		c.teardown()
		if err := c.Connect(); err != nil {
			return done, err
		}
		done = append(done, say("fixReconnect"))
	}
	if len(done) == 0 {
		done = append(done, say("fixNothing"))
	}
	return done, nil
}
