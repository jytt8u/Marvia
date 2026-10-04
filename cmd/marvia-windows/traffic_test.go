package main

import (
	"os"
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

// Неделя — ровно семь суток по порядку, с нулями в днях без туннеля, и она
// переживает перезапуск.
func TestWeekShowsSevenDaysInOrderWithEmptyDaysAndSurvivesRestart(t *testing.T) {
	start := time.Date(2026, 9, 25, 12, 0, 0, 0, time.Local)
	path := filepath.Join(t.TempDir(), "traffic.json")
	var stats trafficStats
	stats.load(path, start)
	var up, down int64
	for _, day := range []int{0, 1, 3, 6} {
		at := start.AddDate(0, 0, day)
		stats.sample(at, up, down)
		up, down = up+10*int64(day+1), down+100*int64(day+1)
		stats.flush(at.Add(5*time.Second), up, down)
	}
	now := start.AddDate(0, 0, 6)
	var restarted trafficStats
	restarted.load(path, now)
	week := restarted.snapshot(now).Week
	if len(week) != 7 || week[0].Day != "2026-09-25" || week[6].Day != "2026-10-01" {
		t.Fatalf("неделя: %+v", week)
	}
	want := []int64{100, 200, 0, 400, 0, 0, 700}
	for i, w := range want {
		if week[i].Down != w {
			t.Errorf("день %s: приём %d, ожидалось %d", week[i].Day, week[i].Down, w)
		}
	}
}

// Книга держит месяц: тридцать первые сутки вытесняют самые старые.
func TestTrafficBookKeepsAMonth(t *testing.T) {
	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.Local)
	var stats trafficStats
	for i := 0; i < 40; i++ {
		stats.sample(start.AddDate(0, 0, i), int64(i), int64(i))
	}
	if len(stats.days) != trafficKeepDays || stats.days[0].Day != start.AddDate(0, 0, 10).Format("2006-01-02") {
		t.Errorf("в книге %d суток, первые %s", len(stats.days), stats.days[0].Day)
	}
}

// Файл прежней версии — одни сутки объектом — читается как книга из одного
// дня, а испорченные и будущие записи выбрасываются.
func TestOldOneDayFileStillReads(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local)
	path := filepath.Join(t.TempDir(), "traffic.json")
	if err := os.WriteFile(path, []byte(`{"day":"2026-09-29","up":5,"down":7,"hours":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var old trafficStats
	old.load(path, now)
	if week := old.snapshot(now).Week; week[5].Down != 7 || week[5].Up != 5 {
		t.Errorf("вчера из старого файла: %+v", week[5])
	}
	bad := `{"days":[{"day":"2026-09-28","up":-1},{"day":"2026-10-05","up":9},{"day":"вчера","up":3},{"day":"2026-09-29","up":4}]}`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	var fixed trafficStats
	fixed.load(path, now)
	if len(fixed.days) != 1 || fixed.days[0].Up != 4 {
		t.Errorf("после чистки: %+v", fixed.days)
	}
}
