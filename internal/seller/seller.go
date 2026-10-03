// Package seller — то, чем продавец говорит с покупателем помимо самих нод:
// куда писать, когда сломалось, где продлить и что сейчас происходит.
//
// Без этого покупателю, у которого кончился срок или пропала связь, некуда
// нажать. Он ищет переписку с продавцом, не находит и уходит — или пишет
// «у вас всё лежит» туда, где его никто не ждёт.
//
// Пакет общий для панели, нашего клиента и чтения чужих подписок намеренно:
// правила одни на все три стороны. Панель по ним отказывает продавцу (400),
// клиент по ним же чистит то, что пришло, — и расхождению между «панель
// пропустила» и «клиент понял» взяться неоткуда.
//
// Клиент панели не доверяет. Панель для покупателя — чужой сервер, а у чужой
// подписки и вовсе кто угодно. Поэтому каждое поле перепроверяется на
// приёме, а не только при сохранении.
package seller

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Заголовки подписки, по которым чужие приложения рисуют кнопки и
// объявление над профилем. Мы их и отдаём, и читаем у чужих панелей.
//
// Источники:
//   - вики Hiddify, раздел о заголовках подписки:
//     https://github.com/hiddify/HiddifyNG/wiki — support-url («Support»),
//     profile-web-page-url («Open web page»), profile-title, в том числе в
//     виде «base64:» плюс UTF-8 в base64;
//   - документация Happ для разработчиков, «App management»:
//     https://www.happ.su/main/dev-docs/app-management — support-url,
//     profile-web-page-url, announce (текстом или в base64, показывается не
//     больше 200 символов). Страница прочитана по выдержке поисковика: из
//     сборочной среды happ.su недоступен, перепроверить дословно;
//   - документация v2RayTun (docs.v2raytun.com) из сборочной среды тоже
//     недоступна. Что v2RayTun показывает announce, следует из реализации в
//     3x-ui: https://github.com/MHSanaei/3x-ui/commit/fd5f591 — те же имена,
//     Announce и Profile-Title с приставкой «base64:», ссылки как есть.
//
// Имена заголовков HTTP регистр не различает; пишем их так же, как 3x-ui,
// чтобы ответ не отличался от привычного на вид.
const (
	HeaderSupport  = "Support-Url"
	HeaderWebPage  = "Profile-Web-Page-Url"
	HeaderAnnounce = "Announce"
	HeaderTitle    = "Profile-Title"
)

const (
	// MaxAnnounce — предел объявления в символах, а не в байтах: продавец
	// пишет по-русски, и двести байт кириллицы — это сто букв.
	//
	// Happ показывает первые двести символов. Предел у нас выше, потому что
	// наш клиент кнопку ещё не рисует, а триста символов — это «технические
	// работы с 2:00 до 4:00, подробности в канале» с запасом на ссылку.
	// Продавцу, которому важен Happ, это сказано в подсказке у поля.
	MaxAnnounce = 300

	// MaxLink — предел ссылки. Ссылка на бота или страницу оплаты — это
	// десятки символов; килобайт — уже не ссылка, а попытка протащить в
	// заголовок что-то другое.
	MaxLink = 1024

	// prefix — приставка, по которой приложения понимают, что значение
	// в base64. Заголовок HTTP — это латиница: кириллица как есть доходит
	// не везде и не всегда целой.
	prefix = "base64:"
)

// Info — поддержка, продление и объявление. Пустое поле — выключено.
type Info struct {
	SupportURL string `json:"support_url"`
	RenewURL   string `json:"renew_url"`
	Announce   string `json:"announce"`
}

// Empty — продавец ничего не задал.
func (i Info) Empty() bool {
	return i.SupportURL == "" && i.RenewURL == "" && i.Announce == ""
}

// CheckLink проверяет ссылку от продавца и возвращает её без пробелов по
// краям. Пусто — законно: значит, кнопки не будет.
//
// Годятся только https:// и tg://. Ссылку приложение отдаёт системе, а та
// откроет что угодно: file:, intent:, javascript: в чужом приложении или
// http:, который подменит любой провайдер по дороге к странице оплаты.
// tg:// — потому что поддержка у продавцов почти всегда в телеграме, а
// ссылка на t.me — это уже https.
func CheckLink(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if len(s) > MaxLink {
		return "", fmt.Errorf("ссылка длиннее %d символов", MaxLink)
	}
	// Только видимая латиница. Пробел, перевод строки или управляющий
	// символ в заголовке ответа — это способ дописать в ответ свой
	// заголовок; а кириллица в адресе у части приложений ломается молча.
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] >= 0x7f {
			return "", errors.New("в ссылке пробел, управляющий символ или не латиница: русский домен впиши в виде xn--…")
		}
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", errors.New("ссылка не разбирается")
	}
	switch u.Scheme {
	case "https":
		if u.Host == "" {
			return "", errors.New("в ссылке нет адреса сайта")
		}
		// https://bank.ru@evil.example ведёт на evil.example, а человек
		// читает «bank.ru». Продавцу такое не нужно, обманщику — нужно.
		if u.User != nil {
			return "", errors.New("в ссылке не должно быть имени пользователя перед @")
		}
	case "tg":
		if u.Host == "" {
			return "", errors.New("ссылка tg:// без действия: нужно вроде tg://resolve?domain=имя")
		}
	default:
		return "", errors.New("ссылка должна начинаться с https:// или tg://")
	}
	return s, nil
}

