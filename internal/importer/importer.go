// Package importer читает базы Marzban и 3x-ui и готовит переезд покупателей
// в панель Marvia.
//
// Пакет только читает. Чужая база открывается на чтение, а в панель пишет
// marvia-panel -import через panel.Store.ImportUser. Поэтому проверку можно
// гонять сколько угодно раз, а записывать — когда отчёт устроил продавца.
//
// Главное, что здесь решается, — что из прежнего останется у покупателя
// рабочим. Сохраняются UUID VLESS, пароли Trojan, срок, остаток квоты и адрес
// подписки. Не сохраняются VMess и Shadowsocks (нода Marvia их не понимает),
// транспорты gRPC и XHTTP и режим Vision. Ссылки с Vision перестают работать,
// но приложение покупателя само обновит подписку по прежнему адресу и получит
// новые — поэтому адрес подписки важнее отдельных ссылок.
package importer

import (
	"crypto/ecdh"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/users"

	_ "modernc.org/sqlite"
)

// Plan — что и как переедет.
type Plan struct {
	// Source — «Marzban» или «3x-ui».
	Source string

	Customers []Customer
	Inbounds  []*Inbound

	// SubPath — путь, по которому прежняя панель отдавала подписки, без
	// косых: «sub» или случайный у 3x-ui.
	SubPath string

	// SubPort и SubDomain — где жила подписка 3x-ui: у неё отдельный
	// сервер, обычно на порту 2096. Пусто — там же, где панель.
	SubPort   int
	SubDomain string

	// Warnings — то, что касается всех сразу.
	Warnings []string
}

// Customer — покупатель прежней панели.
type Customer struct {
	// Name — как его звали на прежней панели: имя в Marzban, email в 3x-ui.
	Name string

	User panel.ImportedUser

	// Notes — что у него переедет не так, как было. Пусто — всё как было.
	Notes []string
}

// Inbound — вход прежней панели: протокол, порт и маскировка.
type Inbound struct {
	Tag      string
	Protocol string
	Port     int
	Network  string
	Security string
	Reality  *Reality
	WSPath   string

	// Customers — сколько покупателей подключаются через этот вход,
	// Vision — сколько из них со ссылкой flow=xtls-rprx-vision.
	Customers int
	Vision    int
}

// Reality — параметры REALITY прежнего входа. С ними нода Marvia на том же
// сервере принимает прежние ссылки: pbk, sid и sni в них не меняются.
type Reality struct {
	PrivateKey  string
	PublicKey   string
	ShortIDs    []string
	ServerNames []string
	Dest        string
}

// Verdict — как нода Marvia отнесётся к ссылкам входа.
type Verdict int

const (
	// OneCommand — REALITY по TCP: установщик ноды ставит её с прежними
	// ключами, и ссылки покупателей продолжают работать.
	OneCommand Verdict = iota
	// Manual — TLS или WebSocket: нода так умеет, но установщик ставит
	// только REALITY, и ноду придётся настроить руками.
	Manual
	// Unsupported — нода Marvia такие ссылки не примет. Покупатель получит
	// новые через подписку.
	Unsupported
)

// Verdict говорит, примет ли нода Marvia ссылки этого входа, и почему нет.
func (in *Inbound) Verdict() (Verdict, string) {
	switch in.Protocol {
	case "vless", "trojan":
	case "vmess":
		return Unsupported, "VMess нода Marvia не понимает"
	case "shadowsocks":
		return Unsupported, "Shadowsocks нода Marvia не понимает"
	default:
		return Unsupported, in.Protocol + " нода Marvia не понимает"
	}

	switch in.Network {
	case "tcp", "raw", "":
		switch in.Security {
		case "reality":
			if in.Reality == nil || in.Reality.PrivateKey == "" {
				return Unsupported, "в настройках REALITY нет приватного ключа"
			}
			if _, _, err := net.SplitHostPort(in.Reality.Dest); err != nil || strings.HasPrefix(in.Reality.Dest, "127.") {
				return Manual, "сайт прикрытия «" + in.Reality.Dest + "» — не адрес чужого сайта, установщик так не ставит"
			}
			return OneCommand, ""
		case "tls":
			return Manual, "TLS со своим сертификатом: нода умеет (-tls-cert), установщик ставит только REALITY"
		default:
			return Unsupported, "без TLS снаружи нода Marvia работает только для отладки"
		}
	case "ws":
		return Manual, "WebSocket: нода умеет (-ws-path), установщик ставит только REALITY"
	default:
		return Unsupported, "транспорт " + in.Network + " нода Marvia не поддерживает"
	}
}

