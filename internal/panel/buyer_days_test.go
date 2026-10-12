package panel

import (
	"strconv"
	"testing"
	"time"
)

// Покупатель видит, сколько дней осталось, словом в нужной форме, а когда
// остаётся мало — срок или трафик, — состояние окрашено как предупреждение.
func TestBuyerSeesDaysLeftInTheRightFormAndAWarningNearTheEnd(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for days, want := range map[int]string{1: "день", 3: "дня", 5: "дней", 12: "дней", 21: "день", 24: "дня"} {
		until := now.Add(time.Duration(days) * 24 * time.Hour)
		view := buyerDetails(User{Enabled: true, ExpiresAt: &until}, buyerRussian, now)
		if view.DaysLeft != strconv.Itoa(days) || view.DaysWord != want {
			t.Errorf("%d: показано %q %q", days, view.DaysLeft, view.DaysWord)
		}
	}
	soon := now.Add(48 * time.Hour)
	if view := buyerDetails(User{Enabled: true, ExpiresAt: &soon}, buyerRussian, now); view.State != "warn" {
		t.Errorf("за два дня до конца состояние %q", view.State)
	}
	if view := buyerDetails(User{Enabled: true, TrafficLimit: 1000, Used: 950}, buyerRussian, now); view.State != "warn" || view.TrafficLeft != 5 {
		t.Errorf("при 5%% трафика: %q, %d%%", view.State, view.TrafficLeft)
	}
	if view := buyerDetails(User{Enabled: false}, buyerRussian, now); view.State != "off" {
		t.Errorf("отключённый доступ: %q", view.State)
	}
}
