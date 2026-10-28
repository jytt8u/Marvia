//go:build windows

package main

import (
	"reflect"
	"unsafe"

	"github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

// Сквозное окно — второй вид «Прозрачной»: под страницей не картинка обоев,
// а то, что действительно лежит за окном, — значки рабочего стола и другие
// окна. Цена — размытия нет: CSS размывает только то, что нарисовала сама
// страница, а рабочий стол рисует Windows. Поэтому обои с размытием остаются
// видом по умолчанию, а сквозной человек выбирает сам.
//
// Нужны три шага, и без любого из них окно остаётся сплошным:
//   - у WebView2 прозрачный фон по умолчанию — иначе под прозрачной
//     страницей белое. Переменная WEBVIEW2_DEFAULT_BACKGROUND_COLOR этого
//     не дала, нужен вызов у контроллера;
//   - окно отдаёт DWM свою альфу: DwmEnableBlurBehindWindow с пустой
//     областью ничего не размывает, а только включает прозрачность.
//     DwmExtendFrameIntoClientArea оставлял фон белым;
//   - окно стирает фон чёрной кистью: при blur-behind чёрное для DWM —
//     прозрачное, а без кисти под страницей остаётся белая подложка окна.

var (
	procDwmEnableBlurBehindWindow = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmEnableBlurBehindWindow")
	procCreateRectRgn             = windows.NewLazySystemDLL("gdi32.dll").NewProc("CreateRectRgn")
	procDeleteObject              = windows.NewLazySystemDLL("gdi32.dll").NewProc("DeleteObject")
	procGetStockObject            = windows.NewLazySystemDLL("gdi32.dll").NewProc("GetStockObject")
	procSetClassLongPtrW          = user32.NewProc("SetClassLongPtrW")
	procInvalidateRect            = user32.NewProc("InvalidateRect")
)

const (
	dwmBBEnable       = 0x1
	dwmBBBlurRegion   = 0x2
	gclpHbrBackground = ^uintptr(9) // GCLP_HBRBACKGROUND, -10
	blackBrush        = 4
)

type dwmBlurBehind struct {
	flags      uint32
	enable     int32
	region     uintptr
	transition int32
}

// chromiumOf достаёт движок из окна go-webview2. Библиотека его не отдаёт,
// а фон по умолчанию задаётся только у контроллера, поэтому — через
// неэкспортированное поле. Тест TestWebViewStillHasItsEngineWhereWeReachForIt
// сторожит это место: обновление библиотеки, переименовавшее поле, иначе
// молча вернуло бы белый фон под «сквозным» окном.
func chromiumOf(w webview2.WebView) (ch *edge.Chromium) {
	defer func() {
		if recover() != nil {
			ch = nil
		}
	}()
	v := reflect.ValueOf(w)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return nil
	}
	f := v.Elem().FieldByName("browser")
	if !f.IsValid() {
		return nil
	}
	got, _ := reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Interface().(*edge.Chromium)
	return got
}

// seeThrough включает и выключает сквозное окно. Виджет всегда сплошной:
// маленькая плашка в углу поверх чужих окон без фона не читается.
func (s *shell) seeThrough(on bool) {
	on = on && !s.widget
	if on == s.through {
		return
	}
	ch := chromiumOf(s.w)
	if ch == nil || ch.GetController() == nil {
		s.log.add("сквозное окно не включилось: движок окна недоступен")
		return
	}
	ctl := ch.GetController().GetICoreWebView2Controller2()
	if ctl == nil {
		s.log.add("сквозное окно не включилось: WebView2 слишком старый")
		return
	}
	// Сплошным окно возвращается к белому фону движка — как до включения:
	// страница всё равно закрашивает его своим.
	color := edge.COREWEBVIEW2_COLOR{A: 255, R: 255, G: 255, B: 255}
	bb := dwmBlurBehind{flags: dwmBBEnable}
	// Сплошному окну кисть не нужна, как и было: фон закрашивает страница.
	var brush uintptr
	if on {
		color = edge.COREWEBVIEW2_COLOR{}
		rgn, _, _ := procCreateRectRgn.Call(0, 0, ^uintptr(0), ^uintptr(0))
		defer procDeleteObject.Call(rgn)
		bb = dwmBlurBehind{flags: dwmBBEnable | dwmBBBlurRegion, enable: 1, region: rgn}
		brush, _, _ = procGetStockObject.Call(blackBrush)
	}
	if err := ctl.PutDefaultBackgroundColor(color); err != nil {
		s.log.add("сквозное окно: фон движка не сменился: %v", err)
		return
	}
	if r, _, _ := procDwmEnableBlurBehindWindow.Call(s.hwnd, uintptr(unsafe.Pointer(&bb))); r != 0 {
		s.log.add("сквозное окно: DWM отказал, код %#x", r)
	}
	_, _, _ = procSetClassLongPtrW.Call(s.hwnd, gclpHbrBackground, brush)
	_, _, _ = procInvalidateRect.Call(s.hwnd, 0, 1)
	s.through = on
}
