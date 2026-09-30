package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDailyTrafficSurvivesRestartWithoutCountingOldBytesTwice(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "traffic.json")
	var first trafficStats
	first.load(path, now)
	first.sample(now, 100, 200)
	first.flush(now.Add(5*time.Second), 150, 400)
	var next trafficStats
	next.load(path, now)
	next.sample(now.Add(10*time.Second), 20, 30)
	got := next.snapshot(now)
	if got.TodayUp != 170 || got.TodayDown != 430 || got.Hours[12] != 600 {
		t.Fatalf("расход потерян или посчитан повторно: %+v", got)
	}
}

func TestTrafficChangesDayWithoutInventingRateAfterSleep(t *testing.T) {
	now := time.Date(2026, 9, 30, 23, 59, 50, 0, time.UTC)
	var stats trafficStats
	stats.sample(now, 0, 0)
	stats.sample(now.Add(5*time.Second), 100, 500)
	before := stats.snapshot(now)
	if before.RateDown != 100 || before.PeakDown != 100 {
		t.Fatalf("неверная скорость: %+v", before)
	}
	afterMidnight := now.Add(time.Minute)
	stats.sample(afterMidnight, 200, 900)
	got := stats.snapshot(afterMidnight)
	if got.TodayUp != 100 || got.TodayDown != 400 || got.Hours[0] != 500 || got.RateDown != 0 {
		t.Fatalf("смена суток или сон исказили расход: %+v", got)
	}
}

func TestReconnectKeepsDayTotalsAndResetsOnlySessionPeaks(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	var stats trafficStats
	stats.sample(now, 0, 0)
	stats.sample(now.Add(5*time.Second), 100, 1000)
	stats.resetRates(now.Add(6*time.Second), 150, 1200)
	got := stats.snapshot(now)
	if got.TodayDown != 1200 || got.TodayUp != 150 || got.PeakDown != 0 || got.RateDown != 0 {
		t.Fatalf("переподключение сбросило дневной расход: %+v", got)
	}
}
