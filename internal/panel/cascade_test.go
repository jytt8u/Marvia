package panel_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
)

// registerEntry выпускает приглашение входа для exit и записывает по нему
// вход так, как это делает установщик.
func (h *harness) registerEntry(exitID int64, name string) createNodeResponse {
	h.t.Helper()
	var invite struct {
		Token   string `json:"token"`
		Command string `json:"command"`
	}
	if code := h.do(http.MethodPost, "/api/v1/nodes/invite", adminToken, map[string]any{"exit_id": exitID}, &invite); code != http.StatusOK {
		h.t.Fatalf("приглашение входа: код %d", code)
	}
	var out createNodeResponse
	if code := h.do(http.MethodPost, "/api/v1/nodes/register", invite.Token, map[string]any{"name": name, "port": 443}, &out); code != http.StatusOK {
		h.t.Fatalf("запись входа: код %d", code)
	}
	return out
}

type syncAnswer struct {
	Users   []json.RawMessage `json:"users"`
	RelayTo string            `json:"relay_to"`
	Relays  []string          `json:"relays"`
}

func (h *harness) sync(token string) syncAnswer {
	h.t.Helper()
	var out syncAnswer
	if code := h.do(http.MethodGet, "/api/v1/node/users", token, nil, &out); code != http.StatusOK {
		h.t.Fatalf("сверка: код %d", code)
	}
	return out
}

// TestEntryLearnsOnlyWhereToForward: вход узнаёт у панели адрес выхода и
// ничего о покупателях; выход — адрес своего входа, от которого верить
// адресу покупателя. Выключенный выход — пустой адрес: вход перестаёт вести
// туда покупателей.
func TestEntryLearnsOnlyWhereToForward(t *testing.T) {
	h := newHarness(t)
	h.createUser(0)
	exit := h.createNode("helsinki")
	entry := h.registerEntry(exit.Node.ID, "moscow")

	if entry.Node.ExitID != exit.Node.ID || entry.Node.PublicKey != "" {
		t.Fatalf("вход записан как %+v", entry.Node)
	}
	got := h.sync(entry.Token)
	if len(got.Users) != 0 {
		t.Fatalf("вход получил покупателей: %d", len(got.Users))
	}
	if got.RelayTo != exit.Node.Address {
		t.Fatalf("вход ведёт на %q, а не на %q", got.RelayTo, exit.Node.Address)
	}

	at := h.sync(exit.Token)
	if len(at.Users) == 0 {
		t.Fatal("выход остался без покупателей")
	}
	if len(at.Relays) != 1 || at.Relays[0] != "127.0.0.1" {
		t.Fatalf("выход верит адресам %v", at.Relays)
	}

	off := false
	h.do(http.MethodPatch, "/api/v1/nodes/"+itoa(exit.Node.ID), adminToken, map[string]any{"enabled": &off}, nil)
	if got := h.sync(entry.Token); got.RelayTo != "" {
		t.Fatalf("вход ведёт на выключенный выход %q", got.RelayTo)
	}
}

// TestBuyersSeeTheEntryAsTheExitAtAnotherAddress: в подписке вход — это
// выход с тем же ключом, но адресом входа и своим номером; QUIC через вход
// не обещается.
func TestBuyersSeeTheEntryAsTheExitAtAnotherAddress(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(0)
	exit := h.createNode("helsinki")
	h.do(http.MethodPatch, "/api/v1/nodes/"+itoa(exit.Node.ID), adminToken, map[string]any{"country": "Финляндия"}, nil)
	entry := h.registerEntry(exit.Node.ID, "Москва")

	resp, err := h.server.Client().Get(h.server.URL + "/sub/" + created.User.SubToken + "?format=json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sub struct {
		Nodes []struct {
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			Country   string `json:"country"`
			Address   string `json:"address"`
			PublicKey string `json:"public_key"`
			QUIC      bool   `json:"quic"`
		} `json:"nodes"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &sub); err != nil {
		t.Fatalf("подписка: %v", err)
	}
	if len(sub.Nodes) != 2 {
		t.Fatalf("в подписке %d нод, ждали выход и вход", len(sub.Nodes))
	}
	for _, n := range sub.Nodes {
		if n.ID != entry.Node.ID {
			continue
		}
		if n.PublicKey != exit.Node.PublicKey || n.Country != "Финляндия" || n.QUIC {
			t.Fatalf("вход в подписке: %+v", n)
		}
		if n.Address != entry.Node.Address || !strings.Contains(n.Name, "Москва") {
			t.Fatalf("вход в подписке ведёт на %s и зовётся %q", n.Address, n.Name)
		}
		return
	}
	t.Fatalf("входа нет в подписке: %s", raw)
}

// TestOnlyAPlainUpToDateNodeCanBeAnExit: выходом не бывает сам вход, нода за
// CDN и нода, не понимающая заголовка входа, — иначе вход «работал» бы, а
// подключиться не мог никто.
func TestOnlyAPlainUpToDateNodeCanBeAnExit(t *testing.T) {
	h := newHarness(t)
	exit := h.createNode("helsinki")
	entry := h.registerEntry(exit.Node.ID, "moscow")

	ask := func(id int64) int {
		return h.do(http.MethodPost, "/api/v1/nodes/invite", adminToken, map[string]any{"exit_id": id}, nil)
	}
	if code := ask(entry.Node.ID); code != http.StatusBadRequest {
		t.Fatalf("вход ко входу: код %d", code)
	}
	if code := ask(999); code != http.StatusBadRequest {
		t.Fatalf("вход к несуществующей ноде: код %d", code)
	}

	old := h.createNode("istanbul")
	h.do(http.MethodPost, "/api/v1/node/usage", old.Token, map[string]any{"usage": map[string]any{}, "version": "v0.13.0-alpha.22"}, nil)
	if code := ask(old.Node.ID); code != http.StatusBadRequest {
		t.Fatalf("вход к ноде без поддержки входов: код %d", code)
	}

	// Сменить выход можно только у входа и только на годную ноду.
	if code := h.do(http.MethodPatch, "/api/v1/nodes/"+itoa(exit.Node.ID), adminToken, map[string]any{"exit_id": old.Node.ID}, nil); code != http.StatusBadRequest {
		t.Fatalf("обычная нода стала входом: код %d", code)
	}
	if code := h.do(http.MethodPatch, "/api/v1/nodes/"+itoa(entry.Node.ID), adminToken, map[string]any{"exit_id": old.Node.ID}, nil); code != http.StatusBadRequest {
		t.Fatalf("вход переведён на ноду без поддержки входов: код %d", code)
	}
	fresh := h.createNode("amsterdam")
	var changed struct {
		Node panel.Node `json:"node"`
	}
	if code := h.do(http.MethodPatch, "/api/v1/nodes/"+itoa(entry.Node.ID), adminToken, map[string]any{"exit_id": fresh.Node.ID}, &changed); code != http.StatusOK || changed.Node.ExitID != fresh.Node.ID {
		t.Fatalf("смена выхода: код %d, %+v", code, changed.Node)
	}
	if got := h.sync(entry.Token); got.RelayTo != fresh.Node.Address {
		t.Fatalf("после смены вход ведёт на %q", got.RelayTo)
	}
}
