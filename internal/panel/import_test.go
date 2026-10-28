package panel_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/panel"
)

// Токены подписки Marzban для проверки совместимости.
//
// Собраны не Go-кодом панели, а отдельно, на Node.js, построчно по
// app/utils/jwt.py Marzban 0.8.4: create_subscription_token для нового формата
// и PyJWT HS256 для старого. Если разбор токена в панели разойдётся с
// Marzban хоть в одном символе, эти строки перестанут приниматься — а с ними
// и подписки всех, кто переехал.
const (
	marzbanSecret = "f1c7a3e2d4b5968778695a4b3c2d1e0ff1c7a3e2d4b5968778695a4b3c2d1e0f"

	// «Alice_01», выдан в 1758000000.
	marzbanToken = "QWxpY2VfMDEsMTc1ODAwMDAwMAcogkHJkhgq"
	// «mike», выдан в 1758000123.
	marzbanTokenMike = "bWlrZSwxNzU4MDAwMTIzOYLz4S7QNf"
	// Старый формат: JWT с sub «alice_01» и iat 1700000000.
	marzbanJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJhbGljZV8wMSIsImFjY2VzcyI6InN1YnNjcmlwdGlvbiIsImlhdCI6MTcwMDAwMDAwMH0.eoqflrLLm6uKGkFeXu5rG6t6qu8jM6ELhCcRL7kypvU"
)

type importBench struct {
	t      *testing.T
	store  *panel.Store
	server *httptest.Server
}

func newImportBench(t *testing.T) *importBench {
	t.Helper()
	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, _, err := store.CreateNode(context.Background(), panel.CreateNodeParams{
		Name: "fi-1", Address: "203.0.113.5:443", SNI: "www.example.com",
		RealityPublicKey: "pbk", RealityShortID: "0123abcd",
	}); err != nil {
		t.Fatalf("нода: %v", err)
	}

	server := httptest.NewServer(panel.NewAPI(store, adminToken, "https://sub.example.com", t.TempDir()).Handler())
	t.Cleanup(server.Close)
	return &importBench{t: t, store: store, server: server}
}

func (b *importBench) importUser(u panel.ImportedUser) panel.ImportResult {
	b.t.Helper()
	res, err := b.store.ImportUser(context.Background(), u)
	if err != nil {
		b.t.Fatalf("перенос %s: %v", u.ExternalID, err)
	}
	return res
}

// fetch возвращает код ответа и тело подписки, раскрытое из base64.
func (b *importBench) fetch(method, path string) (int, string) {
	b.t.Helper()
	req, err := http.NewRequest(method, b.server.URL+path, nil)
	if err != nil {
		b.t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, ""
	}
	body, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		b.t.Fatalf("подписка не в base64: %q", raw)
	}
	return resp.StatusCode, string(body)
}

