//go:build windows

package main

import (
	"encoding/json"
	"syscall"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
)

var createWebView = webview2.NewWithOptions

var (
	procSetWindowLongPtrW    = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW      = user32.NewProc("CallWindowProcW")
	procSetWindowPos         = user32.NewProc("SetWindowPos")
	procShowWindow           = user32.NewProc("ShowWindow")
	procIsWindowVisible      = user32.NewProc("IsWindowVisible")
	procSystemParametersInfo = user32.NewProc("SystemParametersInfoW")
	procPostQuitMessage      = user32.NewProc("PostQuitMessage")
)

const (
	// Индексы для SetWindowLongPtr: отрицательные, в регистр уходят как 32-битные.
	gwlStyle    = uintptr(0xFFFFFFF0) // GWL_STYLE = -16
	gwlpWndProc = uintptr(0xFFFFFFFC) // GWLP_WNDPROC = -4

	wsOverlappedWindow = 0x00CF0000
	wsPopup            = 0x80000000
	wsVisible          = 0x10000000

	swHide = 0
	swShow = 5

	swpNoZOrder     = 0x0004
	swpFrameChanged = 0x0020
	swpNoActivate   = 0x0010

	spiGetWorkArea = 0x0030

	// Размеры из макета: полное окно 1180×720 и трей-виджет 380×560.
	fullWidth    = 1180
	fullHeight   = 720
	widgetWidth  = 400
	widgetHeight = 640
	widgetGap    = 12
)

type rect struct{ Left, Top, Right, Bottom int32 }

// shell — окно программы и значок в трее.
//
// Окно одно, но у него два вида. Полное — как раньше, с рамкой и вкладками.
// Виджет — макет D01: 380×560 без рамки у трея, кнопка, состояние, нода и
// три цифры; открывается по значку и прячется, как только человек нажал
// куда-то ещё. Второго окна с движком нет намеренно: два WebView2 — это два
// набора памяти ради двух шкур одной страницы.
//
// Крестик и «закрыть» больше не выключают программу: окно уходит в трей, а
// туннель живёт. Выход — только из меню значка.
type shell struct {
	w       webview2.WebView
	hwnd    uintptr
	url     string
	widget  bool
	oldProc uintptr
	tray    *tray
	ctl     *Controller
	log     *journal
	icons   [2]uintptr
	caption captionTheme
	loaded  bool
}

// showWindow открывает окно программы и держит её до выхода из меню трея.
//
// Внутри окна — движок WebView2, который в Windows 11 встроен, а в десятке
// доезжает с обновлениями. Своего набора виджетов мы не тащим: он потребовал
// бы компилятора C, весил бы больше самого ядра и выглядел бы одинаково чужим
// на всех системах.
//
// Если окно не создалось, возвращаем ошибку и освобождаем VPN через run.
// Браузер не заменяет трей: у оконного приложения нет консоли для Ctrl+C.
// hidden — начать в трее, без окна: так программа стартует вместе с Windows.
func showWindow(url string, ctl *Controller, log *journal, hidden bool, onWindow *func(windowRequest)) error {
	w := createWebView(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			IconId: 1,
			Title:  "Marvia",
			Width:  fullWidth,
			Height: fullHeight,
			Center: true,
		},
	})
	if w == nil {
		return errWebViewWindow
	}
	s := &shell{w: w, hwnd: uintptr(w.Window()), url: url, ctl: ctl, log: log}
	defer func() { w.Destroy(); s.releaseWindowIcons() }()
	s.setWindowIcons()
	initialTheme, _ := captionColors("#0c0e11", "#f1f4f7")
	s.applyCaption(initialTheme)
	s.subclass()

	t, err := newTray()
	if err != nil {
		// Без значка программа теряет способ выйти из трея, поэтому ведём
		// себя как раньше: крестик выключает. Об этом — в журнал.
		log.add("значок в трее не встал: %v — окно закрывается вместе с туннелем", err)
	} else {
		s.tray = t
		t.onClick = func() { s.toggleWidget() }
		t.onShow = func() { s.showFull("") }
		t.onLink = func(link string) {
			w.Dispatch(func() {
				s.showFull("settings")
				old := ctl.Account()
				if old == link {
					return
				}
				if old != "" && !confirm(say("replaceKeyTitle"), say("replaceKeyBody")) {
					return
				}
				if err := ctl.SetAccount(link); err != nil {
					log.add("ссылка не подошла: %v", err)
				}
			})
		}
		t.onMenu = s.menu
		t.menuState = func() (bool, bool) {
			st := ctl.Status()
			return st.State == StateConnected || st.State == StateConnecting || st.State == StateStalled, st.HasAccount
		}
		defer t.Remove()

		// Напоминание о сроке — уведомлением, пока программа в трее. Первый раз
		// через минуту после запуска: подписка к тому времени уже прочитана из
		// кэша или пришла с панели. Дальше раз в полчаса — срок меряется днями,
		// и чаще проверять незачем.
		stopReminders := make(chan struct{})
		defer close(stopReminders)
		go func() {
			wait := time.Minute
			for {
				select {
				case <-stopReminders:
					return
				case <-time.After(wait):
				}
				wait = 30 * time.Minute
				if title, text, ok := ctl.TrayReminder(time.Now()); ok {
					w.Dispatch(func() { t.Balloon(title, text) })
				}
			}
		}()
	}

	// Страница просит переключить вид: из виджета — в полное окно на нужную
	// вкладку, крестиком виджета — спрятаться. Приходит из обработчика HTTP,
	// то есть из другого потока, а окно трогать можно только из своего.
	*onWindow = func(request windowRequest) {
		w.Dispatch(func() {
			switch request.Mode {
			case "full":
				s.showFull(request.Tab)
			case "hide":
				s.hide()
			case "theme":
				if theme, err := captionColors(request.Background, request.Foreground); err == nil {
					s.applyCaption(theme)
				}
			}
		})
	}

	if hidden && s.tray != nil {
		w.Navigate(url)
		s.loaded = true
		s.hide()
	} else {
		s.showFull("")
	}

	w.Run()
	return nil
}

