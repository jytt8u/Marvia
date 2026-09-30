package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// В файле только объёмы по часам, без сайтов, IP и ключа подписки.
type trafficDay struct {
	Day   string    `json:"day"`
	Up    int64     `json:"up"`
	Down  int64     `json:"down"`
	Hours [24]int64 `json:"hours"`
}

type trafficSnapshot struct {
	TodayUp   int64     `json:"today_up"`
	TodayDown int64     `json:"today_down"`
	Hours     [24]int64 `json:"hours"`
	RateUp    float64   `json:"rate_up"`
	RateDown  float64   `json:"rate_down"`
	PeakUp    float64   `json:"peak_up"`
	PeakDown  float64   `json:"peak_down"`
}

// Общие счётчики процесса не сбрасываются при подключении. Это позволяет
// сохранить весь расход даже при нескольких переподключениях между снятиями.
type trafficStats struct {
	mu                                 sync.Mutex
	day                                trafficDay
	path                               string
	lastUp, lastDown                   int64
	lastTime, saved                    time.Time
	rateUp, rateDown, peakUp, peakDown float64
}

func (s *trafficStats) load(path string, now time.Time) {
	s.path = path
	s.day.Day = now.Format("2006-01-02")
	info, err := os.Stat(path)
	if err != nil || info.Size() > 8<<10 {
		return
	}
	data, err := os.ReadFile(path)
	var day trafficDay
	if err != nil || json.Unmarshal(data, &day) != nil || day.Day != s.day.Day || day.Up < 0 || day.Down < 0 {
		return
	}
	for _, n := range day.Hours {
		if n < 0 {
			return
		}
	}
	s.day = day
}

func (s *trafficStats) sample(now time.Time, up, down int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sampleLocked(now, up, down)
}

func (s *trafficStats) sampleLocked(now time.Time, up, down int64) {
	if day := now.Format("2006-01-02"); day != s.day.Day {
		s.day = trafficDay{Day: day}
	}
	deltaUp, deltaDown := max(0, up-s.lastUp), max(0, down-s.lastDown)
	s.day.Up += deltaUp
	s.day.Down += deltaDown
	s.day.Hours[now.Hour()] += deltaUp + deltaDown
	s.rateUp, s.rateDown = 0, 0
	if dt := now.Sub(s.lastTime).Seconds(); !s.lastTime.IsZero() && dt > 0 && dt <= 30 {
		s.rateUp, s.rateDown = float64(deltaUp)/dt, float64(deltaDown)/dt
		s.peakUp, s.peakDown = max(s.peakUp, s.rateUp), max(s.peakDown, s.rateDown)
	}
	s.lastUp, s.lastDown, s.lastTime = up, down, now
	if now.Sub(s.saved) >= time.Minute {
		s.saveLocked(now)
	}
}

func (s *trafficStats) snapshot(now time.Time) trafficSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	day := s.day
	if day.Day != now.Format("2006-01-02") {
		day = trafficDay{}
	}
	return trafficSnapshot{TodayUp: day.Up, TodayDown: day.Down, Hours: day.Hours,
		RateUp: s.rateUp, RateDown: s.rateDown, PeakUp: s.peakUp, PeakDown: s.peakDown}
}

func (s *trafficStats) resetRates(now time.Time, up, down int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sampleLocked(now, up, down)
	s.rateUp, s.rateDown, s.peakUp, s.peakDown = 0, 0, 0, 0
}

func (s *trafficStats) flush(now time.Time, up, down int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sampleLocked(now, up, down)
	s.saveLocked(now)
}

func (s *trafficStats) saveLocked(now time.Time) {
	if s.path == "" {
		return
	}
	data, err := json.Marshal(s.day)
	if err != nil || os.MkdirAll(filepath.Dir(s.path), 0o700) != nil {
		return
	}
	temp := s.path + ".tmp"
	if os.WriteFile(temp, data, 0o600) != nil {
		return
	}
	if os.Rename(temp, s.path) != nil {
		_ = os.Remove(temp)
		return
	}
	s.saved = now
}
