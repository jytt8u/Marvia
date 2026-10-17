//go:build windows

package main

import (
	"encoding/json"
	"os"
	"slices"
	"sync"
)

// Помехи на время подключения.
//
// Окно, которое только называет мешающую программу, оставляет человека с
// командой PowerShell на руках, а приходят к нам ровно те, кто её не наберёт.
// Поэтому то, что можно остановить и потом вернуть как было, Marvia
// останавливает сама: службу zapret или GoodbyeDPI и подключение WARP. После
// отключения всё возвращается — без VPN эти программы человеку нужны, и
// оставить его без YouTube только потому, что он однажды нажал «Подключиться»,
// было бы хуже самой помехи.
//
// Чужие программы целиком сами не закрываем: Hiddify, второй VPN-клиент,
// winws, запущенный из батника, вернуть в прежнее состояние нельзя. Их
// закрывает кнопка «Починить» — по нажатию человека, см. Controller.Fix.

// held — что мы остановили и обязаны вернуть.
type held struct {
	Services []string `json:"services,omitempty"`
	WARP     bool     `json:"warp,omitempty"`
}

func (h held) empty() bool { return len(h.Services) == 0 && !h.WARP }

// pausableServices — службы средств обхода замедления, как их называют
// установщики: zapret-discord-youtube ставит «zapret», zapret от bol-van —
// winws1 и winws2, GoodbyeDPI — свою. Чужие службы по шаблону имени не
// останавливаем: ошибиться в чужой системе дороже, чем не узнать новую.
var pausableServices = []string{"zapret", "winws1", "winws2", "GoodbyeDPI"}

// Ручки системы; подменяются в тестах.
var (
	serviceRunning = windowsServiceRunning
	stopService    = windowsStopService
	startService   = windowsStartService
	warpConnected  = cliWarpConnected
	setWarp        = cliSetWarp
)

// pauser останавливает помехи и возвращает их.
//
// Что остановлено, лежит в файле, а не только в памяти: упади программа или
// выключись компьютер посреди подключения, zapret так и остался бы
// выключенным, а человек — без YouTube и без понимания почему. Следующий
// запуск находит файл и возвращает всё на место.
type pauser struct {
	mu   sync.Mutex
	path string
	held held
	log  func(string)
}

func newPauser(path string, log func(string)) *pauser {
	p := &pauser{path: path, log: log}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &p.held)
	}
	return p
}

// pending — осталось ли что-то от прошлого запуска.
func (p *pauser) pending() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.held.empty()
}

// pause останавливает то, что работает сейчас. Повторный вызов не трогает
// уже остановленное нами: иначе переподключение запомнило бы службу
// остановленной «до нас» и не вернуло бы её.
func (p *pauser) pause() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, name := range pausableServices {
		if slices.Contains(p.held.Services, name) || !serviceRunning(name) {
			continue
		}
		if err := stopService(name); err != nil {
			p.log(sayf("logPauseFailed", name, err))
			continue
		}
		p.held.Services = append(p.held.Services, name)
		p.save()
		p.log(sayf("logPaused", name))
	}
	if !p.held.WARP && warpConnected() {
		if err := setWarp(false); err != nil {
			p.log(sayf("logPauseFailed", "Cloudflare WARP", err))
		} else {
			p.held.WARP = true
			p.save()
			p.log(sayf("logPaused", "Cloudflare WARP"))
		}
	}
}

// restore возвращает всё, что остановили мы, и только это: службу, которую
// человек выключил сам ещё до Marvia, запускать не наше дело.
func (p *pauser) restore() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.held.empty() {
		return
	}
	for _, name := range p.held.Services {
		if err := startService(name); err != nil {
			p.log(sayf("logResumeFailed", name, err))
			continue
		}
		p.log(sayf("logResumed", name))
	}
	if p.held.WARP {
		if err := setWarp(true); err != nil {
			p.log(sayf("logResumeFailed", "Cloudflare WARP", err))
		} else {
			p.log(sayf("logResumed", "Cloudflare WARP"))
		}
	}
	// Не вернувшееся не держим: повторять запуск на каждом отключении —
	// значит засыпать журнал одной и той же ошибкой.
	p.held = held{}
	_ = os.Remove(p.path)
}

func (p *pauser) save() {
	if p.path == "" {
		return
	}
	raw, err := json.Marshal(p.held)
	if err == nil {
		_ = os.WriteFile(p.path, raw, 0o600)
	}
}
