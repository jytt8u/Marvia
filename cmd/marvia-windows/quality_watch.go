//go:build windows

package main

import (
	"context"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

// qualityEvery — как часто спрашивать текущую ноду. Пять секунд: график
// успевает показать, что связь «поплыла», а лишних пакетов к ноде — один
// служебный кадр раз в пять секунд.
var qualityEvery = 5 * time.Second

// watchQuality пингует текущую ноду, пока жив туннель, и пишет отклик в
// кольцо качества. Пинг через ядро — тот же, что у надзора: он не открывает
// новых соединений и не будит переезд сам по себе.
func (c *Controller) watchQuality(ctx context.Context, d client.Backend) {
	t := time.NewTicker(qualityEvery)
	defer t.Stop()
	for {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		rtt, err := d.Ping(pctx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		ms := int64(0)
		if err == nil {
			ms = max(1, rtt.Milliseconds())
		}
		c.mu.Lock()
		if c.dialer == d {
			c.quality.add(ms)
		}
		c.mu.Unlock()
		select {
		case <-t.C:
		case <-ctx.Done():
			return
		}
	}
}
