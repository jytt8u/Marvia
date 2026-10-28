//go:build windows

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
)

// Сквозное окно достаёт движок из неэкспортированного поля go-webview2.
// Библиотека обновится и переименует поле — режим молча станет сплошным с
// белым фоном. Тест читает исходник той версии, что в go.mod, и краснеет
// раньше, чем это увидит человек.
func TestWebViewStillHasItsEngineWhereWeReachForIt(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/jchv/go-webview2").Output()
	if err != nil {
		t.Skipf("исходник go-webview2 не найден: %v", err)
	}
	src := filepath.Join(strings.TrimSpace(string(out)), "webview.go")
	file, err := parser.ParseFile(token.NewFileSet(), src, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	field, assigned := false, false
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.TypeSpec:
			if st, ok := n.Type.(*ast.StructType); ok && n.Name.Name == "webview" {
				for _, f := range st.Fields.List {
					for _, name := range f.Names {
						field = field || name.Name == "browser"
					}
				}
			}
		case *ast.FuncDecl:
			if n.Name.Name != "NewWithOptions" || n.Body == nil {
				return true
			}
			// В поле кладётся именно движок Chromium из edge — его и ждёт
			// приведение типа в chromiumOf.
			ast.Inspect(n.Body, func(m ast.Node) bool {
				if call, ok := m.(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewChromium" {
						assigned = true
					}
				}
				return true
			})
		}
		return true
	})
	if !field {
		t.Error("у webview в go-webview2 больше нет поля browser — сквозное окно его не найдёт")
	}
	if !assigned {
		t.Error("NewWithOptions больше не создаёт edge.Chromium — сквозное окно не получит движок")
	}
}

// Достаём движок из того же поля, что у настоящего окна, и не падаем, если
// окно устроено иначе: тогда режим просто не включается.
func TestChromiumIsFoundInTheBrowserFieldAndAbsentElsewhere(t *testing.T) {
	type viewWithEngine struct {
		webview2.WebView
		browser any
	}
	engine := &edge.Chromium{}
	if got := chromiumOf(&viewWithEngine{browser: engine}); got != engine {
		t.Fatalf("движок не найден в поле browser: %v", got)
	}
	type viewWithout struct{ webview2.WebView }
	if got := chromiumOf(&viewWithout{}); got != nil {
		t.Fatalf("у окна без поля нашёлся движок: %v", got)
	}
	if got := chromiumOf(nil); got != nil {
		t.Fatalf("у пустого окна нашёлся движок: %v", got)
	}
}