// subclass перехватывает сообщения окна: крестик прячет, а не разрушает,
// потеря фокуса прячет виджет.
func (s *shell) subclass() {
	proc := syscall.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
		switch msg {
		case wmClose:
			if s.tray != nil {
				s.hide()
				return 0
			}
		case wmActivate:
			if s.widget && wp&0xffff == waInactive {
				s.hide()
			}
		}
		r, _, _ := procCallWindowProcW.Call(s.oldProc, hwnd, msg, wp, lp)
		return r
	})
	s.oldProc, _, _ = procSetWindowLongPtrW.Call(s.hwnd, gwlpWndProc, proc)
}

func (s *shell) hide() {
	_, _, _ = procShowWindow.Call(s.hwnd, swHide)
}

func (s *shell) visible() bool {
	r, _, _ := procIsWindowVisible.Call(s.hwnd)
	return r != 0
}

// showFull показывает полное окно; tab — какую вкладку открыть.
func (s *shell) showFull(tab string) {
	s.widget = false
	_, _, _ = procSetWindowLongPtrW.Call(s.hwnd, gwlStyle, wsOverlappedWindow|wsVisible)
	var wa rect
	_, _, _ = procSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0)
	x := int(wa.Left) + (int(wa.Right-wa.Left)-fullWidth)/2
	y := int(wa.Top) + (int(wa.Bottom-wa.Top)-fullHeight)/2
	_, _, _ = procSetWindowPos.Call(s.hwnd, 0, uintptr(x), uintptr(y), fullWidth, fullHeight, swpNoZOrder|swpFrameChanged)
	s.applyCaption(s.caption)
	s.navigateView("full", tab)
	_, _, _ = procShowWindow.Call(s.hwnd, swShow)
	_, _, _ = procSetForegroundWindow.Call(s.hwnd)
}

// toggleWidget — нажатие на значок: виджет показать или спрятать.
func (s *shell) toggleWidget() {
	if s.visible() {
		s.hide()
		return
	}
	s.widget = true
	_, _, _ = procSetWindowLongPtrW.Call(s.hwnd, gwlStyle, wsPopup|wsVisible)
	// В угол рабочей области — туда, где трей. Панель задач может стоять и
	// сбоку, и сверху; рабочая область это учитывает, а угол справа внизу
	// остаётся ближайшим к значку в подавляющем числе случаев.
	var wa rect
	_, _, _ = procSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0)
	dpi, _, _ := user32.NewProc("GetDpiForWindow").Call(s.hwnd)
	if dpi == 0 {
		dpi = 96
	}
	width := min(widgetWidth*int(dpi)/96, int(wa.Right-wa.Left)-widgetGap*2)
	height := min(widgetHeight*int(dpi)/96, int(wa.Bottom-wa.Top)-widgetGap*2)
	x := int(wa.Right) - width - widgetGap
	y := int(wa.Bottom) - height - widgetGap
	_, _, _ = procSetWindowPos.Call(s.hwnd, 0, uintptr(x), uintptr(y), uintptr(width), uintptr(height), swpNoZOrder|swpFrameChanged)
	s.navigateView("widget", "")
	_, _, _ = procShowWindow.Call(s.hwnd, swShow)
	_, _, _ = procSetForegroundWindow.Call(s.hwnd)
}

// Переключение трея не создаёт новую страницу: остаются форма ключа,
// графики и текущие отсчёты скорости, а WebView не загружает шрифты заново.
func (s *shell) navigateView(mode, tab string) {
	if !s.loaded {
		page := s.url
		if mode == "widget" {
			page += "?mode=widget"
		} else if tab != "" {
			page += "?tab=" + tab
		}
		s.w.Navigate(page)
		s.loaded = true
		return
	}
	view, _ := json.Marshal(map[string]string{"mode": mode, "tab": tab})
	s.w.Eval("window.marviaView && window.marviaView(" + string(view) + ")")
}

// menu исполняет выбор из меню значка.
func (s *shell) menu(id int) {
	switch id {
	case menuOpen:
		s.showFull("")
	case menuConnect:
		go func() {
			if err := s.ctl.Connect(); err != nil {
				s.log.add("из трея: %v", err)
			}
		}()
	case menuDisconnect:
		go s.ctl.Disconnect()
	case menuExit:
		s.ctl.Disconnect()
		if s.tray != nil {
			s.tray.Remove()
			s.tray = nil
		}
		_, _, _ = procPostQuitMessage.Call(0)
	}
}
