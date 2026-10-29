package mobile

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Полевой замер обещает одно: отличить заморозку от «не открылось» и от
// «прошло». Перепутай он их — доброволец принесёт отчёт, по которому
// построят не тот обход.

func withStall(t *testing.T, d time.Duration) {
	t.Helper()
	was := fieldStall
	fieldStall = d
	t.Cleanup(func() { fieldStall = was })
}

func TestFreezeIsToldApartFromFullShortAndBlocked(t *testing.T) {
	withStall(t, 300*time.Millisecond)
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/full":
			_, _ = w.Write(make([]byte, fieldLimit+1024))
		case "/exact":
			// Ровно предел: конец файла приходит вместе с последним куском.
			_, _ = w.Write(make([]byte, fieldLimit))
		case "/short":
			_, _ = w.Write([]byte("маленькая страница"))
		case "/freeze":
			// Как ТСПУ: первые ~16 КБ проходят, дальше тишина, соединение
			// при этом не рвётся.
			_, _ = w.Write(make([]byte, 16<<10))
			w.(http.Flusher).Flush()
			<-release
		}
	}))
	defer server.Close()
	defer close(release)
	ctx := context.Background()

	if got := fetchUntilStall(ctx, server.Client(), server.URL+"/full"); got.st != fieldFull || got.bytes < fieldLimit {
		t.Errorf("полная передача названа %q (%d байт)", got.st, got.bytes)
	}
	if got := fetchUntilStall(ctx, server.Client(), server.URL+"/exact"); got.st != fieldFull {
		t.Errorf("передача ровно до предела названа %q", got.st)
	}
	if got := fetchUntilStall(ctx, server.Client(), server.URL+"/short"); got.st != fieldShort {
		t.Errorf("короткий ответ назван %q", got.st)
	}
	got := fetchUntilStall(ctx, server.Client(), server.URL+"/freeze")
	if got.st != fieldFreeze || got.bytes < 8<<10 || got.bytes > 20<<10 {
		t.Errorf("заморозка названа %q на %d байтах", got.st, got.bytes)
	}

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := closed.Addr().String()
	_ = closed.Close()
	if got := fetchUntilStall(ctx, server.Client(), "https://"+addr+"/"); got.st != fieldBlocked {
		t.Errorf("недоступный адрес назван %q", got.st)
	}
}

func TestWhitelistModeNeedsDomesticOpenAndForeignShut(t *testing.T) {
	cases := []struct {
		dom, abroad int
		want        string
	}{
		{3, 0, "whitelist"},
		{1, 0, "whitelist"},
		{3, 2, "open"},
		{0, 0, fieldBlocked}, // сети нет вовсе — про белый список ничего не сказать
	}
	for _, c := range cases {
		if got := judgeWhitelist(c.dom, 3, c.abroad, 3).St; got != c.want {
			t.Errorf("российских %d, зарубежных %d: %q, ожидалось %q", c.dom, c.abroad, got, c.want)
		}
	}
}

// У каждого шага и исхода замера есть слова на обоих языках приложения:
// иначе строка отчёта осталась бы пустой.
func TestEveryFieldStepAndOutcomeHasWords(t *testing.T) {
	res := filepath.Join("..", "android", "app", "src", "main", "res")
	need := []string{"field_title", "field_intro", "field_run", "field_need_off", "field_report"}
	for _, id := range fieldSteps {
		need = append(need, "field_step_"+id)
	}
	for _, st := range []string{fieldFull, fieldFreeze, fieldBlocked, fieldShort, fieldOff, "partial", "whitelist", "open"} {
		need = append(need, "field_st_"+st)
	}
	for _, dir := range []string{"values", "values-ru"} {
		raw, err := os.ReadFile(filepath.Join(res, dir, "diagnostics.xml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range need {
			if !strings.Contains(string(raw), `name="`+name+`"`) {
				t.Errorf("в %s/diagnostics.xml нет строки %s", dir, name)
			}
		}
	}
}
