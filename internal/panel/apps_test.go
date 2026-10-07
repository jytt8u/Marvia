package panel_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/panel"
)

// Приложения раздаёт сама панель.
//
// Магазины приложений и международные CDN в России отваливаются первыми, а
// домен панели у покупателя уже рабочий: он ходит на него за подпиской.

const fakeAPK = "это как бы apk"

// Проверяем всю границу dist → подписка → решение клиента: версии панели
// самой по себе недостаточно, важен файл приложения и его соседний .version.
func TestNewerTestReleaseInDistOffersBothClientUpdates(t *testing.T) {
	for _, available := range []string{"0.13.0-alpha.6", "0.13.0-alpha.5", "0.13.0-alpha.4"} {
		t.Run(available, func(t *testing.T) {
			srv, admin := appPanel(t, map[string]string{
				"marvia-android.apk":               fakeAPK,
				"marvia-android.apk.version":       available + "\n",
				"marvia-windows-setup.exe":         "полный установщик",
				"marvia-windows-setup.exe.version": available + "\n",
			})
			token, _ := buySubscription(t, srv, admin)
			code, body := do(t, srv, "GET", "/sub/"+token+"?format=json", "", "")
			if code != http.StatusOK {
				t.Fatalf("подписка не отдалась: %d %s", code, body)
			}
			var sub client.Subscription
			if err := json.Unmarshal([]byte(body), &sub); err != nil {
				t.Fatal(err)
			}
			for _, platform := range []string{"windows", "android"} {
				offer, ok := sub.Update(platform, "0.13.0-alpha.5")
				if ok != (available == "0.13.0-alpha.6") {
					t.Errorf("%s: версия %s, обновление предложено=%v", platform, available, ok)
				}
				if ok && (offer.Version != available || offer.URL != "https://panel.example.test/sub/"+token+"/app/"+platform) {
					t.Errorf("%s: неверное предложение %+v", platform, offer)
				}
			}
		})
	}
}

// appPanel поднимает панель с каталогом раздачи и кладёт туда приложения.
func appPanel(t *testing.T, files map[string]string) (*httptest.Server, string) {
	t.Helper()

	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	admin, err := panel.NewToken()
	if err != nil {
		t.Fatalf("токен: %v", err)
	}

	dist := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dist, name), []byte(body), 0o644); err != nil {
			t.Fatalf("выкладка %s: %v", name, err)
		}
	}

	srv := httptest.NewServer(panel.NewAPI(store, admin, "https://panel.example.test", dist).Handler())
	t.Cleanup(srv.Close)

	return srv, admin
}

// buySubscription заводит покупателя и отдаёт его токен подписки и ссылки.
func buySubscription(t *testing.T, srv *httptest.Server, admin string) (token, body string) {
	t.Helper()

	code, body := do(t, srv, "POST", "/api/v1/users", admin, `{"label":"покупатель"}`)
	if code != http.StatusOK {
		t.Fatalf("покупатель не завёлся: %d %s", code, body)
	}
	return between(t, body, `"sub_token":"`, `"`), body
}

// TestAppComesFromPanel — покупатель качает приложение с домена продавца.
func TestAppComesFromPanel(t *testing.T) {
	srv, admin := appPanel(t, map[string]string{"marvia-android.apk": fakeAPK})
	token, body := buySubscription(t, srv, admin)

	// Ссылка приходит вместе с доступом: отдельно её искать негде.
	if !strings.Contains(body, `"apps"`) || !strings.Contains(body, "/app/android") {
		t.Fatalf("в ответе нет ссылки на приложение: %s", body)
	}

	code, got := do(t, srv, "GET", "/sub/"+token+"/app/android", "", "")
	if code != http.StatusOK {
		t.Fatalf("приложение не отдалось: %d %s", code, got)
	}
	if got != fakeAPK {
		t.Errorf("отдалось не то: %q", got)
	}
}

