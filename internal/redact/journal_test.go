package redact_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestServerJournalsNeverTakeBuyerData — ни одна строка журнала панели и ноды
// не получает адрес покупателя, адрес назначения, токен или метку.
//
// Проверка по исходникам, а не по прогону: журнал пишется из сотни мест, и
// прогнать каждое — значит написать сотню тестов, которые всё равно не
// поймают сто первое место. Здесь каждый вызов log.* в коде серверов
// разбирается, и его аргументы сверяются со списком имён, за которыми
// стоят данные покупателя. Новое место, которое захочет записать такое,
// упадёт здесь, а не всплывёт в журнале у продавца.
func TestServerJournalsNeverTakeBuyerData(t *testing.T) {
	root := filepath.Join("..", "..")
	dirs := []string{
		"cmd/marvia-panel", "cmd/marvia-node",
		"internal/panel", "internal/users", "internal/nodesync",
		"internal/inbound", "internal/egress", "internal/transport",
		"internal/fallback", "internal/mux", "internal/relay", "internal/vp1",
	}

	// Имена, за которыми в этом коде стоят данные покупателя или секреты.
	forbidden := map[string]string{
		"peer":       "адрес покупателя",
		"remote":     "адрес покупателя",
		"RemoteAddr": "адрес покупателя",
		"Header":     "заголовки запроса: адреса прокси, устройство",
		"target":     "куда шёл покупатель",
		"Target":     "куда шёл покупатель",
		"request":    "запрос покупателя целиком",
		"token":      "токен",
		"Token":      "токен",
		"SubToken":   "токен подписки",
		"Secret":     "секрет доступа",
		"secret":     "секрет доступа",
		"identity":   "ключ покупателя",
		"UUID":       "ключ покупателя",
		"Digest":     "ключ покупателя",
		"Label":      "имя покупателя",
		"ExternalID": "ключ покупателя у продавца",
	}

	// Там, где нода обслуживает соединения покупателей, ошибка уходит в
	// журнал только через why(): ошибки сети несут оба конца соединения.
	whyOnly := map[string]bool{"cmd/marvia-node/serve.go": true}

	checked := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			rel := dir + "/" + name
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, 0)
			if err != nil {
				t.Fatalf("%s: %v", rel, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !isLogCall(call) {
					return true
				}
				checked++
				for _, arg := range call.Args {
					pos := fset.Position(arg.Pos())
					ast.Inspect(arg, func(n ast.Node) bool {
						id, ok := n.(*ast.Ident)
						if !ok {
							return true
						}
						if why, bad := forbidden[id.Name]; bad {
							t.Errorf("%s:%d: в журнал уходит %s (%s)", rel, pos.Line, id.Name, why)
						}
						return true
					})
					if whyOnly[rel] {
						if id, ok := arg.(*ast.Ident); ok && (id.Name == "err" || id.Name == "cause") {
							t.Errorf("%s:%d: ошибка уходит в журнал мимо why() — с ней уйдёт и адрес", rel, pos.Line)
						}
					}
				}
				return true
			})
		}
	}
	if checked < 50 {
		t.Fatalf("разобрано вызовов журнала %d — подозрительно мало, проверка смотрит не туда", checked)
	}
}

// isLogCall — вызов журнала: log.Printf и родня из стандартного пакета.
func isLogCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "log" {
		return false
	}
	switch sel.Sel.Name {
	case "Print", "Printf", "Println", "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln":
		return true
	}
	return false
}