func TestImportedCustomerConnectsWithHisOldUUIDAndPassword(t *testing.T) {
	b := newImportBench(t)
	res := b.importUser(panel.ImportedUser{
		ExternalID: "3x-ui:abc", Label: "alice", Enabled: true,
		// Регистр у UUID бывает любым: Xray сравнивает байты.
		VLESS:  []string{"6F1A6C2E-3D4B-4E5F-8A9B-0C1D2E3F4A5B"},
		Trojan: []string{"old-trojan-password"},
	})

	list, err := b.store.NodeUsers(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, u := range list {
		got[u.Kind] = u.Secret
		if !u.Enabled {
			t.Errorf("%s: перенесённый покупатель выключен", u.Kind)
		}
	}
	if got["vless"] != "6f1a6c2e-3d4b-4e5f-8a9b-0c1d2e3f4a5b" {
		t.Errorf("нода не знает прежний UUID: %q", got["vless"])
	}
	if got["trojan"] != "old-trojan-password" {
		t.Errorf("нода не знает прежний пароль Trojan: %q", got["trojan"])
	}
	if got["vp1"] != "" {
		t.Error("ключ vp1 выпущен молча: его приватную часть никто не увидел бы")
	}

	user, err := b.store.GetUser(context.Background(), res.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if user.ExternalID != "3x-ui:abc" || user.Label != "alice" {
		t.Errorf("покупатель записан не тот: %+v", user)
	}
}

func TestRepeatedImportDoesNotCreateSecondCustomer(t *testing.T) {
	b := newImportBench(t)
	u := panel.ImportedUser{ExternalID: "marzban:bob", Label: "bob", Enabled: true,
		VLESS: []string{"11111111-2222-4333-8444-555555555555"}}

	first := b.importUser(u)
	second := b.importUser(u)
	if !second.Existed || second.UserID != first.UserID {
		t.Fatalf("повторный перенос: %+v, ожидался уже перенесённый №%d", second, first.UserID)
	}
	all, _ := b.store.ListUsers(context.Background())
	if len(all) != 1 {
		t.Fatalf("покупателей %d, а переносили одного", len(all))
	}
}

func TestUsageBeforeMoveCountsTowardsQuota(t *testing.T) {
	b := newImportBench(t)
	const gb = 1 << 30
	half := b.importUser(panel.ImportedUser{ExternalID: "x:half", Enabled: true,
		TrafficLimit: 100 * gb, UsedBefore: 60 * gb,
		VLESS: []string{"11111111-2222-4333-8444-000000000001"}})
	b.importUser(panel.ImportedUser{ExternalID: "x:spent", Enabled: true,
		TrafficLimit: 100 * gb, UsedBefore: 100 * gb,
		VLESS: []string{"11111111-2222-4333-8444-000000000002"}})

	user, _ := b.store.GetUser(context.Background(), half.UserID)
	if user.Used != 60*gb {
		t.Errorf("расход %d, а до переезда было 60 ГБ", user.Used)
	}

	list, _ := b.store.NodeUsers(context.Background(), 1)
	for _, u := range list {
		switch u.Secret {
		case "11111111-2222-4333-8444-000000000001":
			if u.TrafficLimit != 40*gb {
				t.Errorf("ноде дан остаток %d, а не 40 ГБ: переезд подарил трафик", u.TrafficLimit)
			}
		case "11111111-2222-4333-8444-000000000002":
			if u.Enabled {
				t.Error("квота выбрана ещё на прежней панели, а нода пускает")
			}
		}
	}
}

func TestOldSubscriptionAddressOf3xuiKeepsWorking(t *testing.T) {
	b := newImportBench(t)
	b.importUser(panel.ImportedUser{ExternalID: "3x-ui:k3j9x2", Label: "carol", Enabled: true,
		VLESS: []string{"11111111-2222-4333-8444-555555555555"}, SubTokens: []string{"k3j9x2mq8w1z4v7p"}})

	code, body := b.fetch("GET", "/sub/k3j9x2mq8w1z4v7p")
	if code != http.StatusOK || !strings.Contains(body, "vless://11111111-2222-4333-8444-555555555555@203.0.113.5:443") {
		t.Fatalf("старый адрес подписки: %d %q", code, body)
	}

	// Свой путь у 3x-ui часто случайный. Он открывается, только когда его
	// перенесли, и только для прежних адресов.
	if code, _ := b.fetch("GET", "/x7Gq2Lp/k3j9x2mq8w1z4v7p"); code != http.StatusNotFound {
		t.Errorf("путь, которого не было у прежней панели, ответил %d", code)
	}
	if err := b.store.AddLegacySubPath(context.Background(), "/x7Gq2Lp/"); err != nil {
		t.Fatal(err)
	}
	if code, _ := b.fetch("GET", "/x7Gq2Lp/k3j9x2mq8w1z4v7p"); code != http.StatusOK {
		t.Errorf("подписка по пути прежней панели: %d", code)
	}
	if code, _ := b.fetch("GET", "/x7Gq2Lp/чужой"); code != http.StatusNotFound {
		t.Errorf("неизвестный токен на прежнем пути: %d", code)
	}
	if code, _ := b.fetch("POST", "/x7Gq2Lp/k3j9x2mq8w1z4v7p"); code != http.StatusNotFound {
		t.Errorf("POST на путь подписки: %d, ожидался 404, как на любой чужой путь", code)
	}
}

func TestUnknownTwoPartPathsStillAnswerNotFound(t *testing.T) {
	b := newImportBench(t)
	for _, m := range []string{"GET", "POST", "PUT"} {
		if code, _ := b.fetch(m, "/wp-admin/install.php"); code != http.StatusNotFound {
			t.Errorf("%s по пути сканера: %d — панель выдаёт себя ответом не 404", m, code)
		}
	}
}

func TestMarzbanSubscriptionTokensAreAccepted(t *testing.T) {
	b := newImportBench(t)
	created := time.Unix(1690000000, 0)
	b.importUser(panel.ImportedUser{ExternalID: "marzban:alice_01", Label: "Alice_01", Enabled: true,
		VLESS:       []string{"11111111-2222-4333-8444-555555555555"},
		MarzbanName: "Alice_01", MarzbanSecret: marzbanSecret, NotBefore: created})

	// Новый формат; имя в токене с заглавной, в базе — как угодно: Marzban
	// сравнивает без учёта регистра.
	if code, body := b.fetch("GET", "/sub/"+marzbanToken); code != http.StatusOK || !strings.Contains(body, "vless://") {
		t.Errorf("токен Marzban нового формата: %d %q", code, body)
	}
	// Старый формат — JWT.
	if code, _ := b.fetch("GET", "/sub/"+marzbanJWT); code != http.StatusOK {
		t.Errorf("токен Marzban в виде JWT: %d", code)
	}
	// Подпись верна, но такого покупателя не переносили.
	if code, _ := b.fetch("GET", "/sub/"+marzbanTokenMike); code != http.StatusNotFound {
		t.Errorf("токен непереехавшего покупателя: %d", code)
	}
	// Испорченная подпись.
	forged := marzbanToken[:len(marzbanToken)-1] + "A"
	if code, _ := b.fetch("GET", "/sub/"+forged); code != http.StatusNotFound {
		t.Errorf("токен с поддельной подписью принят: %d", code)
	}
}

func TestMarzbanTokenIssuedBeforeRevocationIsRejected(t *testing.T) {
	b := newImportBench(t)
	// Подписку отозвали после того, как выдали оба токена: Marzban их уже
	// не принимал, и переезд не должен их воскрешать.
	b.importUser(panel.ImportedUser{ExternalID: "marzban:alice_01", Enabled: true,
		VLESS:       []string{"11111111-2222-4333-8444-555555555555"},
		MarzbanName: "alice_01", MarzbanSecret: marzbanSecret, NotBefore: time.Unix(1758000001, 0)})

	for name, token := range map[string]string{"новый": marzbanToken, "JWT": marzbanJWT} {
		if code, _ := b.fetch("GET", "/sub/"+token); code != http.StatusNotFound {
			t.Errorf("%s формат: токен до отзыва подписки принят (%d)", name, code)
		}
	}
}

func TestMarzbanTokenSignedByAnotherPanelIsRejected(t *testing.T) {
	b := newImportBench(t)
	b.importUser(panel.ImportedUser{ExternalID: "marzban:alice_01", Enabled: true,
		VLESS:       []string{"11111111-2222-4333-8444-555555555555"},
		MarzbanName: "alice_01", MarzbanSecret: strings.Repeat("0", 64)})

	if code, _ := b.fetch("GET", "/sub/"+marzbanToken); code != http.StatusNotFound {
		t.Errorf("токен чужой панели Marzban принят: %d", code)
	}
}

func TestRotatingSubTokenRevokesOldPanelAddresses(t *testing.T) {
	b := newImportBench(t)
	res := b.importUser(panel.ImportedUser{ExternalID: "marzban:alice_01", Enabled: true,
		VLESS:       []string{"11111111-2222-4333-8444-555555555555"},
		SubTokens:   []string{"k3j9x2mq8w1z4v7p"},
		MarzbanName: "alice_01", MarzbanSecret: marzbanSecret})

	if _, err := b.store.RotateSubToken(context.Background(), res.UserID); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"k3j9x2mq8w1z4v7p", marzbanToken, marzbanJWT} {
		if code, _ := b.fetch("GET", "/sub/"+token); code != http.StatusNotFound {
			t.Errorf("после смены ссылки прежний адрес %s…: %d", token[:8], code)
		}
	}
}

func TestImportDoesNotHandOverAnotherCustomersSecret(t *testing.T) {
	b := newImportBench(t)
	shared := "11111111-2222-4333-8444-555555555555"
	b.importUser(panel.ImportedUser{ExternalID: "x:first", Enabled: true, VLESS: []string{shared},
		SubTokens: []string{"same-sub-id"}})

	res := b.importUser(panel.ImportedUser{ExternalID: "x:second", Enabled: true,
		VLESS: []string{shared}, Trojan: []string{"own-password"}, SubTokens: []string{"same-sub-id"}})
	if len(res.Skipped) != 2 {
		t.Errorf("пропущено %q, ожидались чужой UUID и чужой адрес подписки", res.Skipped)
	}
	if code, body := b.fetch("GET", "/sub/same-sub-id"); code != http.StatusOK || strings.Contains(body, "trojan://") {
		t.Errorf("адрес первого покупателя отдаёт подписку второго: %d %q", code, body)
	}

	_, err := b.store.ImportUser(context.Background(), panel.ImportedUser{
		ExternalID: "x:third", Enabled: true, VLESS: []string{shared}})
	if !errors.Is(err, panel.ErrNoAccess) {
		t.Fatalf("покупатель только с чужим UUID: %v, ожидался отказ", err)
	}
	if _, err := b.store.UserByExternalID(context.Background(), "x:third"); !errors.Is(err, panel.ErrNotFound) {
		t.Error("покупатель без единого ключа всё-таки записан")
	}
}
