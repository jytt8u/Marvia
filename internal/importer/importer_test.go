package importer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	xrayuuid "github.com/xtls/xray-core/common/uuid"

	"github.com/jytt8u/marvia/internal/panel"
)

// Ключ REALITY из RFC 7748, раздел 6.1: приватный ключ Алисы и её публичный.
// Эталон снаружи, а не посчитанный тем же кодом, который проверяем.
const (
	rfcPrivate = "dwdtCnMYpX08FsFyUbJmRd9ML4frwJkqsXf7pR25LCo"
	rfcPublic  = "hSDwCYkwp1R0i33ctD73Wg2_Og0mOBr066SpjqqbTmo"
)

const gb = int64(1) << 30

var now = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func fixture(t *testing.T, name string, statements ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range statements {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("образец базы: %v\n%s", err, s)
		}
	}
	return path
}

// marzbanFixture — база и конфиг в том виде, в каком их оставляет Marzban
// 0.8: схема users и proxies из app/db/models.py, тип набора — имя
// перечисления, время без пояса.
func marzbanFixture(t *testing.T) string {
	t.Helper()
	db := fixture(t, "db.sqlite3",
		`CREATE TABLE users (id INTEGER PRIMARY KEY, username VARCHAR(34) COLLATE NOCASE, status VARCHAR(8) NOT NULL,
			used_traffic BIGINT, data_limit BIGINT, data_limit_reset_strategy VARCHAR(7) NOT NULL, expire INTEGER,
			admin_id INTEGER, sub_revoked_at DATETIME, created_at DATETIME, note VARCHAR(500),
			on_hold_expire_duration BIGINT, on_hold_timeout DATETIME)`,
		`CREATE TABLE proxies (id INTEGER PRIMARY KEY, user_id INTEGER, type VARCHAR(11) NOT NULL, settings JSON NOT NULL)`,
		`CREATE TABLE exclude_inbounds_association (proxy_id INTEGER, inbound_tag VARCHAR(256))`,
		`CREATE TABLE jwt (id INTEGER PRIMARY KEY, secret_key VARCHAR(64) NOT NULL)`,
		`INSERT INTO jwt VALUES (1, 'f1c7a3e2d4b5968778695a4b3c2d1e0ff1c7a3e2d4b5968778695a4b3c2d1e0f')`,

		`INSERT INTO users (id, username, status, used_traffic, data_limit, data_limit_reset_strategy, expire, created_at, sub_revoked_at)
			VALUES (1, 'Alice', 'active', 10737418240, 53687091200, 'month', 1793000000, '2025-01-02 03:04:05.123456', '2025-06-01 00:00:00')`,
		`INSERT INTO proxies VALUES (1, 1, 'VLESS', '{"id": "6F1A6C2E-3D4B-4E5F-8A9B-0C1D2E3F4A5B", "flow": "xtls-rprx-vision"}')`,
		`INSERT INTO proxies VALUES (2, 1, 'Trojan', '{"password": "alice-trojan", "flow": ""}')`,

		`INSERT INTO users (id, username, status, used_traffic, data_limit, data_limit_reset_strategy, expire, created_at, on_hold_expire_duration)
			VALUES (2, 'bob', 'on_hold', 0, NULL, 'no_reset', NULL, '2025-03-01 00:00:00', 2592000)`,
		`INSERT INTO proxies VALUES (3, 2, 'VLESS', '{"id": "bob", "flow": ""}')`,
		`INSERT INTO proxies VALUES (4, 2, 'VMess', '{"id": "11111111-2222-4333-8444-555555555555"}')`,

		`INSERT INTO users (id, username, status, used_traffic, data_limit, data_limit_reset_strategy, expire, created_at)
			VALUES (3, 'carol', 'disabled', 0, 0, 'no_reset', 0, '2025-04-01 00:00:00')`,
		`INSERT INTO proxies VALUES (5, 3, 'VMess', '{"id": "22222222-2222-4333-8444-555555555555"}')`,

		`INSERT INTO users (id, username, status, used_traffic, data_limit, data_limit_reset_strategy, expire, created_at)
			VALUES (4, 'dave', 'active', 0, 0, 'no_reset', 0, '2025-05-01 00:00:00')`,
		`INSERT INTO proxies VALUES (6, 4, 'VLESS', '{"id": "33333333-2222-4333-8444-555555555555", "flow": "xtls-rprx-vision"}')`,
		`INSERT INTO exclude_inbounds_association VALUES (6, 'VLESS TCP REALITY')`,
	)

	// Marzban читает конфиг через commentjson — комментарии в нём бывают.
	config := `{
  // входы
  "inbounds": [
    {"tag": "VLESS TCP REALITY", "protocol": "vless", "port": 443, "settings": {"clients": []},
     "streamSettings": {"network": "tcp", "security": "reality", "realitySettings": {
       "dest": "www.google.com:443", "serverNames": ["www.google.com", "google.com"],
       "privateKey": "` + rfcPrivate + `", "shortIds": ["", "a1b2"]}}},
    {"tag": "VLESS WS", "protocol": "vless", "port": "8080", "streamSettings": {"network": "ws", "wsSettings": {"path": "/ws // не комментарий"}}},
    # комментарий в стиле питона
    {"tag": "VMess TCP", "protocol": "vmess", "port": 2053, "streamSettings": {"network": "tcp"}},
    /* gRPC */
    {"tag": "Trojan gRPC", "protocol": "trojan", "port": 2083, "streamSettings": {"network": "grpc", "security": "tls"}}
  ]
}`
	if err := os.WriteFile(filepath.Join(filepath.Dir(db), "xray_config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return db
}

func byName(t *testing.T, plan *Plan, name string) Customer {
	t.Helper()
	for _, c := range plan.Customers {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("покупателя %s нет в плане", name)
	return Customer{}
}

func byTag(t *testing.T, plan *Plan, tag string) *Inbound {
	t.Helper()
	for _, in := range plan.Inbounds {
		if in.Tag == tag {
			return in
		}
	}
	t.Fatalf("входа %s нет в плане", tag)
	return nil
}

func hasNote(c Customer, part string) bool {
	for _, n := range c.Notes {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}

func TestMarzbanCustomerKeepsKeysQuotaAndSubscriptionRight(t *testing.T) {
	plan, err := ReadMarzban(marzbanFixture(t), "", now)
	if err != nil {
		t.Fatal(err)
	}
	alice := byName(t, plan, "Alice").User

	if alice.ExternalID != "marzban:alice" {
		t.Errorf("ключ покупателя %q: Marzban сравнивает имена без регистра, и мы так же", alice.ExternalID)
	}
	if len(alice.VLESS) != 1 || alice.VLESS[0] != "6f1a6c2e-3d4b-4e5f-8a9b-0c1d2e3f4a5b" {
		t.Errorf("VLESS %q", alice.VLESS)
	}
	if len(alice.Trojan) != 1 || alice.Trojan[0] != "alice-trojan" {
		t.Errorf("Trojan %q", alice.Trojan)
	}
	if alice.TrafficLimit != 50*gb || alice.UsedBefore != 10*gb {
		t.Errorf("квота %d, расход %d: ожидались 50 и 10 ГБ", alice.TrafficLimit, alice.UsedBefore)
	}
	if alice.ExpiresAt == nil || !alice.ExpiresAt.Equal(time.Unix(1793000000, 0)) {
		t.Errorf("срок %v", alice.ExpiresAt)
	}
	if !alice.Enabled {
		t.Error("активный покупатель перенесён выключенным")
	}
	// Подписку отзывали 1 июня: токены до этого Marzban не принимал.
	if want := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC); !alice.NotBefore.Equal(want) {
		t.Errorf("токены действительны с %v, а подписку отозвали %v", alice.NotBefore, want)
	}
	if alice.MarzbanSecret == "" || alice.MarzbanName != "Alice" {
		t.Error("право проверять старые токены подписки не перенесено")
	}
	c := byName(t, plan, "Alice")
	if !hasNote(c, "Vision") || !hasNote(c, "каждый месяц") {
		t.Errorf("заметки %q: должны сказать про Vision и про месячный сброс", c.Notes)
	}
}

func TestStringIDBecomesTheUUIDXrayPutsOnTheWire(t *testing.T) {
	for _, id := range []string{"bob", "a", strings.Repeat("y", 30), "6F1A6C2E3D4B4E5F8A9B0C1D2E3F4A5B"} {
		want, err := xrayuuid.ParseString(id)
		if err != nil {
			t.Fatalf("сам Xray не принял %q: %v", id, err)
		}
		got, ok := xrayID(id)
		if !ok || got != want.String() {
			t.Errorf("id %q: у нас %q, у Xray %q — прежняя ссылка не сошлась бы", id, got, want.String())
		}
	}
	for _, id := range []string{"", strings.Repeat("x", 31)} {
		if _, ok := xrayID(id); ok {
			t.Errorf("id %q принят, хотя Xray его отвергает", id)
		}
	}
}

func TestOnHoldCustomerStartsCountingFromTheMove(t *testing.T) {
	plan, err := ReadMarzban(marzbanFixture(t), "", now)
	if err != nil {
		t.Fatal(err)
	}
	bob := byName(t, plan, "bob")
	if bob.User.ExpiresAt == nil || !bob.User.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Errorf("срок %v: месяц «с первого подключения» должен начаться с переезда", bob.User.ExpiresAt)
	}
	if !hasNote(bob, "с первого подключения") || !hasNote(bob, "VMess") {
		t.Errorf("заметки %q", bob.Notes)
	}
}

func TestCustomerWithOnlyVMessGetsFreshVLESSThroughSubscription(t *testing.T) {
	plan, err := ReadMarzban(marzbanFixture(t), "", now)
	if err != nil {
		t.Fatal(err)
	}
	carol := byName(t, plan, "carol")
	if len(carol.User.VLESS) != 1 || carol.User.Enabled {
		t.Errorf("carol: VLESS %q, включена %v — ожидался новый VLESS и прежнее «выключена»", carol.User.VLESS, carol.User.Enabled)
	}
	if !hasNote(carol, "новый VLESS") {
		t.Errorf("заметки %q: продавец должен знать, что ключ новый", carol.Notes)
	}
}

func TestInboundVerdictsSayWhatTheNodeWillAccept(t *testing.T) {
	plan, err := ReadMarzban(marzbanFixture(t), "", now)
	if err != nil {
		t.Fatal(err)
	}

	reality := byTag(t, plan, "VLESS TCP REALITY")
	if v, why := reality.Verdict(); v != OneCommand {
		t.Fatalf("REALITY по TCP: %v (%s), ожидалась установка одной командой", v, why)
	}
	env := reality.NodeEnv()
	for _, want := range []string{"PORT=443", "DEST=www.google.com:443", "SNI=www.google.com,google.com",
		"SHORT_IDS=00,a1b2", "REALITY_KEY=" + rfcPrivate, "REALITY_PUB=" + rfcPublic} {
		if !strings.Contains(env, want) {
			t.Errorf("строка для установщика %q: нет %s", env, want)
		}
	}
	// alice и bob ходят через него, dave этот вход исключён.
	if reality.Customers != 2 || reality.Vision != 1 {
		t.Errorf("через REALITY: %d покупателей, с Vision %d; ожидались 2 и 1", reality.Customers, reality.Vision)
	}

	ws := byTag(t, plan, "VLESS WS")
	if v, _ := ws.Verdict(); v != Manual || ws.Port != 8080 || ws.WSPath != "/ws // не комментарий" {
		t.Errorf("WebSocket: %v, порт %d, путь %q", v, ws.Port, ws.WSPath)
	}
	// Vision поверх WebSocket Xray игнорирует — ссылка dave на нём рабочая.
	if ws.Vision != 0 {
		t.Errorf("на WebSocket насчитан Vision: %d", ws.Vision)
	}
	for _, tag := range []string{"VMess TCP", "Trojan gRPC"} {
		if v, _ := byTag(t, plan, tag).Verdict(); v != Unsupported {
			t.Errorf("%s: %v, а нода такое не примет", tag, v)
		}
	}
}

// xuiFixture — база 3x-ui 2.x: клиенты в JSON входа, расход в
// client_traffics, числа местами строками, как писали старые версии.
func xuiFixture(t *testing.T) string {
	t.Helper()
	reality := `{"network":"tcp","security":"reality","realitySettings":{"target":"www.microsoft.com:443",` +
		`"serverNames":["www.microsoft.com"],"privateKey":"` + rfcPrivate + `","shortIds":["c0ffee"],` +
		`"settings":{"publicKey":"` + rfcPublic + `","fingerprint":"chrome"}}}`
	return fixture(t, "x-ui.db",
		`CREATE TABLE inbounds (id INTEGER PRIMARY KEY, user_id INTEGER, up INTEGER, down INTEGER, total INTEGER,
			remark TEXT, enable NUMERIC, expiry_time INTEGER, listen TEXT, port INTEGER, protocol TEXT,
			settings TEXT, stream_settings TEXT, tag TEXT, sniffing TEXT)`,
		`CREATE TABLE client_traffics (id INTEGER PRIMARY KEY, inbound_id INTEGER, enable NUMERIC, email TEXT UNIQUE,
			up INTEGER, down INTEGER, expiry_time INTEGER, total INTEGER, reset INTEGER)`,
		`CREATE TABLE settings (id INTEGER PRIMARY KEY, key TEXT, value TEXT)`,
		`INSERT INTO settings (key, value) VALUES ('subPath', '/xYz123/'), ('subPort', '2097'), ('subDomain', 'sub.old.example')`,

		`INSERT INTO inbounds (id, total, enable, expiry_time, port, protocol, settings, stream_settings, tag) VALUES (1, 0, 1, 0, 443, 'vless',
			'{"clients":[{"id":"6f1a6c2e-3d4b-4e5f-8a9b-0c1d2e3f4a5b","flow":"xtls-rprx-vision","email":"anna-443","subId":"s1",
			  "totalGB":53687091200,"expiryTime":1790000000000,"enable":true,"limitIp":2},
			 {"id":"77777777-2222-4333-8444-555555555555","flow":"","email":"Boris","subId":"",
			  "totalGB":"0","expiryTime":-2592000000,"enable":true,"limitIp":"0"}]}',
			'`+reality+`', 'inbound-443')`,
		`INSERT INTO inbounds (id, total, enable, expiry_time, port, protocol, settings, stream_settings, tag) VALUES (2, 0, 1, 0, 8443, 'trojan',
			'{"clients":[{"password":"anna-trojan","email":"anna-8443","subId":"s1","totalGB":53687091200,"expiryTime":1795000000000,"enable":true}]}',
			'{"network":"ws","security":"tls","wsSettings":{"path":"/t"}}', 'inbound-8443')`,
		`INSERT INTO inbounds (id, total, enable, expiry_time, port, protocol, settings, stream_settings, tag) VALUES (3, 0, 0, 0, 2053, 'vmess',
			'{"clients":[{"id":"88888888-2222-4333-8444-555555555555","email":"ghost","subId":"s9"}]}', '{"network":"tcp"}', 'inbound-2053')`,

		`INSERT INTO client_traffics (inbound_id, enable, email, up, down) VALUES (1, 1, 'anna-443', 1073741824, 4294967296)`,
		`INSERT INTO client_traffics (inbound_id, enable, email, up, down) VALUES (2, 1, 'anna-8443', 0, 5368709120)`,
		`INSERT INTO client_traffics (inbound_id, enable, email, up, down) VALUES (1, 0, 'boris', 0, 0)`,
	)
}

func TestXUIClientsSharingSubIDBecomeOneCustomer(t *testing.T) {
	plan, err := ReadXUI(xuiFixture(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Customers) != 2 {
		t.Fatalf("покупателей %d, ожидались анна (два входа, один subId) и борис", len(plan.Customers))
	}
	anna := byName(t, plan, "anna-443")
	u := anna.User
	if u.ExternalID != "3x-ui:s1" || len(u.SubTokens) != 1 || u.SubTokens[0] != "s1" {
		t.Errorf("ключ %q, адреса подписки %q", u.ExternalID, u.SubTokens)
	}
	if len(u.VLESS) != 1 || len(u.Trojan) != 1 || u.Trojan[0] != "anna-trojan" {
		t.Errorf("VLESS %q, Trojan %q: должны прийти с обоих входов", u.VLESS, u.Trojan)
	}
	// Два входа по 50 ГБ — у человека было 100 ГБ; израсходовано 5 + 5.
	if u.TrafficLimit != 100*gb || u.UsedBefore != 10*gb {
		t.Errorf("квота %d, расход %d", u.TrafficLimit, u.UsedBefore)
	}
	if u.ExpiresAt == nil || !u.ExpiresAt.Equal(time.UnixMilli(1795000000000)) {
		t.Errorf("срок %v: берётся поздний из двух", u.ExpiresAt)
	}
	if u.MaxIPs != 2 || !u.Enabled {
		t.Errorf("устройств %d, включена %v", u.MaxIPs, u.Enabled)
	}
	if !hasNote(anna, "Vision") {
		t.Errorf("заметки %q", anna.Notes)
	}
}

func TestXUIClientDetailsSurviveOldNumberFormats(t *testing.T) {
	plan, err := ReadXUI(xuiFixture(t), now)
	if err != nil {
		t.Fatal(err)
	}
	boris := byName(t, plan, "Boris")
	u := boris.User
	if u.ExternalID != "3x-ui:email:boris" || len(u.SubTokens) != 0 {
		t.Errorf("без subId: ключ %q, адреса %q", u.ExternalID, u.SubTokens)
	}
	if u.TrafficLimit != 0 {
		t.Errorf("totalGB \"0\" — безлимит, а перенесено %d", u.TrafficLimit)
	}
	if u.Enabled {
		t.Error("3x-ui выключил клиента в client_traffics, а перенесён включённым")
	}
	if u.ExpiresAt == nil || !u.ExpiresAt.Equal(now.Add(30*24*time.Hour)) || !hasNote(boris, "с первого подключения") {
		t.Errorf("срок от первого подключения: %v, заметки %q", u.ExpiresAt, boris.Notes)
	}

	if plan.SubPath != "xYz123" || plan.SubPort != 2097 || plan.SubDomain != "sub.old.example" {
		t.Errorf("подписка жила на %s:%d/%s/", plan.SubDomain, plan.SubPort, plan.SubPath)
	}
	for _, c := range plan.Customers {
		if c.Name == "ghost" {
			t.Error("клиент выключенного входа перенесён, хотя в подписку он не попадал")
		}
	}
}

func TestMismatchedRealityKeysStopTheImport(t *testing.T) {
	stream := `{"network":"tcp","security":"reality","realitySettings":{"target":"a.com:443","privateKey":"` + rfcPrivate +
		`","shortIds":["01"],"settings":{"publicKey":"` + strings.Repeat("A", 43) + `"}}}`
	db := fixture(t, "x-ui.db",
		`CREATE TABLE inbounds (id INTEGER PRIMARY KEY, enable NUMERIC, port INTEGER, protocol TEXT, settings TEXT, stream_settings TEXT, tag TEXT)`,
		`INSERT INTO inbounds VALUES (1, 1, 443, 'vless', '{"clients":[]}', '`+stream+`', 'in')`)
	if _, err := ReadXUI(db, now); err == nil {
		t.Fatal("публичный ключ не от этого приватного, а перенос идёт дальше — ни одна ссылка не сошлась бы")
	}
}

func TestReportGivesNodeLineAndWritesNothing(t *testing.T) {
	path := xuiFixture(t)
	before, _ := os.ReadFile(path)

	plan, err := ReadXUI(path, now)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	plan.Report(&out, path)
	text := out.String()
	for _, want := range []string{"REALITY_KEY=" + rfcPrivate, "2 покупателя", "/xYz123/", "2097", "у 1 покупателя ссылки с Vision"} {
		if !strings.Contains(text, want) {
			t.Errorf("в отчёте нет %q:\n%s", want, text)
		}
	}

	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("чтение изменило базу прежней панели")
	}
}

func TestXUICustomerGetsSubscriptionAtOldAddressAfterMove(t *testing.T) {
	plan, err := ReadXUI(xuiFixture(t), now)
	if err != nil {
		t.Fatal(err)
	}
	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, _, err := store.CreateNode(ctx, panel.CreateNodeParams{Name: "old", Address: "198.51.100.7:443",
		SNI: "www.microsoft.com", RealityPublicKey: rfcPublic, RealityShortID: "c0ffee"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range plan.Customers {
		if _, err := store.ImportUser(ctx, c.User); err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
	}
	if err := store.AddLegacySubPath(ctx, plan.SubPath); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(panel.NewAPI(store, strings.Repeat("a", 32), "https://panel.example.com", t.TempDir()).Handler())
	defer server.Close()
	resp, err := http.Get(server.URL + "/xYz123/s1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	body, _ := base64.StdEncoding.DecodeString(string(raw))
	if resp.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), "vless://6f1a6c2e-3d4b-4e5f-8a9b-0c1d2e3f4a5b@198.51.100.7:443") ||
		!strings.Contains(string(body), "trojan://anna-trojan@198.51.100.7:443") {
		t.Fatalf("подписка по прежнему адресу: %d\n%s", resp.StatusCode, body)
	}
	// Остаток в заголовке — ровно как было: 100 ГБ, из них 10 израсходовано.
	if info := resp.Header.Get("Subscription-Userinfo"); !strings.Contains(info, "download=10737418240") ||
		!strings.Contains(info, "total=107374182400") {
		t.Errorf("Subscription-Userinfo: %q", info)
	}
}

func TestShortHandWrittenSubIDIsReported(t *testing.T) {
	db := fixture(t, "x-ui.db",
		`CREATE TABLE inbounds (id INTEGER PRIMARY KEY, enable NUMERIC, port INTEGER, protocol TEXT, settings TEXT, stream_settings TEXT, tag TEXT)`,
		`INSERT INTO inbounds VALUES (1, 1, 443, 'vless',
			'{"clients":[{"id":"6f1a6c2e-3d4b-4e5f-8a9b-0c1d2e3f4a5b","email":"a","subId":"user1"},
			 {"id":"6f1a6c2e-3d4b-4e5f-8a9b-0c1d2e3f4a5c","email":"b","subId":"k3j9x2mq8w1z4v7p"}]}',
			'{"network":"tcp","security":"tls"}', 'in')`)
	plan, err := ReadXUI(db, now)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "у 1 покупателя адрес подписки короче") {
			found = true
		}
	}
	if !found {
		t.Errorf("про подбираемый адрес подписки молчат: %q", plan.Warnings)
	}
}
