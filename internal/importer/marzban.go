package importer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/panel"
)

// ReadMarzban читает базу Marzban и его xray_config.json.
//
// Пустой configPath — xray_config.json рядом с базой: так их раскладывает
// установщик Marzban, /var/lib/marzban. Поддержана только SQLite — она стоит
// по умолчанию; база в MySQL пока не читается, и это сказано в ошибке.
func ReadMarzban(dbPath, configPath string, now time.Time) (*Plan, error) {
	if configPath == "" {
		configPath = filepath.Join(filepath.Dir(dbPath), "xray_config.json")
	}
	inbounds, err := readXrayConfig(configPath)
	if err != nil {
		return nil, err
	}

	db, err := openReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	plan := &Plan{Source: "Marzban", SubPath: "sub"}
	for _, in := range inbounds {
		plan.Inbounds = append(plan.Inbounds, in)
	}
	sortInbounds(plan.Inbounds)

	// Ключ, которым Marzban подписывает токены подписки. Без него старые
	// адреса подписки не проверить — переедут только ключи.
	var secret string
	if err := db.QueryRow(`SELECT secret_key FROM jwt ORDER BY id LIMIT 1`).Scan(&secret); err != nil {
		plan.Warnings = append(plan.Warnings,
			"в базе нет ключа подписи (таблица jwt): старые адреса подписки работать не будут, только ссылки")
	}

	excluded, err := marzbanExcluded(db)
	if err != nil {
		return nil, err
	}
	proxies, err := marzbanProxies(db)
	if err != nil {
		return nil, err
	}

	have, err := columns(db, "users")
	if err != nil {
		return nil, err
	}
	if !have["username"] {
		return nil, fmt.Errorf("%s не похожа на базу Marzban: в ней нет таблицы users с username", dbPath)
	}
	rows, err := db.Query(`SELECT id, username, ` + col(have, "status") + `, ` + col(have, "used_traffic") + `, ` +
		col(have, "data_limit") + `, ` + col(have, "expire") + `, ` + col(have, "created_at") + `, ` +
		col(have, "sub_revoked_at") + `, ` + col(have, "on_hold_expire_duration") + `, ` +
		col(have, "data_limit_reset_strategy") + ` FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("чтение покупателей Marzban: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id               int64
			username         string
			status, reset    sql.NullString
			used, limit, exp sql.NullInt64
			hold             sql.NullInt64
			created, revoked any
		)
		if err := rows.Scan(&id, &username, &status, &used, &limit, &exp, &created, &revoked, &hold, &reset); err != nil {
			return nil, fmt.Errorf("чтение покупателя Marzban: %w", err)
		}

		c := Customer{Name: username}
		u := &c.User
		u.ExternalID = "marzban:" + strings.ToLower(username)
		u.Label = username
		u.Enabled = status.String != "disabled"
		u.TrafficLimit = limit.Int64
		u.UsedBefore = used.Int64
		switch {
		case exp.Int64 > 0:
			t := time.Unix(exp.Int64, 0).UTC()
			u.ExpiresAt = &t
		case status.String == "on_hold" && hold.Int64 > 0:
			u.ExpiresAt = timeAfter(now, time.Duration(hold.Int64)*time.Second)
			c.Notes = append(c.Notes, notStartedNote)
		}
		if r := reset.String; r != "" && r != "no_reset" {
			c.Notes = append(c.Notes, "квота сбрасывалась сама ("+resetWords(r)+"); Marvia сама не сбрасывает — продлевай лимитом")
		}

		if t, ok := parseTime(created); ok {
			u.CreatedAt = t
			u.NotBefore = t
		}
		if t, ok := parseTime(revoked); ok && t.After(u.NotBefore) {
			u.NotBefore = t
		}
		if secret != "" {
			u.MarzbanName = username
			u.MarzbanSecret = secret
		}

		marzbanAccess(&c, proxies[id], excluded, inbounds)
		if len(u.VLESS) == 0 && len(u.Trojan) == 0 && secret != "" {
			// Подключиться ему через Marvia нечем, но подписка переезжает:
			// новый UUID придёт к нему с ней сам.
			if fresh, err := panel.NewUUID(); err == nil {
				u.VLESS = []string{fresh}
				c.Notes = append(c.Notes, "выдан новый VLESS: придёт с подпиской, прежних ссылок нода не примет")
			}
		}
		plan.Customers = append(plan.Customers, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	plan.Warnings = append(plan.Warnings,
		"Marzban подбирал формат подписки под приложение (Clash, sing-box); Marvia отдаёт список ссылок — приложения на Clash (Clash Verge, Stash, Mihomo) её не примут",
		"путь подписки взят по умолчанию, /sub/; если в .env Marzban задан XRAY_SUBSCRIPTION_PATH, укажи его в -import-sub-path")
	return plan, nil
}

type marzbanProxy struct {
	id       int64
	kind     string
	uuid     string
	password string
	flow     string
}

// marzbanProxies — наборы доступа по покупателям.
func marzbanProxies(db *sql.DB) (map[int64][]marzbanProxy, error) {
	rows, err := db.Query(`SELECT id, user_id, type, settings FROM proxies`)
	if err != nil {
		return nil, fmt.Errorf("чтение ключей Marzban: %w", err)
	}
	defer rows.Close()

	out := map[int64][]marzbanProxy{}
	for rows.Next() {
		var (
			id, userID int64
			kind       string
			raw        []byte
		)
		if err := rows.Scan(&id, &userID, &kind, &raw); err != nil {
			return nil, err
		}
		var s struct {
			ID       string `json:"id"`
			Password string `json:"password"`
			Flow     string `json:"flow"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("ключ Marzban №%d: %w", id, err)
		}
		// В базе лежит имя перечисления: VLESS, VMess, Trojan, Shadowsocks.
		out[userID] = append(out[userID], marzbanProxy{
			id: id, kind: strings.ToLower(kind), uuid: s.ID, password: s.Password, flow: s.Flow,
		})
	}
	return out, rows.Err()
}

