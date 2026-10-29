package mobile

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Полевой замер блокировок — кнопка для добровольца в «Диагностике».
//
// Почти у всех идей против белых списков и заморозки нет эталона: что
// именно режет ТСПУ на этой SIM сегодня, публично не описано, а поведение
// рваное — по оператору, району, базовой станции. Строить обход вслепую
// значит подтверждать свои догадки. Этот замер отвечает на вопросы, от
// которых зависит, что строить:
//
//   - включён ли режим белого списка: российское открывается, зарубежное нет;
//   - замерзает ли передача после ~16 КБ к зарубежному хостеру, а к
//     российскому — нет (заморозка по адресу, а не по содержимому);
//   - замерзает ли обычный HTTPS к адресу нашей ноды так же, как VP1 к ней
//     (если да — режут адрес, и маскировка формы трафика не поможет);
//   - открывается ли российское объектное хранилище в этом режиме.
//
// Идёт только при выключенном VPN: мерится сеть SIM-карты как есть. Всё —
// мимо туннеля (приложение и так из него исключено). Отчёт остаётся на
// телефоне; отправляет его человек руками.

// fieldSteps — шаги по порядку: от того, что не зависит от продавца, к его
// нодам.
var fieldSteps = []string{"whitelist", "freeze_ru", "freeze_abroad", "freeze_cdn", "nodes_tls", "nodes_vp1", "storage"}

// FieldSteps — коды шагов полевого замера через запятую.
func FieldSteps() string { return strings.Join(fieldSteps, ",") }

// Контрольные адреса. Переменные — ради тестов.
var (
	// fieldDomestic — сайты, которые держат в белом списке: государство,
	// поиск, соцсеть.
	fieldDomestic = []string{"https://www.gosuslugi.ru/", "https://ya.ru/", "https://vk.com/"}
	// fieldForeign — зарубежные, которых в белом списке нет.
	fieldForeign = []string{"https://www.wikipedia.org/", "https://github.com/", "https://www.example.com/"}
	// Большие файлы у трёх видов хозяев: российский, зарубежный хостер
	// (там же, где обычно стоят ноды), зарубежный CDN.
	fieldFreezeRU     = "https://mirror.yandex.ru/ubuntu/ls-lR.gz"
	// OVH, а не Hetzner: Hetzner (fsn1, nbg1, hel1, ash) и Leaseweb 29.10.2026
	// рвали соединение сразу после рукопожатия любому TLS из Go и uTLS, хотя
	// curl с того же компьютера качал. Причина не выяснена; контрольный адрес
	// должен отвечать, иначе замер мерил бы его странности, а не сеть.
	fieldFreezeAbroad = "https://proof.ovh.net/files/10Mb.dat"
	fieldFreezeCDN    = "https://speed.cloudflare.com/__down?bytes=262144"
	// fieldStorage — объектное хранилище российского облака: на нём держался
	// бы запасной путь к подписке, если оно открывается в белом списке.
	fieldStorage = "https://storage.yandexcloud.net/"
)

const (
	// fieldLimit — сколько качать. Заморозку описывают на 15–20 КБ; 256 КБ
	// с запасом показывают, прошла передача порог или нет.
	fieldLimit = 256 << 10
	// fieldSample — сколько просить у ноды по VP1 (не больше vp1.MaxSpeedSample).
	fieldSample = 128 << 10
	// fieldTotal — потолок на один замер.
	fieldTotal = 25 * time.Second
	// fieldNodeLimit — сколько нод мерить: вывод ясен по первым.
	fieldNodeLimit = 4
)

// fieldStall — сколько тишины считать заморозкой. Живой канал за шесть
// секунд хоть что-то приносит. Переменная — ради тестов.
var fieldStall = 6 * time.Second

// Исход одной передачи.
const (
	fieldFull    = "ok"      // докачалось до предела
	fieldFreeze  = "freeze"  // пошло и встало — заморозка
	fieldBlocked = "blocked" // не открылось вовсе
	fieldShort   = "short"   // ответило и закончило раньше предела — мерить нечего
	fieldOff     = "off"     // шаг не применим
)

// fieldStep — итог шага. Для whitelist N/Of — российские, N2/Of2 —
// зарубежные; для нод — сколько прошло из скольких, а по каждой — Detail.
type fieldStep struct {
	ID     string      `json:"id"`
	St     string      `json:"st"`
	N      int         `json:"n,omitempty"`
	Of     int         `json:"of,omitempty"`
	N2     int         `json:"n2,omitempty"`
	Of2    int         `json:"of2,omitempty"`
	Bytes  int64       `json:"bytes,omitempty"`
	MS     int64       `json:"ms,omitempty"`
	Why    string      `json:"why,omitempty"`
	Detail []fieldNode `json:"nodes,omitempty"`
}