func TestWindowsDownloadIncludesItsDriver(t *testing.T) {
	for _, tc := range []struct {
		name               string
		files              map[string]string
		wantFile, wantBody string
	}{
		{"установщик", map[string]string{"marvia-windows.exe": "неполный EXE", "marvia-windows.zip": "переносимый комплект", "marvia-windows-setup.exe": "полный установщик", "marvia-windows-setup.exe.version": "1.0.2\n"}, "marvia-windows-setup.exe", "полный установщик"},
		{"переносимый комплект", map[string]string{"marvia-windows.exe": "неполный EXE", "marvia-windows.zip": "переносимый комплект", "marvia-windows.zip.version": "1.0.2\n"}, "marvia-windows.zip", "переносимый комплект"},
		{"только EXE", map[string]string{"marvia-windows.exe": "неполный EXE"}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, admin := appPanel(t, tc.files)
			token, created := buySubscription(t, srv, admin)
			_, subscription := do(t, srv, "GET", "/sub/"+token+"?format=json", "", "")
			_, listed := do(t, srv, "GET", "/api/v1/apps", admin, "")
			response, err := srv.Client().Get(srv.URL + "/sub/" + token + "/app/windows")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if tc.wantFile == "" {
				for _, body := range []string{created, subscription, listed} {
					if strings.Contains(body, `"windows"`) {
						t.Errorf("обещан неполный комплект Windows: %s", body)
					}
				}
				if response.StatusCode != http.StatusServiceUnavailable {
					t.Errorf("неполный комплект отдан: %d", response.StatusCode)
				}
				return
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("загрузка: %d", response.StatusCode)
			}
			if got := response.Header.Get("Content-Disposition"); got != `attachment; filename="`+tc.wantFile+`"` {
				t.Errorf("имя файла: %s", got)
			}
			if tc.wantFile == "marvia-windows.zip" && response.Header.Get("Content-Type") != "application/zip" {
				t.Error("архив выдан без типа ZIP")
			}
			body, err := io.ReadAll(response.Body)
			if err != nil || string(body) != tc.wantBody {
				t.Errorf("отдан другой комплект: %s, %v", body, err)
			}
			if !strings.Contains(created, "/app/windows") || !strings.Contains(subscription, `"version":"1.0.2"`) {
				t.Errorf("нет ссылки или версии: %s %s", created, subscription)
			}
			if !strings.Contains(listed, tc.wantFile) || strings.Contains(listed, `"file":"marvia-windows.exe"`) {
				t.Errorf("список приложений: %s", listed)
			}
			if code, _ := do(t, srv, "GET", "/sub/чужой/app/windows", "", ""); code != http.StatusNotFound {
				t.Errorf("комплект доступен без подписки: %d", code)
			}
		})
	}
}

// TestAppNeedsSubToken — по чужому токену приложение не отдаётся.
//
// Открытый файл apk на домене панели признаётся сканеру, что это панель обхода
// блокировок, на первом же запросе. У покупателя токен есть — он пришёл в той
// же ссылке, что и доступ.
func TestAppNeedsSubToken(t *testing.T) {
	srv, admin := appPanel(t, map[string]string{"marvia-android.apk": fakeAPK})
	buySubscription(t, srv, admin)

	for _, token := range []string{"чужой", "", "../../etc/passwd"} {
		code, _ := do(t, srv, "GET", "/sub/"+token+"/app/android", "", "")
		if code == http.StatusOK {
			t.Errorf("приложение отдалось по токену %q", token)
		}
	}
}

// TestUnknownAppIsNotAPath — имя приложения не превращается в путь на диске.
func TestUnknownAppIsNotAPath(t *testing.T) {
	srv, admin := appPanel(t, map[string]string{"marvia-android.apk": fakeAPK})
	token, _ := buySubscription(t, srv, admin)

	for _, name := range []string{"linux", "panel.db", "..%2Fpanel.db"} {
		code, _ := do(t, srv, "GET", "/sub/"+token+"/app/"+name, "", "")
		if code == http.StatusOK {
			t.Errorf("панель отдала %q", name)
		}
	}
}

// TestNoAppNoLink — ссылки на невыложенное приложение не бывает.
//
// Ссылка, ведущая в никуда, хуже её отсутствия: покупатель по ней сходит и
// придёт с вопросом к продавцу.
func TestNoAppNoLink(t *testing.T) {
	srv, admin := appPanel(t, nil)
	token, body := buySubscription(t, srv, admin)

	if strings.Contains(body, `"apps"`) {
		t.Errorf("ссылка на приложение есть, а файла нет: %s", body)
	}

	code, got := do(t, srv, "GET", "/sub/"+token+"/app/android", "", "")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("ожидался внятный отказ, пришло %d %s", code, got)
	}
	if !strings.Contains(got, "marvia-android.apk") {
		t.Errorf("в отказе не сказано, какой файл положить: %s", got)
	}
}