// marzbanExcluded — какие входы у какого набора выключены. Остальные входы
// его протокола действуют: так считает сам Marzban.
func marzbanExcluded(db *sql.DB) (map[int64]map[string]bool, error) {
	out := map[int64]map[string]bool{}
	rows, err := db.Query(`SELECT proxy_id, inbound_tag FROM exclude_inbounds_association`)
	if err != nil {
		// В старых версиях таблицы нет — значит, исключений не было.
		return out, nil
	}
	defer rows.Close()
	for rows.Next() {
		var (
			proxy int64
			tag   string
		)
		if err := rows.Scan(&proxy, &tag); err != nil {
			return nil, err
		}
		if out[proxy] == nil {
			out[proxy] = map[string]bool{}
		}
		out[proxy][tag] = true
	}
	return out, rows.Err()
}

// marzbanAccess раскладывает наборы покупателя: что переедет, что нет.
func marzbanAccess(c *Customer, proxies []marzbanProxy, excluded map[int64]map[string]bool, inbounds map[string]*Inbound) {
	u := &c.User
	sawVision := false
	for _, p := range proxies {
		// Сначала — через какие входы он ходит: это нужно отчёту и для
		// тех протоколов, которые не переезжают.
		for _, in := range inbounds {
			if in.Protocol != p.kind || excluded[p.id][in.Tag] {
				continue
			}
			in.Customers++
			if p.kind == "vless" && p.flow == vision && in.visionApplies() {
				in.Vision++
				sawVision = true
			}
		}

		switch p.kind {
		case "vless":
			id, ok := xrayID(p.uuid)
			if !ok {
				c.Notes = append(c.Notes, "VLESS id «"+p.uuid+"» Xray и сам не принял бы — пропущен")
				continue
			}
			u.VLESS = addUnique(u.VLESS, id)
		case "trojan":
			if p.password != "" {
				u.Trojan = addUnique(u.Trojan, p.password)
			}
		case "vmess":
			c.Notes = append(c.Notes, "VMess не переедет: нода Marvia его не понимает")
		case "shadowsocks":
			c.Notes = append(c.Notes, "Shadowsocks не переедет: нода Marvia его не понимает")
		default:
			c.Notes = append(c.Notes, p.kind+" не переедет")
		}
	}
	if sawVision {
		c.Notes = append(c.Notes, visionNote)
	}
}

// visionNote — что будет с покупателем, у которого ссылки с Vision.
const visionNote = "ссылки с Vision нода Marvia не примет: новые придут с подпиской"

// readXrayConfig читает входы из xray_config.json Marzban.
func readXrayConfig(path string) (map[string]*Inbound, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("конфиг Xray: %w (укажи его в -import-xray)", err)
	}
	var cfg struct {
		Inbounds []struct {
			Tag            string          `json:"tag"`
			Protocol       string          `json:"protocol"`
			Port           json.RawMessage `json:"port"`
			StreamSettings json.RawMessage `json:"streamSettings"`
		} `json:"inbounds"`
	}
	// Marzban читает конфиг через commentjson: комментарии в нём законны.
	if err := json.Unmarshal(stripComments(raw), &cfg); err != nil {
		return nil, fmt.Errorf("конфиг Xray %s: %w", path, err)
	}
	out := map[string]*Inbound{}
	for _, x := range cfg.Inbounds {
		if x.Tag == "" {
			continue
		}
		in, err := newInbound(x.Tag, x.Protocol, portNumber(x.Port), x.StreamSettings)
		if err != nil {
			return nil, err
		}
		out[x.Tag] = in
	}
	return out, nil
}

// portNumber — порт входа Xray: число или строка с числом. Диапазон портов
// даёт ноль: нода Marvia слушает один порт.
func portNumber(raw json.RawMessage) int {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(s))
	}
	return n
}

// stripComments убирает // и /* */ вне строк — то, что принимает commentjson.
func stripComments(src []byte) []byte {
	out := make([]byte, 0, len(src))
	inString, escaped := false, false
	for i := 0; i < len(src); i++ {
		ch := src[i]
		if inString {
			out = append(out, ch)
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch {
		case ch == '"':
			inString = true
			out = append(out, ch)
		case ch == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			out = append(out, '\n')
		case ch == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i++
		case ch == '#':
			// commentjson понимает и «#» до конца строки.
			for i < len(src) && src[i] != '\n' {
				i++
			}
			out = append(out, '\n')
		default:
			out = append(out, ch)
		}
	}
	return out
}

// parseTime читает время из SQLite: драйвер отдаёт его то строкой, то уже
// разобранным — зависит от объявленного типа столбца. Marzban пишет время
// без пояса, в UTC.
func parseTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t.UTC(), !t.IsZero()
	case []byte:
		return parseTime(string(t))
	case string:
		for _, layout := range []string{"2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999", time.RFC3339Nano} {
			if parsed, err := time.ParseInLocation(layout, t, time.UTC); err == nil {
				return parsed.UTC(), true
			}
		}
	case int64:
		if t > 0 {
			return time.Unix(t, 0).UTC(), true
		}
	}
	return time.Time{}, false
}

func resetWords(strategy string) string {
	switch strategy {
	case "day":
		return "каждый день"
	case "week":
		return "каждую неделю"
	case "month":
		return "каждый месяц"
	case "year":
		return "каждый год"
	}
	return strategy
}
