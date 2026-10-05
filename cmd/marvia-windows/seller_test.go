package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

// Закрытое напоминание не возвращается, а следующая ступень — «остался
// день» после «осталось три» — показывается снова.
func TestClosedReminderStaysClosedButTheLastDayComesBack(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	sub := client.Subscription{ExpiresAt: now.Add(60 * time.Hour).Format(time.RFC3339)}

	first := sellerView(sub, "", now)
	if first.Remind != client.ReminderExpiry || first.RemindValue != 3 || first.RemindKey == "" {
		t.Fatalf("за трое суток: %+v", first)
	}
	if again := sellerView(sub, first.RemindKey, now.Add(time.Hour)); again.Remind != "" {
		t.Fatalf("закрытое напоминание вернулось: %+v", again)
	}
	last := sellerView(sub, first.RemindKey, now.Add(40*time.Hour))
	if last.Remind != client.ReminderExpiry || last.RemindValue != 1 {
		t.Fatalf("последний день не напомнили: %+v", last)
	}
}

// Ссылки продавца в окно не уходят: только «кнопка есть». Открывает их
// программа по имени.
func TestWindowSeesButtonsButNotLinks(t *testing.T) {
	sub := client.Subscription{
		SupportURL: "https://t.me/seller_support",
		RenewURL:   "tg://resolve?domain=seller_bot",
		Announce:   "работы с 2:00 до 4:00",
	}
	v := sellerView(sub, "", time.Now())
	if !v.Support || !v.Renew || v.Announce != sub.Announce {
		t.Fatalf("канал продавца: %+v", v)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "t.me") || strings.Contains(string(raw), "tg://") {
		t.Errorf("ссылка ушла в окно: %s", raw)
	}
}

// Система получает только https:// и tg://, без кавычек, и только ту
// ссылку, которую продавец оставил.
func TestOnlyHttpsAndTelegramLinksReachTheSystem(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"https://t.me/seller_support", true},
		{"tg://resolve?domain=seller_bot", true},
		{"http://pay.example/renew", false},
		{"javascript:alert(1)", false},
		{"file:///C:/Windows/System32/calc.exe", false},
		{`https://pay.example/"/select,C:\`, false},
	}
	for _, c := range cases {
		got, err := sellerLink(client.Subscription{SupportURL: c.url}, "support")
		if c.ok && (err != nil || got != c.url) {
			t.Errorf("%s отвергнута: %v", c.url, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s дошла бы до системы", c.url)
		}
	}
	if _, err := sellerLink(client.Subscription{}, "renew"); !errors.Is(err, errNoSellerLink) {
		t.Errorf("без ссылки: %v", err)
	}
	if _, err := sellerLink(client.Subscription{SupportURL: "https://t.me/x"}, "update"); err == nil {
		t.Error("открылась ссылка, которой у продавца нет")
	}
}

// Уведомление из трея всплывает один раз на ступень и не всплывает, если
// человек уже закрыл это напоминание в окне.
func TestTrayReminderShowsEachStepOnceAndRespectsTheWindow(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	sub := client.Subscription{ExpiresAt: now.Add(60 * time.Hour).Format(time.RFC3339)}

	r, show := trayReminder(sub, "", "", now)
	if !show || r.Kind != client.ReminderExpiry {
		t.Fatalf("за трое суток уведомления нет: %+v %v", r, show)
	}
	if _, again := trayReminder(sub, "", r.Key, now.Add(30*time.Minute)); again {
		t.Fatal("то же уведомление всплыло второй раз")
	}
	if _, closed := trayReminder(sub, r.Key, "", now); closed {
		t.Fatal("уведомление всплыло, хотя напоминание закрыли в окне")
	}
	if last, show := trayReminder(sub, "", r.Key, now.Add(40*time.Hour)); !show || last.Value != 1 {
		t.Fatalf("последний день не всплыл: %+v %v", last, show)
	}
	if _, show := trayReminder(client.Subscription{}, "", "", now); show {
		t.Fatal("уведомление без срока и лимита")
	}
}
