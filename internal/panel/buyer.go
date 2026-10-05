package panel

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/seller"
	"rsc.io/qr"
	"golang.org/x/text/language"
)

// Отдельный шаблон позволяет менять страницу покупателя, не задевая панель.
// html/template экранирует и метку доступа, и чужое объявление продавца.
//
//go:embed web/buyer.html
var buyerHTML string

var buyerTemplate = template.Must(template.New("buyer").Parse(buyerHTML))
var buyerLanguages = language.NewMatcher([]language.Tag{language.Russian, language.English})

// Только явный запрос HTML от браузера меняет старый ответ. У Happ можно
// подменить User-Agent на Chrome, у Hiddify он похож на Clash: распознавать
// приложение по одному заголовку нельзя. Любой явный format имеет приоритет,
// а ссылка из QR закрепляет base64 даже для приложения с подменённым UA.
func wantsBuyerPage(r *http.Request) bool {
	if r.URL.Query().Has("format") {
		return false
	}
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" && mode != "navigate" {
		return false
	}
	if dest := r.Header.Get("Sec-Fetch-Dest"); dest != "" && dest != "document" {
		return false
	}
	agent := strings.ToLower(r.UserAgent())
	for _, client := range []string{
		"happ", "v2ray", "hiddify", "marvia", "veil", "clash", "sing-box",
		"streisand", "shadowrocket", "nekobox", "karing", "curl/", "wget/",
		"go-http-client", "dart/", "okhttp", "cfnetwork", "cronet",
	} {
		if strings.Contains(agent, client) {
			return false
		}
	}
	if !strings.HasPrefix(agent, "mozilla/5.0 ") ||
		!(strings.Contains(agent, "chrome/") || strings.Contains(agent, "firefox/") ||
			strings.Contains(agent, "safari/") || strings.Contains(agent, "fxios/")) {
		return false
	}
	for _, value := range r.Header.Values("Accept") {
		for _, part := range strings.Split(value, ",") {
			kind, params, err := mime.ParseMediaType(strings.TrimSpace(part))
			if err != nil || kind != "text/html" {
				continue
			}
			if raw, exists := params["q"]; exists {
				q, err := strconv.ParseFloat(raw, 64)
				if err != nil || !(q > 0 && q <= 1) {
					continue
				}
			}
			return true
		}
	}
	return false
}

type buyerText struct {
	Language, Title, Intro, Status, Term, Traffic, Active, Disabled, Expired, Exhausted string
	NoExpiry, ValidUntil, ExpiredOn, Unlimited, Remaining, Used, Unit                   string
	Connect, Instructions, Link, QR, Downloads, Download, NoApps, NativeKey             string
	Renew, Support, Announcement, Private                                               string
}

var buyerRussian = buyerText{
	Language: "ru", Title: "Ваш доступ", Intro: "Срок, трафик и всё для подключения — в одном месте.",
	Status: "Состояние", Term: "Срок доступа", Traffic: "Трафик", Active: "Доступ работает",
	Disabled: "Доступ отключён продавцом", Expired: "Срок закончился", Exhausted: "Трафик закончился",
	NoExpiry: "Без ограничения по сроку", ValidUntil: "Действует до %s", ExpiredOn: "Срок закончился %s",
	Unlimited: "Без ограничения по трафику", Remaining: "Осталось %s из %s", Used: "Использовано %s",
	Unit: "Б", Connect: "Добавьте подписку в приложение",
	Instructions: "В Happ, v2RayTun или Hiddify выберите добавление подписки по ссылке. Вставьте адрес ниже или отсканируйте QR-код из приложения.",
	Link:         "Ссылка подписки", QR: "QR-код ссылки подписки", Downloads: "Нужно приложение?",
	Download: "Скачать для %s", NoApps: "Продавец пока не выложил приложения. Попросите у него файл для вашей системы.",
	NativeKey: "Для подключения через VP1 в Marvia используйте личный ключ marvia:// из сообщения продавца. Ссылка подписки его не заменяет.",
	Renew:     "Продлить", Support: "Поддержка", Announcement: "Объявление продавца",
	Private: "Эта ссылка даёт доступ к подписке. Не публикуйте её и QR-код.",
}

var buyerEnglish = buyerText{
	Language: "en", Title: "Your access", Intro: "Expiry, traffic and everything you need to connect.",
	Status: "Status", Term: "Access period", Traffic: "Traffic", Active: "Access is active",
	Disabled: "Access disabled by the seller", Expired: "Access has expired", Exhausted: "Traffic allowance used up",
	NoExpiry: "No expiry date", ValidUntil: "Valid until %s", ExpiredOn: "Expired on %s",
	Unlimited: "Unlimited traffic", Remaining: "%s left out of %s", Used: "%s used",
	Unit: "B", Connect: "Add your subscription to an app",
	Instructions: "In Happ, v2RayTun or Hiddify, choose to add a subscription by URL. Paste the address below or scan the QR code from the app.",
	Link:         "Subscription URL", QR: "Subscription URL QR code", Downloads: "Need an app?",
	Download: "Download for %s", NoApps: "The seller has not uploaded any apps yet. Ask them for a file for your system.",
	NativeKey: "To connect using VP1 in Marvia, use the personal marvia:// key from your seller's message. The subscription URL does not replace it.",
	Renew:     "Renew", Support: "Support", Announcement: "Seller announcement",
	Private: "This URL grants access to your subscription. Keep it and the QR code private.",
}

