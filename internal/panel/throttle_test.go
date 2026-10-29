package panel_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
)

// slowReports — трое разных покупателей сообщают, за сколько нода отдала им
// пробную порцию.
func slowReports(t *testing.T, store *panel.Store, nodeID int64, fetchMS int64) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		u, _, err := store.CreateUser(ctx, panel.CreateUserParams{Label: "покупатель"})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveReports(ctx, u.ID, []panel.Report{{NodeID: nodeID, OK: true, LatencyMS: 40, FetchMS: fetchMS}}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestNodeIsCalledThrottledWhenBuyersCannotPullFromIt: нода на связи, к ней
// подключаются, но пробную порцию трое покупателей тянут по шесть секунд —
// панель называет её зарезанной, называет скорость и ставит в подписке ниже
// здоровой. Один медленный покупатель — не повод: это может быть его вайфай.
func TestNodeIsCalledThrottledWhenBuyersCannotPullFromIt(t *testing.T) {
	store := alertStore(t)
	ctx := context.Background()
	slow, _, _ := store.CreateNode(ctx, panel.CreateNodeParams{Name: "marvia-node-x", Address: "203.0.113.1:443"})
	fast, _, _ := store.CreateNode(ctx, panel.CreateNodeParams{Name: "fi-1", Address: "203.0.113.2:443"})
	slowReports(t, store, slow.ID, 6000)
	slowReports(t, store, fast.ID, 300)

	health, err := store.NodeHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h := health[slow.ID]; !h.Throttled() || h.FetchKbps() < 150 || h.FetchKbps() > 200 {
		t.Fatalf("зарезанная нода: %+v, %d кбит/с", h, h.FetchKbps())
	}
	if health[fast.ID].Throttled() {
		t.Fatal("здоровую ноду назвали зарезанной")
	}
	order := panel.RankNodes([]panel.Node{slow, fast}, health)
	if order[0].ID != fast.ID {
		t.Fatalf("зарезанная нода стоит в подписке первой")
	}

	one, _, _ := store.CreateNode(ctx, panel.CreateNodeParams{Name: "один медленный", Address: "203.0.113.3:443"})
	u, _, _ := store.CreateUser(ctx, panel.CreateUserParams{Label: "один"})
	_ = store.SaveReports(ctx, u.ID, []panel.Report{{NodeID: one.ID, OK: true, LatencyMS: 40, FetchMS: 9000}})
	health, _ = store.NodeHealth(ctx)
	if health[one.ID].Throttled() {
		t.Fatal("одного медленного покупателя хватило, чтобы назвать ноду зарезанной")
	}
}

// TestSellerIsToldOnceThatANodeIsThrottled: о зарезанной ноде продавец узнаёт
// в телеграме один раз, со скоростью и советом, а не каждые пять минут.
func TestSellerIsToldOnceThatANodeIsThrottled(t *testing.T) {
	store := alertStore(t)
	ctx := context.Background()
	node, _, _ := store.CreateNode(ctx, panel.CreateNodeParams{Name: "marvia-node-x", Address: "203.0.113.1:443"})
	if err := store.TouchNode(ctx, node.ID); err != nil {
		t.Fatal(err)
	}
	slowReports(t, store, node.ID, 6000)

	var box sent
	alerts := panel.NewAlerts(store)
	box.capture(alerts)
	alerts.Check(ctx)
	alerts.Check(ctx)
	var told []string
	for _, text := range box.texts {
		if strings.Contains(text, "режут") {
			told = append(told, text)
		}
	}
	if len(told) != 1 || !strings.Contains(told[0], "marvia-node-x") || !strings.Contains(told[0], "кбит/с") {
		t.Fatalf("про зарезанную ноду: %v", box.texts)
	}
}

// TestNodesListShowsThrottling: в списке нод у зарезанной — пометка и
// скорость у покупателей, чтобы окно панели нарисовало это на карточке.
func TestNodesListShowsThrottling(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("slow")
	for i := 0; i < 3; i++ {
		u := h.createUser(0)
		body := `{"reports":[{"node_id":` + itoa(node.Node.ID) + `,"ok":true,"latency_ms":40,"fetch_ms":6000}]}`
		resp, err := h.server.Client().Post(h.server.URL+"/sub/"+u.User.SubToken+"/report", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("отчёт с fetch_ms: %s", resp.Status)
		}
	}
	var out struct {
		Nodes []struct {
			ID        int64 `json:"id"`
			Throttled bool  `json:"throttled"`
			FetchKbps int64 `json:"fetch_kbps"`
		} `json:"nodes"`
	}
	h.do(http.MethodGet, "/api/v1/nodes", adminToken, nil, &out)
	if len(out.Nodes) != 1 || !out.Nodes[0].Throttled || out.Nodes[0].FetchKbps == 0 {
		t.Fatalf("список нод: %+v", out.Nodes)
	}
}
