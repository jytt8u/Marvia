package importer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/panel"
)

// ReadXUI читает базу 3x-ui, обычно /etc/x-ui/x-ui.db.
//
// Покупатель в 3x-ui — не одна запись. Каждому входу продавец заводит
// своего клиента с уникальным email, а одним человеком их делает общий
// subId: по нему собирается подписка. Поэтому клиенты группируются по subId,
// а без него — каждый сам по себе, по email.
//
// Клиентов читаем из JSON входа (settings.clients): его пишут и ветка 2.x,
// и 3.x, хотя у 3.x есть отдельная таблица. Расход — из client_traffics.
func ReadXUI(dbPath string, now time.Time) (*Plan, error) {
	db, err := openReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	have, err := columns(db, "inbounds")
	if err != nil {
		return nil, err
	}
	if !have["protocol"] || !have["settings"] {
		return nil, fmt.Errorf("%s не похожа на базу 3x-ui: в ней нет таблицы inbounds", dbPath)
	}

	plan := &Plan{Source: "3x-ui", SubPath: "sub", SubPort: 2096}
	xuiSettings(db, plan)

	traffic, err := xuiTraffic(db)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`SELECT id, ` + col(have, "remark") + `, ` + col(have, "enable") + `, port, protocol, settings, ` +
		col(have, "stream_settings") + `, ` + col(have, "tag") + `, ` + col(have, "total") + `, ` +
		col(have, "expiry_time") + ` FROM inbounds ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("чтение входов 3x-ui: %w", err)
	}
	defer rows.Close()

	groups := map[string]*xuiGroup{}
	var order []string
	for rows.Next() {
		var (
			id                    int64
			remark, tag           sql.NullString
			enable                sql.NullBool
			port                  int
			protocol              string
			settings, stream      []byte
			inboundTotal, inbound sql.NullInt64
		)
		if err := rows.Scan(&id, &remark, &enable, &port, &protocol, &settings, &stream, &tag, &inboundTotal, &inbound); err != nil {
			return nil, fmt.Errorf("чтение входа 3x-ui: %w", err)
		}
		name := tag.String
		if name == "" {
			name = "inbound-" + strconv.FormatInt(id, 10)
		}
		if enable.Valid && !enable.Bool {
			plan.Warnings = append(plan.Warnings, "вход "+name+" выключен: его клиенты не переносятся, как не попадали и в подписку")
			continue
		}
		in, err := newInbound(name, protocol, port, stream)
		if err != nil {
			return nil, err
		}
		plan.Inbounds = append(plan.Inbounds, in)
		if inboundTotal.Int64 > 0 || inbound.Int64 > 0 {
			plan.Warnings = append(plan.Warnings, "у входа "+name+" свой общий лимит или срок: у Marvia такого нет, переносятся лимиты каждого покупателя")
		}

		var s struct {
			Clients []xuiClient `json:"clients"`
		}
		if len(settings) > 0 {
			if err := json.Unmarshal(settings, &s); err != nil {
				return nil, fmt.Errorf("клиенты входа %s: %w", name, err)
			}
		}
		for _, cl := range s.Clients {
			key := "email:" + strings.ToLower(cl.Email)
			if cl.SubID != "" {
				key = cl.SubID
			}
			g := groups[key]
			if g == nil {
				g = &xuiGroup{key: key}
				groups[key] = g
				order = append(order, key)
			}
			g.entries = append(g.entries, xuiEntry{client: cl, inbound: in, traffic: traffic[strings.ToLower(cl.Email)]})
			in.Customers++
			if in.Protocol == "vless" && cl.Flow == vision && in.visionApplies() {
				in.Vision++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortInbounds(plan.Inbounds)

	short := 0
	for _, key := range order {
		c := groups[key].customer(now)
		if len(c.User.SubTokens) > 0 && len(c.User.SubTokens[0]) < shortSubID {
			short++
		}
		plan.Customers = append(plan.Customers, c)
	}
	// subId продавец мог вписать руками: «user1», номер телефона. На прежней
	// панели такой адрес был так же уязвим, но переезд — повод об этом
	// сказать: по подобранному адресу отдаются UUID покупателя.
	if short > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"у %s адрес подписки короче %d символов — его можно подобрать перебором; таким лучше сменить ссылку в панели после переезда",
			customersOf(short), shortSubID))
	}
	return plan, nil
}

// shortSubID — subId короче этого 3x-ui сам не выдаёт: его случайные — 16
// символов. Короче — значит, вписан руками.
const shortSubID = 10

// xuiClient — клиент из settings.clients. Старые версии писали числа
// строками, поэтому числа — flexInt.
type xuiClient struct {
	ID         string  `json:"id"`
	Password   string  `json:"password"`
	Flow       string  `json:"flow"`
	Email      string  `json:"email"`
	SubID      string  `json:"subId"`
	TotalGB    flexInt `json:"totalGB"`
	ExpiryTime flexInt `json:"expiryTime"`
	Enable     *bool   `json:"enable"`
	LimitIP    flexInt `json:"limitIp"`
	Reset      flexInt `json:"reset"`
	CreatedAt  flexInt `json:"created_at"`
}

type xuiTrafficRow struct {
	used    int64
	enabled bool
	found   bool
}

type xuiEntry struct {
	client  xuiClient
	inbound *Inbound
	traffic xuiTrafficRow
}

type xuiGroup struct {
	key     string
	entries []xuiEntry
}

// customer сводит клиентов одного человека в одного покупателя Marvia.
//
// Квота у Marvia одна на человека, а у 3x-ui — своя у каждого клиента.
// Поэтому лимиты складываются (два входа по 50 ГБ — это 100 ГБ на человека),
// безлимит хоть на одном делает безлимитным всё, срок берётся поздний.
func (g *xuiGroup) customer(now time.Time) Customer {
	first := g.entries[0].client
	c := Customer{Name: first.Email}
	u := &c.User
	u.ExternalID = "3x-ui:" + g.key
	u.Label = first.Email
	if first.SubID != "" {
		u.SubTokens = []string{first.SubID}
	}

	unlimited, noExpiry, notStarted, sawVision := false, false, false, false
	var latest time.Time
	for _, e := range g.entries {
		cl := e.client
		enabled := cl.Enable == nil || *cl.Enable
		if e.traffic.found && !e.traffic.enabled {
			enabled = false
		}
		if enabled {
			u.Enabled = true
		}

		// totalGB — байты, что бы ни говорило имя поля.
		if cl.TotalGB <= 0 {
			unlimited = true
		} else {
			u.TrafficLimit += int64(cl.TotalGB)
		}
		u.UsedBefore += e.traffic.used

		switch ms := int64(cl.ExpiryTime); {
		case ms == 0:
			noExpiry = true
		case ms < 0:
			// Отрицательный срок — длительность от первого подключения.
			notStarted = true
			if t := now.Add(time.Duration(-ms) * time.Millisecond); t.After(latest) {
				latest = t
			}
		default:
			if t := time.UnixMilli(ms).UTC(); t.After(latest) {
				latest = t
			}
		}
		if int(cl.LimitIP) > u.MaxIPs {
			u.MaxIPs = int(cl.LimitIP)
		}
		if cl.CreatedAt > 0 {
			if t := time.UnixMilli(int64(cl.CreatedAt)).UTC(); u.CreatedAt.IsZero() || t.Before(u.CreatedAt) {
				u.CreatedAt = t
			}
		}
		if cl.Reset > 0 {
			c.Notes = addUnique(c.Notes, "срок продлевался сам каждые "+strconv.Itoa(int(cl.Reset))+" дн.; Marvia сама не продлевает")
		}

		switch e.inbound.Protocol {
		case "vless":
			id, ok := xrayID(cl.ID)
			if !ok {
				c.Notes = append(c.Notes, "VLESS id «"+cl.ID+"» Xray и сам не принял бы — пропущен")
				continue
			}
			u.VLESS = addUnique(u.VLESS, id)
			if cl.Flow == vision && e.inbound.visionApplies() {
				sawVision = true
			}
		case "trojan":
			if cl.Password != "" {
				u.Trojan = addUnique(u.Trojan, cl.Password)
			}
		case "vmess":
			c.Notes = addUnique(c.Notes, "VMess не переедет: нода Marvia его не понимает")
		case "shadowsocks":
			c.Notes = addUnique(c.Notes, "Shadowsocks не переедет: нода Marvia его не понимает")
		default:
			c.Notes = addUnique(c.Notes, e.inbound.Protocol+" не переедет")
		}
	}
	if unlimited {
		u.TrafficLimit = 0
	}
	if !noExpiry && !latest.IsZero() {
		u.ExpiresAt = &latest
	}
	if notStarted && !noExpiry {
		c.Notes = append(c.Notes, notStartedNote)
	}
	if sawVision {
		c.Notes = append(c.Notes, visionNote)
	}
	if len(u.VLESS) == 0 && len(u.Trojan) == 0 && len(u.SubTokens) > 0 {
		if fresh, err := panel.NewUUID(); err == nil {
			u.VLESS = []string{fresh}
			c.Notes = append(c.Notes, "выдан новый VLESS: придёт с подпиской, прежних ссылок нода не примет")
		}
	}
	return c
}

// xuiTraffic — расход и состояние клиентов по email. Email в 3x-ui уникален
// без учёта регистра.
func xuiTraffic(db *sql.DB) (map[string]xuiTrafficRow, error) {
	out := map[string]xuiTrafficRow{}
	rows, err := db.Query(`SELECT email, up, down, enable FROM client_traffics`)
	if err != nil {
		// Без таблицы расхода переносить можно — просто с нулевым расходом.
		return out, nil
	}
	defer rows.Close()
	for rows.Next() {
		var (
			email    string
			up, down sql.NullInt64
			enable   sql.NullBool
		)
		if err := rows.Scan(&email, &up, &down, &enable); err != nil {
			return nil, err
		}
		out[strings.ToLower(email)] = xuiTrafficRow{
			used: up.Int64 + down.Int64, enabled: !enable.Valid || enable.Bool, found: true,
		}
	}
	return out, rows.Err()
}

// xuiSettings — где жила подписка: путь, порт и домен из таблицы settings.
// Путь в 3x-ui после сброса настроек случайный, поэтому его надо прочесть,
// а не взять по умолчанию.
func xuiSettings(db *sql.DB, plan *Plan) {
	rows, err := db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			values[k] = v
		}
	}
	if p := strings.Trim(values["subPath"], "/"); p != "" && !strings.Contains(p, "/") {
		plan.SubPath = p
	}
	if n, err := strconv.Atoi(values["subPort"]); err == nil && n > 0 {
		plan.SubPort = n
	}
	plan.SubDomain = values["subDomain"]
	if values["subEnable"] == "false" {
		plan.Warnings = append(plan.Warnings, "подписка в 3x-ui была выключена: покупатели подключались ссылками, и адреса подписки им не помогут")
	}
	if uri := values["subURI"]; uri != "" {
		plan.Warnings = append(plan.Warnings, "подписка 3x-ui отдавалась через "+uri+": старые адреса заработают, когда этот адрес будет вести на панель Marvia")
	}
}

// flexInt — число, которое старые версии 3x-ui писали строкой.
type flexInt int64

func (f *flexInt) UnmarshalJSON(raw []byte) error {
	text := strings.TrimSpace(string(raw))
	if text == "null" {
		*f = 0
		return nil
	}
	if unquoted, err := strconv.Unquote(text); err == nil {
		text = strings.TrimSpace(unquoted)
	}
	if text == "" {
		*f = 0
		return nil
	}
	if v, err := strconv.ParseInt(text, 10, 64); err == nil {
		*f = flexInt(v)
		return nil
	}
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return fmt.Errorf("не число: %s", raw)
	}
	*f = flexInt(v)
	return nil
}