// Title — вход словами: «VLESS · TCP · REALITY · порт 443».
func (in *Inbound) Title() string {
	parts := []string{strings.ToUpper(in.Protocol)}
	if in.Protocol == "vmess" {
		parts[0] = "VMess"
	}
	if in.Protocol == "shadowsocks" {
		parts[0] = "Shadowsocks"
	}
	if in.Protocol == "trojan" {
		parts[0] = "Trojan"
	}
	network := in.Network
	if network == "" || network == "raw" {
		network = "tcp"
	}
	parts = append(parts, strings.ToUpper(network))
	if in.Security != "" && in.Security != "none" {
		parts = append(parts, strings.ToUpper(in.Security))
	}
	parts = append(parts, "порт "+strconv.Itoa(in.Port))
	return strings.Join(parts, " · ")
}

// NodeEnv — переменные для установщика ноды, с которыми она примет прежние
// ссылки. Только для OneCommand.
func (in *Inbound) NodeEnv() string {
	r := in.Reality
	env := []string{
		"PORT=" + strconv.Itoa(in.Port),
		"DEST=" + r.Dest,
	}
	if len(r.ServerNames) > 0 {
		env = append(env, "SNI="+strings.Join(r.ServerNames, ","))
	}
	if len(r.ShortIDs) > 0 {
		env = append(env, "SHORT_IDS="+strings.Join(r.ShortIDs, ","))
	}
	env = append(env, "REALITY_KEY="+r.PrivateKey, "REALITY_PUB="+r.PublicKey)
	return strings.Join(env, " ")
}

// vision — режим XTLS Vision. Нода Marvia его не понимает: внутри иной
// формат кадров, и разговор с таким клиентом просто не сойдётся.
const vision = "xtls-rprx-vision"

// visionApplies — действует ли flow на этом входе. Xray включает Vision только
// поверх TCP с TLS или REALITY; на WebSocket поле flow игнорируется.
func (in *Inbound) visionApplies() bool {
	return (in.Network == "tcp" || in.Network == "raw" || in.Network == "") &&
		(in.Security == "reality" || in.Security == "tls")
}

// stream — streamSettings инбаунда Xray. Формат один у Marzban (в
// xray_config.json) и у 3x-ui (в столбце stream_settings).
type stream struct {
	Network  string `json:"network"`
	Security string `json:"security"`
	Reality  *struct {
		Dest        json.RawMessage `json:"dest"`
		Target      json.RawMessage `json:"target"`
		ServerNames []string        `json:"serverNames"`
		PrivateKey  string          `json:"privateKey"`
		ShortIDs    []string        `json:"shortIds"`
		Settings    *struct {
			PublicKey string `json:"publicKey"`
		} `json:"settings"`
	} `json:"realitySettings"`
	WS *struct {
		Path string `json:"path"`
	} `json:"wsSettings"`
}

