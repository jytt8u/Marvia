package main

import "sort"

// Качество связи с текущей нодой: отклик, джиттер и потери вживую.
//
// Раньше «Отклик ноды» был одним числом с момента подключения: нода могла
// начать терять пакеты или тормозить, а окно продолжало показывать 32 мс.
// Теперь программа спрашивает ноду каждые несколько секунд тем же дешёвым
// пингом, что и надзор, и окно показывает последние минуты: что с откликом
// сейчас, ровный ли он и сколько ответов не дошло. Обещание «самого
// быстрого VPN» ничего не стоит — честные числа стоят.

// qualityKeep — сколько замеров помнить: при пинге раз в пять секунд это
// пять минут — достаточно, чтобы увидеть, что связь «поплыла», и не так
// много, чтобы старая беда висела на графике вечно.
const qualityKeep = 60

// QualityView — качество связи для окна.
type QualityView struct {
	// PingMS — медиана последних удачных замеров: одиночный всплеск не
	// должен перекрашивать число.
	PingMS int64 `json:"ping_ms"`
	// JitterMS — средний разброс между соседними удачными замерами.
	JitterMS int64 `json:"jitter_ms"`
	// LossPct — доля замеров без ответа, в процентах.
	LossPct int `json:"loss_pct"`
	// Samples — отклик по порядку, от старого к новому; 0 — ответа не было.
	Samples []int64 `json:"samples,omitempty"`
}

type qualityRing struct {
	samples []int64
}

// add записывает замер; ноль и меньше — ответа не было.
func (q *qualityRing) add(ms int64) {
	if ms < 0 {
		ms = 0
	}
	q.samples = append(q.samples, ms)
	if len(q.samples) > qualityKeep {
		q.samples = q.samples[len(q.samples)-qualityKeep:]
	}
}

func (q *qualityRing) reset() { q.samples = nil }

func (q *qualityRing) view() QualityView {
	if len(q.samples) == 0 {
		return QualityView{}
	}
	v := QualityView{Samples: append([]int64(nil), q.samples...)}
	var ok []int64
	lost, jitter, pairs := 0, int64(0), 0
	prev := int64(0)
	for _, s := range q.samples {
		if s <= 0 {
			lost++
			prev = 0
			continue
		}
		if prev > 0 {
			d := s - prev
			if d < 0 {
				d = -d
			}
			jitter += d
			pairs++
		}
		prev = s
		ok = append(ok, s)
	}
	v.LossPct = lost * 100 / len(q.samples)
	if pairs > 0 {
		v.JitterMS = jitter / int64(pairs)
	}
	if n := len(ok); n > 0 {
		recent := append([]int64(nil), ok[max(0, n-5):]...)
		sort.Slice(recent, func(i, j int) bool { return recent[i] < recent[j] })
		v.PingMS = recent[len(recent)/2]
	}
	return v
}
