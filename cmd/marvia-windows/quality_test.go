package main

import "testing"

// TestQualityTellsJitterAndLossNotJustPing: два пропуска из десяти — 20 %
// потерь, а скачущий отклик — джиттер; одиночный всплеск не перекрашивает
// число отклика.
func TestQualityTellsJitterAndLossNotJustPing(t *testing.T) {
	var q qualityRing
	for _, ms := range []int64{30, 32, 0, 31, 30, 33, 0, 30, 31, 400} {
		q.add(ms)
	}
	v := q.view()
	if v.LossPct != 20 {
		t.Fatalf("потери %d%%, ожидалось 20", v.LossPct)
	}
	if v.PingMS < 30 || v.PingMS > 33 {
		t.Fatalf("отклик %d мс: всплеск в 400 перекрасил число", v.PingMS)
	}
	if v.JitterMS <= 0 {
		t.Fatal("скачущий отклик без джиттера")
	}
	if len(v.Samples) != 10 || v.Samples[2] != 0 {
		t.Fatalf("история: %v", v.Samples)
	}
}

// TestQualityForgetsTheDistantPast: кольцо держит последние пять минут, а не
// всю сессию — давняя беда не висит на графике вечно.
func TestQualityForgetsTheDistantPast(t *testing.T) {
	var q qualityRing
	for range qualityKeep {
		q.add(0)
	}
	for range qualityKeep {
		q.add(40)
	}
	if v := q.view(); v.LossPct != 0 || len(v.Samples) != qualityKeep {
		t.Fatalf("старые потери не ушли: %+v", v)
	}
}