// newInbound собирает вход из протокола, порта и streamSettings.
func newInbound(tag, protocol string, port int, raw []byte) (*Inbound, error) {
	in := &Inbound{Tag: tag, Protocol: strings.ToLower(protocol), Port: port}
	if len(raw) == 0 {
		return in, nil
	}
	var s stream
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("настройки входа %s: %w", tag, err)
	}
	in.Network = strings.ToLower(s.Network)
	in.Security = strings.ToLower(s.Security)
	if s.WS != nil {
		in.WSPath = s.WS.Path
	}
	if s.Reality != nil && in.Security == "reality" {
		r := &Reality{
			PrivateKey:  s.Reality.PrivateKey,
			ServerNames: s.Reality.ServerNames,
			Dest:        destString(s.Reality.Target),
		}
		if r.Dest == "" {
			r.Dest = destString(s.Reality.Dest)
		}
		// Пустой shortId в Xray — восемь нулевых байт, и клиент без sid
		// присылает именно их. У ноды Marvia пустое значение из списка
		// выпадает, поэтому пишем те же нули явно.
		for _, id := range s.Reality.ShortIDs {
			if id == "" {
				id = "00"
			}
			r.ShortIDs = append(r.ShortIDs, id)
		}
		if r.PrivateKey != "" {
			pub, err := realityPublicKey(r.PrivateKey)
			if err != nil {
				return nil, fmt.Errorf("ключ REALITY входа %s: %w", tag, err)
			}
			r.PublicKey = pub
			// 3x-ui хранит публичный ключ рядом. Расхождение значит, что
			// в базе что-то перепутано, и молча выбрать одно из двух нельзя:
			// не тот pbk — и не сойдётся ни одна ссылка.
			if s.Reality.Settings != nil && s.Reality.Settings.PublicKey != "" && s.Reality.Settings.PublicKey != pub {
				return nil, fmt.Errorf("вход %s: публичный ключ REALITY не соответствует приватному", tag)
			}
		}
		in.Reality = r
	}
	return in, nil
}

// destString разбирает dest/target REALITY: строка «host:port» или число —
// локальный порт.
func destString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return "127.0.0.1:" + strconv.Itoa(n)
	}
	return ""
}

// realityPublicKey выводит публичный ключ REALITY из приватного. X25519 из
// стандартной библиотеки; кодировка — base64url без «=», как у Xray.
func realityPublicKey(private string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(private, "="))
	if err != nil {
		return "", fmt.Errorf("не base64url: %w", err)
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

// xrayID приводит id клиента VLESS к UUID так же, как Xray.
//
// Xray принимает в id не только UUID, но и любую строку до 30 символов: из
// неё он делает UUID пятой версии через SHA-1. Покупатель с id «alice»
// присылает на провод именно этот UUID, и нода Marvia, зная его, примет
// прежнюю ссылку без изменений. Повторяет common/uuid.ParseString Xray; тест
// сверяет с ним напрямую.
func xrayID(id string) (string, bool) {
	l := len(id)
	if l >= 32 && l <= 36 {
		raw, err := users.ParseUUID(id)
		if err != nil {
			return "", false
		}
		return users.FormatUUID(raw), true
	}
	if l == 0 || l > 30 {
		return "", false
	}
	h := sha1.New()
	h.Write(make([]byte, 16))
	h.Write([]byte(id))
	u := h.Sum(nil)[:16]
	u[6] = (u[6] & 0x0f) | (5 << 4)
	u[8] = u[8]&(0xff>>2) | (0x02 << 6)
	return users.FormatUUID(u), true
}

// openReadOnly открывает чужую базу SQLite только на чтение.
//
// Пишущее открытие базы, с которой ещё работает прежняя панель, — способ
// испортить её на ходу. query_only страхует от ошибки в нашем же коде.
func openReadOnly(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("база %s не открывается: %w", path, err)
	}
	return db, nil
}

// columns — какие столбцы есть в таблице. Схемы прежних панелей менялись от
// версии к версии, и запрос со столбцом, которого ещё нет, упал бы целиком.
func columns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// col возвращает имя столбца или NULL, если его нет в этой версии схемы.
func col(have map[string]bool, name string) string {
	if have[name] {
		return name
	}
	return "NULL"
}

func addUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// sortInbounds — входы по порту: так их проще сверить с конфигом.
func sortInbounds(list []*Inbound) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Port < list[j].Port })
}

// notStartedNote — у покупателя срок ещё не начался: ему дали «месяц с
// первого подключения», а он не подключался. У Marvia такого режима нет,
// поэтому отсчёт начинается с переезда — заметкой в отчёте, а не молча.
const notStartedNote = "срок считался с первого подключения, а он ещё не подключался: отсчёт начат с переезда"

func timeAfter(now time.Time, d time.Duration) *time.Time {
	t := now.Add(d)
	return &t
}