// TestListAppsTellsWhatIsLaidOut — продавец видит, что у него выложено.
func TestListAppsTellsWhatIsLaidOut(t *testing.T) {
	srv, admin := appPanel(t, map[string]string{"marvia-android.apk": fakeAPK})

	code, body := do(t, srv, "GET", "/api/v1/apps", admin, "")
	if code != http.StatusOK {
		t.Fatalf("список приложений не отдался: %d %s", code, body)
	}

	sum := sha256.Sum256([]byte(fakeAPK))
	if !strings.Contains(body, hex.EncodeToString(sum[:])) {
		t.Errorf("сумма не сошлась или её нет: %s", body)
	}
	if strings.Contains(body, "marvia-windows.exe") {
		t.Errorf("панель обещает то, чего не выложено: %s", body)
	}

	// А без токена — не отдаётся: что за приложения у продавца, посторонним
	// знать незачем.
	if code, _ := do(t, srv, "GET", "/api/v1/apps", "", ""); code != http.StatusUnauthorized {
		t.Errorf("список приложений отдался без токена: %d", code)
	}
}

// TestSubTokenRotates — утёкшую ссылку подписки можно сменить, не отзывая доступ.
//
// Ссылку покупатели раздают знакомым, а по ней отдаются список нод и секреты
// vless с trojan. Отзывать за это весь доступ — терять покупателя.
func TestSubTokenRotates(t *testing.T) {
	srv, admin := appPanel(t, map[string]string{"marvia-android.apk": fakeAPK})
	old, _ := buySubscription(t, srv, admin)

	if code, _ := do(t, srv, "GET", "/sub/"+old, "", ""); code != http.StatusOK {
		t.Fatalf("подписка не работает до смены: %d", code)
	}

	code, body := do(t, srv, "POST", "/api/v1/users/1/sub-token", admin, "")
	if code != http.StatusOK {
		t.Fatalf("токен не сменился: %d %s", code, body)
	}
	fresh := between(t, body, `"sub_token":"`, `"`)
	if fresh == old {
		t.Fatal("токен остался прежним")
	}
	if strings.Contains(body, "marvia://") {
		t.Errorf("смена адреса подписки выдала ключ доступа заново: %s", body)
	}

	if code, _ := do(t, srv, "GET", "/sub/"+old, "", ""); code != http.StatusNotFound {
		t.Errorf("старая ссылка всё ещё работает: %d", code)
	}
	if code, _ := do(t, srv, "GET", "/sub/"+fresh, "", ""); code != http.StatusOK {
		t.Errorf("новая ссылка не работает: %d", code)
	}

	// И приложение по старой ссылке больше не качается.
	if code, _ := do(t, srv, "GET", "/sub/"+old+"/app/android", "", ""); code == http.StatusOK {
		t.Error("приложение отдаётся по старой ссылке подписки")
	}
}

// TestSubscriptionTellsWhichVersionIsLaidOut — подписка называет версию
// выложенного приложения, чтобы клиент сам заметил, что устарел.
//
// Версия — из файла рядом с приложением. Нет файла — нет версии, и клиент
// про обновления молчит: лучше не сказать, чем сказать не то.
func TestSubscriptionTellsWhichVersionIsLaidOut(t *testing.T) {
	srv, admin := appPanel(t, map[string]string{
		"marvia-android.apk":         fakeAPK,
		"marvia-android.apk.version": "0.10.0\n",
		"marvia-windows.zip":         "это как бы полный комплект",
	})
	token, _ := buySubscription(t, srv, admin)

	code, body := do(t, srv, "GET", "/sub/"+token+"?format=json", "", "")
	if code != http.StatusOK {
		t.Fatalf("подписка не отдалась: %d %s", code, body)
	}
	if !strings.Contains(body, `"android":{"url":"https://panel.example.test/sub/`+token+`/app/android","version":"0.10.0"}`) {
		t.Errorf("версия android не названа: %s", body)
	}
	if strings.Contains(body, `"windows":{"url":"https://panel.example.test/sub/`+token+`/app/windows","version"`) {
		t.Errorf("у windows версии нет, а подписка её обещает: %s", body)
	}
	if !strings.Contains(body, `/app/windows"`) {
		t.Errorf("windows выложен, а ссылки нет: %s", body)
	}

	// Та же версия видна и продавцу в списке приложений.
	_, list := do(t, srv, "GET", "/api/v1/apps", admin, "")
	if !strings.Contains(list, `"version":"0.10.0"`) {
		t.Errorf("список приложений без версии: %s", list)
	}
}