// CheckAnnounce готовит объявление от продавца: чистит так же, как это
// сделает клиент, и отказывает, если и после этого оно длиннее предела.
//
// Чистим, а не отказываем за перевод строки: текст вставляют из телеграма,
// и спорить с продавцом о невидимом символе бессмысленно — клиент всё равно
// его выбросит. Длина — другое дело: обрезанное посреди слова объявление
// продавец должен увидеть сам, до того как его увидят покупатели.
func CheckAnnounce(raw string) (string, error) {
	s := cleanText(raw)
	if n := utf8.RuneCountInString(s); n > MaxAnnounce {
		return "", fmt.Errorf("объявление длиннее %d символов: %d", MaxAnnounce, n)
	}
	return s, nil
}

// Check проверяет всё, что прислал продавец. Ошибка называет поле: «ссылка
// не разбирается» без имени поля заставляет гадать, какая из двух.
func (i Info) Check() (Info, error) {
	var (
		out Info
		err error
	)
	if out.SupportURL, err = CheckLink(i.SupportURL); err != nil {
		return Info{}, fmt.Errorf("поддержка: %w", err)
	}
	if out.RenewURL, err = CheckLink(i.RenewURL); err != nil {
		return Info{}, fmt.Errorf("продление: %w", err)
	}
	if out.Announce, err = CheckAnnounce(i.Announce); err != nil {
		return Info{}, err
	}
	return out, nil
}

// Clean — то же для клиента, который не спорит, а отбрасывает.
//
// Негодная ссылка выбрасывается целиком: починить чужую ссылку «как
// получится» — значит открыть человеку не то, что задумал продавец.
// Объявление вычищается и, если длинное, обрезается: текст безопасен, а
// первые триста символов полезнее, чем ничего.
func Clean(i Info) Info {
	var out Info
	out.SupportURL, _ = CheckLink(i.SupportURL)
	out.RenewURL, _ = CheckLink(i.RenewURL)
	out.Announce = cleanText(i.Announce)
	if utf8.RuneCountInString(out.Announce) > MaxAnnounce {
		out.Announce = strings.TrimSpace(string([]rune(out.Announce)[:MaxAnnounce-1])) + "…"
	}
	return out
}

// cleanText оставляет от текста одну строку без управляющих символов и без
// символов направления письма.
//
// Управляющие — потому что перевод строки и прочее рвут разметку экрана, а
// в заголовке ответа и вовсе опасны. Символы направления (U+202E и
// родственники) переворачивают текст при показе: «продлить на
// moc.ecived-elif» читается как безобидное имя, а ведёт в другое место.
// Это старый приём подмены, и у объявления продавцу он не нужен.
func cleanText(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r) || bidi(r):
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// bidi — символы, которые меняют направление письма, а сами не видны.
func bidi(r rune) bool {
	switch {
	case r == 0x061C, r == 0x200E, r == 0x200F:
		return true
	case r >= 0x202A && r <= 0x202E:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// Encode заворачивает текст для заголовка: «base64:» и UTF-8 в base64.
// Так кириллица и эмодзи доходят до приложения целыми.
func Encode(text string) string {
	return prefix + base64.StdEncoding.EncodeToString([]byte(text))
}

// decode разворачивает значение заголовка. Без приставки — текст как есть:
// так тоже пишут, и документация Happ это допускает.
//
// base64 бывает и обычный, и для адресов, с добивкой и без: чужие панели
// пишут кто как. Не разобралось — пусто, а не мусор на экране.
func decode(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return v
	}
	body := strings.TrimSpace(v[len(prefix):])
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		if raw, err := enc.DecodeString(body); err == nil {
			return string(raw)
		}
	}
	return ""
}

// FromHeader читает заголовки подписки — нашей или чужой — и сразу чистит.
func FromHeader(h http.Header) Info {
	return Clean(Info{
		SupportURL: h.Get(HeaderSupport),
		RenewURL:   h.Get(HeaderWebPage),
		Announce:   decode(h.Get(HeaderAnnounce)),
	})
}

// WriteHeader ставит заголовки для чужих приложений. Пустые поля не
// пишутся: пустой support-url часть приложений рисует кнопкой в никуда.
//
// title — имя профиля в приложении. Пусто — заголовка нет, и приложение
// возьмёт имя как обычно, из адреса подписки.
func (i Info) WriteHeader(h http.Header, title string) {
	if i.SupportURL != "" {
		h.Set(HeaderSupport, i.SupportURL)
	}
	if i.RenewURL != "" {
		h.Set(HeaderWebPage, i.RenewURL)
	}
	if i.Announce != "" {
		h.Set(HeaderAnnounce, Encode(i.Announce))
	}
	if title = cleanText(title); title != "" {
		h.Set(HeaderTitle, Encode(title))
	}
}