type buyerApp struct {
	URL, Label string
}

type buyerView struct {
	Text                           buyerText
	Nonce, Label, Status, Term     string
	Traffic, Used, SubscriptionURL string
	Attention                      bool
	Seller                         buyerSeller
	Apps                           []buyerApp
	QR                             template.URL
}

type buyerSeller struct {
	SupportURL, RenewURL template.URL
	Announce             string
}

func (a *API) buyerPage(w http.ResponseWriter, r *http.Request, user User) {
	text := buyerRussian
	_, index := language.MatchStrings(buyerLanguages, r.Header.Get("Accept-Language"))
	if index == 1 {
		text = buyerEnglish
	}
	view := buyerDetails(user, text, time.Now())
	// Адрес берётся из настройки продавца, а не из Host или Forwarded:
	// чужой заголовок не должен подменить ссылку внутри QR-кода.
	view.SubscriptionURL = a.subURL(user.SubToken) + "?format=base64"
	info, _ := a.store.SellerInfo(r.Context())
	info = seller.Clean(info)
	// html/template по умолчанию запрещает tg://. Доверяем только ссылкам,
	// прошедшим общую проверку продавца; экранирование атрибутов остаётся.
	view.Seller = buyerSeller{SupportURL: template.URL(info.SupportURL), RenewURL: template.URL(info.RenewURL), Announce: info.Announce}
	order := []string{"windows", "android"}
	if strings.Contains(strings.ToLower(r.UserAgent()), "android") {
		order = []string{"android", "windows"}
	}
	for _, name := range order {
		if _, exists := a.availableApp(name); exists {
			platform := "Windows"
			if name == "android" {
				platform = "Android"
			}
			view.Apps = append(view.Apps, buyerApp{
				URL:   "/sub/" + url.PathEscape(user.SubToken) + "/app/" + name,
				Label: fmt.Sprintf(text.Download, platform),
			})
		}
	}
	// Тот же кодировщик, что рисует QR в панели продавца (qr.go): одна
	// библиотека на одно дело, а не две.
	code, err := qr.Encode(view.SubscriptionURL, qr.M)
	var png []byte
	if err == nil {
		png = code.PNG()
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "не удалось подготовить QR-код подписки")
		return
	}
	// PNG создаём сами; чужой текст никогда не становится доверенной разметкой.
	view.QR = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))
	view.Nonce, err = newNonce()
	if err != nil {
		fail(w, http.StatusInternalServerError, "не удалось подготовить страницу подписки")
		return
	}
	var body bytes.Buffer
	if err := buyerTemplate.Execute(&body, view); err != nil {
		fail(w, http.StatusInternalServerError, "не удалось подготовить страницу подписки")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Language", text.Language)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'none'; style-src 'nonce-"+view.Nonce+"'; img-src data:; form-action 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	// Открытие не является действием продавца: IP, токен и просмотр не
	// записываются ни в журнал панели, ни в серверный журнал.
	_, _ = w.Write(body.Bytes())
}

func buyerDetails(user User, text buyerText, now time.Time) buyerView {
	view := buyerView{Text: text, Label: user.Label, Status: text.Active, Term: text.NoExpiry, Traffic: text.Unlimited}
	expired := user.ExpiresAt != nil && !user.ExpiresAt.After(now)
	exhausted := user.TrafficLimit > 0 && user.Used >= user.TrafficLimit
	switch {
	case !user.Enabled:
		view.Status = text.Disabled
	case expired:
		view.Status = text.Expired
	case exhausted:
		view.Status = text.Exhausted
	}
	view.Attention = !user.Enabled || expired || exhausted
	if user.ExpiresAt != nil {
		format := "02.01.2006, 15:04 UTC"
		if text.Language == "en" {
			format = "02 Jan 2006, 15:04 UTC"
		}
		phrase := text.ValidUntil
		if expired {
			phrase = text.ExpiredOn
		}
		view.Term = fmt.Sprintf(phrase, user.ExpiresAt.UTC().Format(format))
	}
	if user.TrafficLimit > 0 {
		remaining := max(int64(0), user.TrafficLimit-user.Used)
		view.Traffic = fmt.Sprintf(text.Remaining, buyerBytes(remaining, text), buyerBytes(user.TrafficLimit, text))
	}
	view.Used = fmt.Sprintf(text.Used, buyerBytes(user.Used, text))
	return view
}

func buyerBytes(n int64, text buyerText) string {
	units := []string{text.Unit, "КБ", "МБ", "ГБ", "ТБ", "ПБ", "ЭБ"}
	if text.Language == "en" {
		units = []string{text.Unit, "KB", "MB", "GB", "TB", "PB", "EB"}
	}
	value, unit := float64(max(int64(0), n)), 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	number := strconv.FormatFloat(value, 'f', 1, 64)
	number = strings.TrimSuffix(number, ".0")
	if text.Language == "ru" {
		number = strings.ReplaceAll(number, ".", ",")
	}
	return number + " " + units[unit]
}
