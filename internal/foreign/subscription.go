package foreign

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/httpguard"
	"github.com/jytt8u/marvia/internal/seller"
)

// Subscription — чужая подписка: ноды и, если продавец их сообщил, остаток и
// срок.
type Subscription struct {
	Links []Link
	// Skipped — сколько строк не разобралось: чужие форматы, плагины. Экрану
	// это нужно, чтобы не делать вид, что продавец дал меньше нод, чем дал.
	Skipped int

	Upload, Download, Total int64
	Expire                  time.Time

	// Seller — поддержка, продление и объявление из заголовков чужой
	// панели (support-url, profile-web-page-url, announce). Правила те же,
	// что для своей: см. internal/seller. Чужой панели доверия ещё меньше,
	// чем своей, — её мог поднять кто угодно.
	Seller seller.Info
}

// Used — расход по заголовку. Сумма не переполняется: два огромных числа
// от чужой панели дали бы отрицательный расход.
func (s Subscription) Used() int64 {
	if s.Upload > math.MaxInt64-s.Download {
		return math.MaxInt64
	}
	return s.Upload + s.Download
}

// Remaining — сколько трафика осталось; -1 — без ограничения.
func (s Subscription) Remaining() int64 {
	if s.Total <= 0 {
		return -1
	}
	return max(0, s.Total-s.Used())
}

// ParseList разбирает тело подписки: ссылки по строке, как есть или в
// base64. Так отдают подписки 3x-ui, Marzban, Remnawave и сама панель Marvia
// в режиме «для чужих клиентов».
func ParseList(body []byte) Subscription {
	text := strings.TrimSpace(string(body))
	if !strings.Contains(text, "://") {
		if dec, err := decodeBase64(strings.Join(strings.Fields(text), "")); err == nil {
			text = string(dec)
		}
	}
	var sub Subscription
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		l, err := Parse(line)
		if err != nil {
			sub.Skipped++
			continue
		}
		sub.Links = append(sub.Links, l)
	}
	return sub
}

// ParseUserinfo разбирает subscription-userinfo: «upload=1; download=2;
// total=3; expire=1735689600». Заголовок не стандарт, но пишут его все панели.
func (s *Subscription) ParseUserinfo(h string) {
	for _, part := range strings.Split(h, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		// Отрицательного расхода и лимита не бывает. Чужая панель, приславшая
		// его, нарисовала бы остаток больше лимита; считаем такое поле
		// незаданным, как и неразборчивое.
		if err != nil || n < 0 {
			continue
		}
		switch strings.ToLower(k) {
		case "upload":
			s.Upload = n
		case "download":
			s.Download = n
		case "total":
			s.Total = n
		case "expire":
			if n > 0 {
				s.Expire = time.Unix(n, 0)
			}
		}
	}
}

// maxBody — больше подписка не бывает: тысяча нод — это сотни килобайт.
const maxBody = 4 << 20

// Meta — то из заголовков чужой подписки, что клиент хранит рядом с телом.
type Meta struct {
	Userinfo string
	Seller   seller.Info
}

// FetchRaw забирает тело чужой подписки и её заголовки: остаток, поддержку,
// продление, объявление. Тело — как есть, чтобы его можно было положить в
// кэш и разобрать потом тем же ParseList; заголовки о продавце — уже
// вычищенными.
//
// Представляемся v2rayNG: панели выбирают формат ответа по User-Agent, и на
// незнакомый многие отдают страницу для браузера, а не список. Ошибка — без
// адреса: в нём токен подписки, а ошибки уходят в журнал.
func FetchRaw(ctx context.Context, url string) (body []byte, meta Meta, err error) {
	return FetchRawWithClient(ctx, url, http.DefaultClient)
}

// FetchRawWithClient позволяет обновлять чужую подписку внутри туннеля,
// сохраняя ограничения тела, заголовки продавца и защиту от потери TLS.
func FetchRawWithClient(ctx context.Context, url string, httpClient *http.Client) (body []byte, meta Meta, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, Meta{}, errors.New("адрес подписки не разбирается")
	}
	req.Header.Set("User-Agent", "v2rayNG/1.10.0")
	resp, err := httpguard.SubscriptionClient(httpClient).Do(req)
	if err != nil {
		return nil, Meta{}, errors.New("подписка недоступна")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, Meta{}, fmt.Errorf("подписка ответила %s", resp.Status)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, Meta{}, errors.New("подписка оборвалась на полуслове")
	}
	return body, Meta{
		Userinfo: resp.Header.Get("Subscription-Userinfo"),
		Seller:   seller.FromHeader(resp.Header),
	}, nil
}

// Usable — подписка годится для подключения; иначе — почему нет.
func (s Subscription) Usable() error {
	if len(s.Links) > 0 {
		return nil
	}
	if s.Skipped > 0 {
		return fmt.Errorf("в подписке %d нод, и ни одну клиент не понимает", s.Skipped)
	}
	return errors.New("в подписке нет ни одной ноды")
}
