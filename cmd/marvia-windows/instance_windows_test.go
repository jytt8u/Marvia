//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestOneSessionKeepsOnlyOneInstance(t *testing.T) {
	name := fmt.Sprintf("Local\\Marvia.Test.%d.%d", os.Getpid(), time.Now().UnixNano())
	first, accepted, err := acquireInstance(name)
	if err != nil || !accepted {
		t.Fatalf("первый запуск не принят: %v", err)
	}
	second, accepted, err := acquireInstance(name)
	if err != nil || accepted {
		t.Fatalf("второй запуск принят: %v", err)
	}
	_ = windows.CloseHandle(first)
	_ = windows.CloseHandle(second)
	third, accepted, err := acquireInstance(name)
	if third != 0 {
		defer windows.CloseHandle(third)
	}
	if err != nil || !accepted {
		t.Fatalf("повторный запуск после выхода заблокирован: %v", err)
	}
}

func TestSecondProcessOpensTheExistingTrayWindow(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, err := instanceName()
	if err != nil {
		t.Fatal(err)
	}
	guard, first, err := acquireInstance(name)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(guard)
	if !first {
		t.Skip("уже работает новая Marvia; её экземпляр не трогаем")
	}
	icon := testTray(t)
	defer icon.Remove()
	shown := false
	icon.onShow = func() { shown = true }
	cmd := exec.Command(os.Args[0], "-test.run=^TestForwardingChild$")
	cmd.Env = append(os.Environ(), "MARVIA_FORWARDING_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("второй процесс не открыл окно: %v, %s", err, output)
	}
	var msg struct {
		HWND           uintptr
		ID             uint32
		WParam, LParam uintptr
		Time           uint32
		Point          point
		Private        uint32
	}
	peek := user32.NewProc("PeekMessageW")
	dispatch := user32.NewProc("DispatchMessageW")
	for {
		found, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg)), icon.hwnd, 0, 0, 1)
		if found == 0 {
			break
		}
		_, _, _ = dispatch.Call(uintptr(unsafe.Pointer(&msg)))
	}
	if !shown {
		t.Fatal("второй процесс не передал показ существующему окну")
	}
}

func TestForwardingChild(t *testing.T) {
	if os.Getenv("MARVIA_FORWARDING_CHILD") != "1" {
		t.Skip("вспомогательный процесс")
	}
	running, err := forwardToRunning()
	if err != nil || !running {
		t.Fatalf("не найден существующий экземпляр: %v", err)
	}
}

func TestElevationKeepsEveryArgumentWhole(t *testing.T) {
	args := []string{"-url-file", `C:\Users\Имя Фамилия\my file.txt`, "-dns", "1.1.1.1:53", `marvia://test-key@panel.example/sub/token?name=one two`, `trailing\`, `quote"inside`, ""}
	parsed, err := windows.DecomposeCommandLine(`"Marvia.exe" ` + elevatedArguments(args))
	if err != nil || !reflect.DeepEqual(parsed[1:], args) {
		t.Fatalf("аргументы изменились при повышении прав: %v", err)
	}
}

func TestInstanceLinkRejectsCommandsAndMalformedPayloads(t *testing.T) {
	for _, value := range []string{"-url-file C:\\config", "https://example.test/", "marvia://a@host/sub/token"} {
		data, _ := windows.UTF16FromString(value)
		payload := copyData{kind: 1, size: uint32(len(data) * 2), data: uintptr(unsafe.Pointer(&data[0]))}
		got := instanceLink(&payload)
		if (got != "") != (value == "marvia://a@host/sub/token") {
			t.Fatal("IPC принял команду или потерял ссылку")
		}
		payload.size = maxInstanceLink + 2
		if instanceLink(&payload) != "" {
			t.Fatal("слишком большой запрос принят")
		}
		runtime.KeepAlive(data)
	}
}
