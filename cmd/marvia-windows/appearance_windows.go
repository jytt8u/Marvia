//go:build windows

package main

import (
	"fmt"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procLoadImageW            = user32.NewProc("LoadImageW")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procDwmSetWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
)

type captionTheme struct {
	background uint32
	foreground uint32
	dark       int32
}

func captionColors(background, foreground string) (captionTheme, error) {
	decode := func(value string) (uint32, error) {
		if len(value) != 7 || value[0] != '#' {
			return 0, fmt.Errorf("ожидался цвет #RRGGBB")
		}
		for _, c := range value[1:] {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return 0, fmt.Errorf("ожидался цвет #RRGGBB")
			}
		}
		v, err := strconv.ParseUint(value[1:], 16, 24)
		if err != nil {
			return 0, fmt.Errorf("цвет не разобрался: %w", err)
		}
		// COLORREF хранит каналы в обратном порядке относительно CSS.
		return uint32(v>>16) | uint32(v&0xff00) | uint32(v&0xff)<<16, nil
	}
	bg, err := decode(background)
	if err != nil {
		return captionTheme{}, err
	}
	fg, err := decode(foreground)
	if err != nil {
		return captionTheme{}, err
	}
	var dark int32
	if 299*(bg&255)+587*((bg>>8)&255)+114*((bg>>16)&255) < 128000 {
		dark = 1
	}
	return captionTheme{bg, fg, dark}, nil
}

// Иконка файла и окна — из одного ресурса. PNG оставляем запасным путём для
// локального go test/build без генерации ресурсов: окно всё равно узнаваемо.
func applicationIcon(size int) (uintptr, error) {
	inst, _, _ := procGetModuleHandleW.Call(0)
	icon, _, _ := procLoadImageW.Call(inst, 1, 1, uintptr(size), uintptr(size), 0)
	if icon == 0 {
		icon, _, _ = procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&trayPNG[0])), uintptr(len(trayPNG)), 1, 0x30000, uintptr(size), uintptr(size), 0)
	}
	if icon == 0 {
		return 0, fmt.Errorf("не удалось загрузить логотип приложения")
	}
	return icon, nil
}

func (s *shell) setWindowIcons() {
	for index, size := range []int{16, 32} {
		icon, err := applicationIcon(size)
		if err != nil {
			s.log.add("иконка окна: %v", err)
			continue
		}
		s.icons[index] = icon
		_, _, _ = procSendMessageW.Call(s.hwnd, 0x0080, uintptr(index), icon) // WM_SETICON
	}
}

func (s *shell) releaseWindowIcons() {
	for _, icon := range s.icons {
		if icon != 0 {
			_, _, _ = procDestroyIcon.Call(icon)
		}
	}
}

// Системные кнопки и изменение размера остаются у Windows. Цвет заголовка
// приходит из темы страницы; старые Windows игнорируют неподдержанные атрибуты.
func (s *shell) applyCaption(theme captionTheme) (modeSupported, colorSupported bool) {
	s.caption = theme
	set := func(attribute uintptr, value unsafe.Pointer) uintptr {
		r, _, _ := procDwmSetWindowAttribute.Call(s.hwnd, attribute, uintptr(value), 4)
		return r
	}
	modeResult := set(20, unsafe.Pointer(&theme.dark))
	if modeResult != 0 {
		modeResult = set(19, unsafe.Pointer(&theme.dark))
	}
	colorResult := set(35, unsafe.Pointer(&theme.background))
	set(36, unsafe.Pointer(&theme.foreground))
	set(34, unsafe.Pointer(&theme.background))
	return modeResult == 0, colorResult == 0
}
