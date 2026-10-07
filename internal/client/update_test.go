package client

import "testing"

func TestTestReleaseUpdateIsOfferedOnBothPlatforms(t *testing.T) {
	for _, platform := range []string{"windows", "android"} {
		t.Run(platform, func(t *testing.T) {
			for _, tc := range []struct {
				current, available string
				want               bool
			}{
				{"0.13.0-alpha.5", "v0.13.0-alpha.6", true},
				{"0.13.0-alpha.9", "0.13.0-alpha.10", true},
				{"0.13.0-alpha.6", "0.13.0-beta.1", true},
				{"0.13.0-beta.2", "0.13.0-rc.1", true},
				{"0.13.0-rc.1", "0.13.0", true},
				{"0.13.0-alpha.6", "0.13.0-alpha.5", false},
				{"0.13.0-alpha.6", "0.13.0-alpha.6", false},
				{"0.13.0", "0.13.0-rc.1", false},
				{"dev", "0.13.0-alpha.6", false},
			} {
				t.Run(tc.current+"→"+tc.available, func(t *testing.T) {
					url := "https://panel.example.test/sub/test/app/" + platform
					sub := Subscription{Apps: map[string]AppOffer{platform: {URL: url, Version: tc.available}}}
					offer, ok := sub.Update(platform, tc.current)
					if ok != tc.want {
						t.Fatalf("предложение обновления: %v, ждали %v", ok, tc.want)
					}
					if ok && (offer.Version != tc.available || offer.URL != url) {
						t.Fatalf("потеряны версия или ссылка: %+v", offer)
					}
				})
			}
		})
	}
}

// Обновление предлагается только вперёд и только когда есть с чем сравнить.
func TestUpdateIsOfferedOnlyForward(t *testing.T) {
	sub := Subscription{Apps: map[string]AppOffer{
		"windows": {URL: "https://p/sub/x/app/windows", Version: "v0.10.0"},
		"android": {URL: "https://p/sub/x/app/android"},
	}}

	if _, ok := sub.Update("windows", "0.9.3"); !ok {
		t.Fatal("0.9.3 → v0.10.0: обновление не предложено")
	}
	if _, ok := sub.Update("windows", "0.10.0"); ok {
		t.Fatal("та же версия предложена как новая")
	}
	if _, ok := sub.Update("windows", "0.11.0"); ok {
		t.Fatal("старая сборка на панели предложена как новая")
	}
	if _, ok := sub.Update("windows", "dev"); ok {
		t.Fatal("сборке разработчика предложено обновление")
	}
	if _, ok := sub.Update("android", "0.1"); ok {
		t.Fatal("без версии на панели сравнивать не с чем, а обновление предложено")
	}
	if _, ok := sub.Update("linux", "0.1"); ok {
		t.Fatal("платформа, которой нет на панели, получила обновление")
	}
	if _, ok := sub.Update("windows", "что-то"); ok {
		t.Fatal("непонятная своя версия сочтена устаревшей")
	}
}

// Ссылка на обновление уходит в систему как есть, а панель для покупателя —
// чужой сервер. Открывать оттуда можно только страницу в браузере.
func TestUpdateLinkIsOnlyAWebPage(t *testing.T) {
	bad := []string{
		"file:///C:/Windows/System32/cmd.exe",
		"\\\\evil\\share\\update.exe",
		"C:\\Users\\Public\\x.exe",
		"intent://scan/#Intent;scheme=zxing;end",
		"javascript:alert(1)",
		"https:///no-host",
		"",
	}
	for _, link := range bad {
		sub := Subscription{Apps: map[string]AppOffer{"windows": {URL: link, Version: "v9.9.9"}}}
		if _, ok := sub.Update("windows", "0.1.0"); ok {
			t.Errorf("ссылка %q предложена к открытию", link)
		}
	}
	sub := Subscription{Apps: map[string]AppOffer{"windows": {URL: "https://p.example:8443/sub/x/app/windows", Version: "v9.9.9"}}}
	if _, ok := sub.Update("windows", "0.1.0"); !ok {
		t.Fatal("обычная https-ссылка отвергнута")
	}
}
