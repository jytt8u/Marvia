package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// В файле только объёмы по часам, без сайтов, IP и ключа подписки — и только
// на этом компьютере: панель продавца подённой истории покупателя не ведёт и
// вести не должна, как и на телефоне.
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

	// Week — последние семь суток, от старых к сегодняшним, с нулями там, где
	// туннель не поднимался: пропуск в графике читался бы как «этого дня не
	// было», а он был, просто без расхода.
	Week []trafficDayTotal `json:"week"`
}

// trafficDayTotal — итог одних суток для недельного графика.
type trafficDayTotal struct {
	Day  string `json:"day"`
	Up   int64  `json:"up"`
	Down int64  `json:"down"`
}

// trafficKeepDays — сколько суток хранится. Тридцать, как на телефоне:
// неделя видна в окне, а месяц — запас на тот день, когда появится и он, и
// на вопрос продавцу «почему кончилась квота». Больше — уже не учёт, а
// история, которую незачем копить.
const trafficKeepDays = 30

// weekDays — сколько суток в недельном графике.
const weekDays = 7

// Общие счётчики процесса не сбрасываются при подключении. Это позволяет
// сохранить весь расход даже при нескольких переподключениях между снятиями.
//
// Книга по суткам, по местному времени компьютера, как на телефоне: «вчера»
// должно совпадать с человеческим вчера. Сутки идут по возрастанию, последние
// — текущие.
type trafficStats struct {
	mu                                 sync.Mutex
	days                               []trafficDay
	path                               string
	lastUp, lastDown                   int64
	lastTime, saved                    time.Time
	rateUp, rateDown, peakUp, peakDown float64
}

// trafficFile — книга на диске. Раньше файл держал одни сутки объектом
// trafficDay; такой файл читается как книга из одного дня.
type trafficFile struct {
	Days []trafficDay `json:"days"`
}

func (s *trafficStats) load(path string, now time.Time) {
	s.path = path
	info, err := os.Stat(path)
	if err != nil || info.Size() > 64<<10 {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var book trafficFile
	if json.Unmarshal(data, &book) != nil || book.Days == nil {
		var single trafficDay
		if json.Unmarshal(data, &single) != nil || single.Day == "" {
			return
		}
		book.Days = []trafficDay{single}
	}
	// Испорченная запись — отрицательные числа, неразборчивая дата, сутки из
	// будущего после перевода часов — выбрасывается целиком: врать в графике
	// хуже, чем показать день пустым.
	today := now.Format("2006-01-02")
	oldest := now.AddDate(0, 0, -(trafficKeepDays - 1)).Format("2006-01-02")
	for _, day := range book.Days {
		if _, err := time.Parse("2006-01-02", day.Day); err != nil || day.Day > today || day.Day < oldest || !validDay(day) {
			continue
		}
		if n := len(s.days); n > 0 && s.days[n-1].Day >= day.Day {
			continue
		}
		s.days = append(s.days, day)
	}
}

func validDay(day trafficDay) bool {
	if day.Up < 0 || day.Down < 0 {
		return false
	}
	for _, n := range day.Hours {
		if n < 0 {
			return false
		}
	}
	return true
}

// current — запись текущих суток; заводит её, если сутки сменились, и
// забывает то, что старше месяца.
func (s *trafficStats) current(now time.Time) *trafficDay {
	day := now.Format("2006-01-02")
	if n := len(s.days); n == 0 || s.days[n-1].Day != day {
		s.days = append(s.days, trafficDay{Day: day})
		if extra := len(s.days) - trafficKeepDays; extra > 0 {
			s.days = append(s.days[:0], s.days[extra:]...)
		}
	}
	return &s.days[len(s.days)-1]
}

func (s *trafficStats) sample(now time.Time, up, down int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sampleLocked(now, up, down)
}

func (s *trafficStats) sampleLocked(now time.Time, up, down int64) {
	day := s.current(now)
	deltaUp, deltaDown := max(0, up-s.lastUp), max(0, down-s.lastDown)
	day.Up += deltaUp
	day.Down += deltaDown
	day.Hours[now.Hour()] += deltaUp + deltaDown
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
	var day trafficDay
	byDay := make(map[string]trafficDay, len(s.days))
	for _, d := range s.days {
		byDay[d.Day] = d
	}
	if d, ok := byDay[now.Format("2006-01-02")]; ok {
		day = d
	}
	week := make([]trafficDayTotal, 0, weekDays)
	for i := weekDays - 1; i >= 0; i-- {
		name := now.AddDate(0, 0, -i).Format("2006-01-02")
		d := byDay[name]
		week = append(week, trafficDayTotal{Day: name, Up: d.Up, Down: d.Down})
	}
	return trafficSnapshot{TodayUp: day.Up, TodayDown: day.Down, Hours: day.Hours,
		RateUp: s.rateUp, RateDown: s.rateDown, PeakUp: s.peakUp, PeakDown: s.peakDown,
		Week: week}
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
	data, err := json.Marshal(trafficFile{Days: s.days})
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
