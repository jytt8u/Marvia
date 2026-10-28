package panel_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/users"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Проверяем границу панель → нода: выданный человеку закрытый ключ не должен
// попасть в список доступа, включая дополнительные поля записи.
func TestNodeAccessListDoesNotContainVP1PrivateKey(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("security")
	created := h.createUser(0, panel.CredVP1, panel.CredVLESS, panel.CredTrojan)
	private := created.secretOf(panel.CredVP1)
	raw, err := vp1.DecodeKey(private)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := vp1.KeyPairFromPrivate(raw)
	if err != nil {
		t.Fatal(err)
	}
	list := h.nodeUsers(node.Token)
	if len(list) != 3 {
		t.Fatalf("получено %d записей вместо трёх", len(list))
	}
	wire, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), private) {
		t.Fatal("закрытый ключ клиента передан ноде")
	}
	seen := map[string]bool{}
	for _, entry := range list {
		seen[entry.Kind] = true
		if entry.Kind == users.KindVP1 {
			if entry.Secret != vp1.EncodeKey(pair.Public) {
				t.Fatal("нода получила не публичный ключ клиента")
			}
		} else if entry.Secret != created.secretOf(entry.Kind) {
			t.Fatalf("учётные данные %s не совпадают с выданными клиенту", entry.Kind)
		}
	}
	if !seen[users.KindVP1] || !seen[users.KindVLESS] || !seen[users.KindTrojan] {
		t.Fatal("список не содержит все три протокола")
	}
}

// TestNodeAccessListDoesNotNameBuyers — нода не знает имён покупателей.
//
// Нода стоит на чужой дешёвой машине и изымается легче панели, а её токен
// лежит у неё же на диске. Список, который она забирает каждые пятнадцать
// секунд, должен быть ровно тем, что нужно для пропуска: ключи, сроки,
// лимиты. Имя покупателя в нём превращало бы любую изъятую или взломанную
// ноду в список клиентов продавца.
func TestNodeAccessListDoesNotNameBuyers(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("security")
	created := h.createUser(0, panel.CredVP1, panel.CredVLESS)
	if created.User.Label == "" {
		t.Fatal("у покупателя нет имени, проверять нечего")
	}

	code, body := h.raw("GET", "/api/v1/node/users", node.Token)
	if code != http.StatusOK {
		t.Fatalf("список для ноды: код %d", code)
	}
	if strings.Contains(body, created.User.Label) {
		t.Fatalf("имя покупателя %q ушло ноде", created.User.Label)
	}
	if !strings.Contains(body, created.secretOf(panel.CredVLESS)) {
		t.Fatal("в списке для ноды нет самого доступа — проверка ничего не доказывает")
	}
}
