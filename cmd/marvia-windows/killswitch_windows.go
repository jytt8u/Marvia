//go:build windows

package main

import "github.com/jytt8u/marvia/internal/killswitch"

// Kill switch на компьютере: что пропускается и почему — в
// internal/killswitch, здесь — когда он стоит.
//
// Стоит от подъёма адаптера до «Отключить», и ничто, кроме «Отключить», его
// не снимает: замолчала нода, пропал адаптер, переезд не удался — фильтр на
// месте, и трафик мимо туннеля не идёт. Пока подключение не поднималось ни
// разу, его нет: человек видит «не подключено» и знает, что идёт напрямую.
// Падение самого Marvia снимает фильтр вместе с процессом — см. Guard.

// raiseGuard ставит фильтр для поднятого адаптера, если человек его включил.
//
// Не встал — туннель всё равно поднимается, а окно говорит, что защиты нет.
// Отказать в подключении было бы хуже: без туннеля весь трафик и так идёт
// напрямую, то есть ровно то, от чего kill switch защищает.
func (c *Controller) raiseGuard(luid uint64, on bool) (*killswitch.Guard, string) {
	if !on {
		return nil, ""
	}
	g, err := killswitch.Start(killswitch.Plan(killswitch.Config{Tunnel: luid, Home: c.lan.Load()}))
	if err != nil {
		c.log.add("%s", sayf("logKillFailed", err))
		return nil, err.Error()
	}
	c.log.add("%s", say("logKillOn"))
	return g, ""
}

// reguard переставляет фильтр после правки настроек при поднятом туннеле:
// включение, выключение и домашняя сеть действуют сразу, без переподключения.
//
// Новый набор встаёт раньше, чем снимается старый, — мгновения без запрета
// между ними нет. Новый не встал — старый остаётся: прежние правила лучше,
// чем никаких. Под tunnelMu: иначе правка, пришедшая посреди «Отключить»,
// поставила бы фильтр уже после уборки, и компьютер остался бы без сети до
// выхода из программы.
func (c *Controller) reguard() {
	c.tunnelMu.Lock()
	defer c.tunnelMu.Unlock()
	c.mu.Lock()
	adapter, old, on := c.adapter, c.guard, c.settings.KillSwitch
	c.mu.Unlock()
	if adapter == nil {
		return
	}
	if !on {
		c.mu.Lock()
		c.guard, c.guardErr = nil, ""
		c.mu.Unlock()
		if old != nil {
			_ = old.Close()
			c.log.add("%s", say("logKillOff"))
		}
		return
	}
	fresh, reason := c.raiseGuard(adapter.LUID(), true)
	c.mu.Lock()
	c.guardErr = reason
	if fresh != nil {
		c.guard = fresh
	}
	c.mu.Unlock()
	if fresh != nil && old != nil {
		_ = old.Close()
	}
}