// fieldNode — результат по одной ноде: её имя у продавца (не адрес) и исход.
type fieldNode struct {
	Name  string `json:"name"`
	St    string `json:"st"`
	Bytes int64  `json:"bytes"`
}

// Field проходит шаг полевого замера и отдаёт итог JSON.
func (d *Diagnostics) Field(id string) string {
	out, _ := json.Marshal(d.field(id))
	return string(out)
}

func (d *Diagnostics) field(id string) fieldStep {
	if _, _, up := d.up(); up {
		// С поднятым туннелем мерилась бы не сеть SIM, а наш же туннель.
		return fieldStep{ID: id, St: fieldOff, Why: "vpn_on"}
	}
	ctx := d.ctx
	switch id {
	case "whitelist":
		dom := reachable(ctx, fieldDomestic)
		abroad := reachable(ctx, fieldForeign)
		return judgeWhitelist(dom, len(fieldDomestic), abroad, len(fieldForeign))
	case "freeze_ru":
		return transferStep(id, fetchUntilStall(ctx, directHTTP(), fieldFreezeRU))
	case "freeze_abroad":
		return transferStep(id, fetchUntilStall(ctx, directHTTP(), fieldFreezeAbroad))
	case "freeze_cdn":
		return transferStep(id, fetchUntilStall(ctx, directHTTP(), fieldFreezeCDN))
	case "nodes_tls", "nodes_vp1":
		return d.fieldNodes(ctx, id)
	case "storage":
		if reachable(ctx, []string{fieldStorage}) == 1 {
			return fieldStep{ID: id, St: fieldFull}
		}
		return fieldStep{ID: id, St: fieldBlocked}
	}
	return fieldStep{ID: id, St: fieldOff}
}

// judgeWhitelist — режим белого списка: российское открывается, зарубежное
// нет. Ни того ни другого — сети нет вовсе, и про белый список это ничего
// не говорит.
func judgeWhitelist(dom, domOf, abroad, abroadOf int) fieldStep {
	s := fieldStep{ID: "whitelist", N: dom, Of: domOf, N2: abroad, Of2: abroadOf}
	switch {
	case dom == 0 && abroad == 0:
		s.St = fieldBlocked
	case abroad == 0:
		s.St = "whitelist"
	default:
		s.St = "open"
	}
	return s
}

// transfer — что вышло у одной передачи.
type transfer struct {
	st    string
	bytes int64
	ms    int64
}

func transferStep(id string, t transfer) fieldStep {
	return fieldStep{ID: id, St: t.st, Bytes: t.bytes, MS: t.ms}
}

// reachable — сколько адресов ответили хоть чем-то по HTTPS. Код ответа не
// важен: 403 или переадресация — это сайт, который открылся.
func reachable(ctx context.Context, urls []string) int {
	var n atomic.Int32
	done := make(chan struct{}, len(urls))
	for _, u := range urls {
		go func() {
			defer func() { done <- struct{}{} }()
			c, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(c, http.MethodGet, u, nil)
			if err != nil {
				return
			}
			hc := directHTTP()
			// Переадресацию не идём: ответ сайта и так доказывает, что он
			// открылся, а следующий адрес мог бы оказаться зарубежным.
			hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			resp, err := hc.Do(req)
			if err != nil {
				return
			}
			_ = resp.Body.Close()
			n.Add(1)
		}()
	}
	for range urls {
		<-done
	}
	return int(n.Load())
}

// fetchUntilStall качает адрес до fieldLimit и замечает, если передача
// встала: fieldStall без единого байта после начала — заморозка.
func fetchUntilStall(ctx context.Context, hc *http.Client, url string) transfer {
	start := time.Now()
	c, cancel := context.WithTimeout(ctx, fieldTotal)
	defer cancel()
	// Сторож тишины: сбрасывается каждым пришедшим куском. Сработал —
	// отменяет запрос, и мы знаем, что это тишина, а не ошибка сети.
	var stalled atomic.Bool
	watchdog := time.AfterFunc(fieldStall, func() { stalled.Store(true); cancel() })
	defer watchdog.Stop()

	req, err := http.NewRequestWithContext(c, http.MethodGet, url, nil)
	if err != nil {
		return transfer{st: fieldBlocked}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return transfer{st: fieldBlocked, ms: millis(time.Since(start))}
	}
	defer resp.Body.Close()
	watchdog.Reset(fieldStall)

	var got int64
	buf := make([]byte, 16<<10)
	for got < fieldLimit {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			got += int64(n)
			watchdog.Reset(fieldStall)
		}
		if err != nil && got >= fieldLimit {
			// Последний кусок пришёл вместе с концом файла: предел пройден.
			break
		}
		if err != nil {
			ms := millis(time.Since(start))
			switch {
			case stalled.Load() || errors.Is(err, context.DeadlineExceeded) && got > 0:
				return transfer{st: fieldFreeze, bytes: got, ms: ms}
			case errors.Is(err, io.EOF):
				return transfer{st: fieldShort, bytes: got, ms: ms}
			case got == 0:
				return transfer{st: fieldBlocked, ms: ms}
			default:
				return transfer{st: fieldFreeze, bytes: got, ms: ms}
			}
		}
	}
	return transfer{st: fieldFull, bytes: got, ms: millis(time.Since(start))}
}

