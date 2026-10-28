//go:build windows

package main

import (
	"net/http/httptest"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestTrayCanReceiveExplorerRestartBroadcast(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	icon := testTray(t)
	defer icon.Remove()
	className, _ := syscall.UTF16PtrFromString("MarviaTray")
	messageWindow, _, _ := user32.NewProc("FindWindowExW").Call(hwndMessage, 0, uintptr(unsafe.Pointer(className)), 0)
	if messageWindow == icon.hwnd {
		t.Fatal("окно трея не получает широковещательные сообщения Проводника")
	}
}

func TestWindowKeepsLogoAndUsesBothLightAndDarkCaption(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	icon := testTray(t)
	s := &shell{hwnd: icon.hwnd, log: newJournal()}
	defer func() { icon.Remove(); s.releaseWindowIcons() }()
	s.setWindowIcons()
	for index := range s.icons {
		got, _, _ := procSendMessageW.Call(s.hwnd, 0x007f, uintptr(index), 0)
		if got == 0 || got != s.icons[index] {
			t.Fatalf("в окне нет иконки размера %d", index)
		}
	}
	for _, colors := range [][2]string{{"#0c0e11", "#f1f4f7"}, {"#f3f5f7", "#0d1013"}} {
		theme, err := captionColors(colors[0], colors[1])
		if err != nil {
			t.Fatal(err)
		}
		modeSupported, colorSupported := s.applyCaption(theme)
		if !modeSupported {
			t.Fatal("Windows не приняла режим заголовка")
		}
		if !colorSupported {
			t.Log("Windows не поддерживает отдельный цвет заголовка")
		}
		if s.caption != theme {
			t.Fatal("тема окна не сохранилась для возврата из виджета")
		}
	}
}

func testTray(t *testing.T) *tray {
	t.Helper()
	className, _ := syscall.UTF16PtrFromString("Shell_TrayWnd")
	explorer, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(className)), 0)
	if explorer == 0 {
		t.Skip("в сеансе CI нет Проводника с системным треем")
	}
	icon, err := newTray()
	if err != nil {
		t.Fatal(err)
	}
	return icon
}

func TestTrayReturnsAfterExplorerRemovesNotificationIcons(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	icon := testTray(t)
	defer icon.Remove()
	if icon.taskbarCreated == 0 {
		t.Fatal("сообщение Проводника не зарегистрировано")
	}
	// Удаляем только наш тестовый значок: настоящий Проводник не перезапускаем.
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&icon.data)))
	icon.wndProc(icon.hwnd, icon.taskbarCreated, 0, 0)
	type identifier struct {
		size uint32
		hwnd uintptr
		id   uint32
		guid [16]byte
	}
	id := identifier{hwnd: icon.hwnd, id: icon.data.ID}
	id.size = uint32(unsafe.Sizeof(id))
	var bounds rect
	result, _, _ := shell32.NewProc("Shell_NotifyIconGetRect").Call(uintptr(unsafe.Pointer(&id)), uintptr(unsafe.Pointer(&bounds)))
	if result != 0 {
		t.Fatalf("Проводник не видит восстановленный значок: %x", result)
	}
}

func TestPageThemeReachesNativeWindowAndRejectsBrokenColors(t *testing.T) {
	var received windowRequest
	callback := func(request windowRequest) { received = request }
	app := &ui{onWindow: &callback}
	response := httptest.NewRecorder()
	app.window(response, httptest.NewRequest("POST", "/api/window", strings.NewReader(`{"mode":"theme","background":"#123456","foreground":"#eeeeee"}`)))
	if response.Code != 200 || received.Background != "#123456" {
		t.Fatal("тема страницы не дошла до окна")
	}
	response = httptest.NewRecorder()
	app.window(response, httptest.NewRequest("POST", "/api/window", strings.NewReader(`{"mode":"theme","background":"url(bad)","foreground":"#eeeeee"}`)))
	if response.Code != 400 || received.Background != "#123456" {
		t.Fatal("сломанный цвет изменил оформление окна")
	}
}

func TestCaptionPreservesColorChannels(t *testing.T) {
	theme, err := captionColors("#123456", "#abcdef")
	if err != nil || theme.background != 0x563412 || theme.foreground != 0xefcdab || theme.dark != 1 {
		t.Fatalf("неверные каналы или яркость: %+v, %v", theme, err)
	}
	for _, bad := range []string{"", "#123", "#+12345", "#gggggg", "#1234567"} {
		if _, err := captionColors(bad, "#eeeeee"); err == nil {
			t.Fatalf("неверный цвет принят: %q", bad)
		}
	}
}