// fieldNodes мерит ноды рабочей подписки двумя способами. nodes_tls —
// обычный HTTPS к адресу ноды по её имени SNI: у REALITY это сайт
// прикрытия, у TLS — сама нода. nodes_vp1 — наш протокол и порция данных.
// Если обычный HTTPS к тому же адресу замерзает так же, как VP1, режут
// адрес, а не протокол.
func (d *Diagnostics) fieldNodes(ctx context.Context, id string) fieldStep {
	if isForeign(d.link) {
		return fieldStep{ID: id, St: fieldOff, Why: "foreign"}
	}
	account, err := client.ParseAccountLink(d.link)
	if err != nil {
		return fieldStep{ID: id, St: fieldOff, Why: "no_key"}
	}
	cached, err := client.LoadCache(accountCachePath(d.cacheDir, account.SubscriptionURL), account.SubscriptionURL)
	if err != nil || len(cached.Nodes()) == 0 {
		return fieldStep{ID: id, St: fieldOff, Why: "no_nodes"}
	}
	nodes := cached.Nodes()
	if len(nodes) > fieldNodeLimit {
		nodes = nodes[:fieldNodeLimit]
	}

	s := fieldStep{ID: id, St: fieldFull}
	for _, n := range nodes {
		var t transfer
		if id == "nodes_tls" {
			if n.Transport() == client.TransportWS {
				// За CDN адрес принадлежит не ноде: мерился бы CDN.
				s.Detail = append(s.Detail, fieldNode{Name: n.Title(), St: fieldOff})
				continue
			}
			t = fetchUntilStall(ctx, nodeHTTP(n), "https://"+sniOf(n)+"/")
		} else {
			t = fieldVP1(ctx, n, account, d.fragment)
		}
		s.Of++
		if t.st == fieldFull || t.st == fieldShort {
			s.N++
		}
		s.Detail = append(s.Detail, fieldNode{Name: n.Title(), St: t.st, Bytes: t.bytes})
	}
	switch {
	case s.Of == 0:
		s.St = fieldOff
	case s.N == 0:
		s.St = fieldBlocked
		for _, n := range s.Detail {
			if n.St == fieldFreeze {
				s.St = fieldFreeze
			}
		}
	case s.N < s.Of:
		s.St = "partial"
	}
	return s
}

// fieldVP1 — порция данных с ноды по VP1, как замер скорости. Ошибка после
// того, как рукопожатие прошло, — это и есть заморозка: рукопожатие
// укладывается в первые килобайты.
func fieldVP1(ctx context.Context, n client.Node, account client.Account, fragment bool) transfer {
	start := time.Now()
	key, err := vp1.KeyPairFromPrivate(account.PrivateKey)
	if err != nil {
		return transfer{st: fieldBlocked}
	}
	dialer, err := client.NewDialer(n, key, client.Options{Fragment: fragment})
	if err != nil {
		return transfer{st: fieldBlocked}
	}
	defer dialer.Close()
	c, cancel := context.WithTimeout(ctx, fieldTotal)
	defer cancel()
	if err := dialer.Warmup(c); err != nil {
		return transfer{st: fieldBlocked, ms: millis(time.Since(start))}
	}
	if _, err := dialer.MeasureFetch(c, fieldSample); err != nil {
		return transfer{st: fieldFreeze, ms: millis(time.Since(start))}
	}
	return transfer{st: fieldFull, bytes: fieldSample, ms: millis(time.Since(start))}
}

// sniOf — имя, с которым идти к ноде: её SNI, иначе хост из адреса.
func sniOf(n client.Node) string {
	if n.SNI != "" {
		return n.SNI
	}
	host, _, err := net.SplitHostPort(n.Address)
	if err != nil {
		return n.Address
	}
	return host
}

// nodeHTTP — HTTPS-клиент, который любое имя ведёт на адрес ноды. Сертификат
// проверяется как обычно: у REALITY его отдаёт настоящий сайт прикрытия.
func nodeHTTP(n client.Node) *http.Client {
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return directDial(ctx, network, n.Address)
		},
		TLSClientConfig: &tls.Config{ServerName: sniOf(n), MinVersion: tls.VersionTLS12},
	}
	return &http.Client{Transport: tr}
}
