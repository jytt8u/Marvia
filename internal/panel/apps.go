package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Раздача приложений покупателям.
//
// Приложение должен отдавать сервер продавца, а не магазин и не чужой
// файлохостинг. В августе 2026 провайдеры начали рубить загрузку из Google
// Play и App Store, объясняя это блокировкой международных CDN; GitHub, где
// лежат наши сборки, раздаётся ровно через такой же CDN. Ссылка «скачай
// приложение» перестаёт работать раньше всего остального — и продавец теряет
// покупателя до того, как тот заплатил.
//
// Домен панели у покупателя уже рабочий: он ходит на него за подпиской. Пусть
// с него же и качает.

// Приложения, которые панель раздаёт. Имя из адреса сопоставляется с файлом:
// брать имя файла прямо из адреса нельзя, иначе туда попросят «../../etc/shadow».
type appFile struct {
	file string
	mime string
}

var appFiles = map[string]appFile{
	"android": {"marvia-android.apk", "application/vnd.android.package-archive"},
	"windows": {"marvia-windows-setup.exe", "application/octet-stream"},
}

// Сначала предлагаем установщик, затем полный переносимый архив. Старый
// одиночный EXE не выдаём: окно откроется, но без Wintun VPN не поднимется.
func (a *API) availableApp(name string) (appFile, bool) {
	app, known := appFiles[name]
	if !known {
		return appFile{}, false
	}
	candidates := []appFile{app}
	if name == "windows" {
		candidates = append(candidates, appFile{"marvia-windows.zip", "application/zip"})
	}
	for _, candidate := range candidates {
		info, err := os.Stat(filepath.Join(a.distDir, candidate.file))
		if err == nil && info.Mode().IsRegular() {
			return candidate, true
		}
	}
	return appFile{}, false
}

// appDownload отдаёт приложение по токену подписки.
//
// Токен, а не открытый адрес: панель не должна на первом же запросе сканера
// признаваться, что она панель обхода блокировок. У покупателя токен есть — он
// пришёл в той же ссылке, что и доступ.
func (a *API) appDownload(w http.ResponseWriter, r *http.Request) {
	if _, err := a.store.UserBySubToken(r.Context(), r.PathValue("token")); err != nil {
		// Не подсказываем, существует ли токен: перебор подписок — обычное
		// занятие тех, кто ищет чужие ноды.
		http.NotFound(w, r)
		return
	}

	app, known := appFiles[r.PathValue("name")]
	if !known {
		http.NotFound(w, r)
		return
	}
	if available, exists := a.availableApp(r.PathValue("name")); exists {
		app = available
	} else {
		fail(w, http.StatusServiceUnavailable, "приложение пока не выложено: нужен полный комплект "+app.file)
		return
	}

	f, err := os.Open(filepath.Join(a.distDir, app.file))
	if err != nil {
		fail(w, http.StatusServiceUnavailable,
			"приложение пока не выложено: положи "+app.file+" в "+a.distDir)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", app.mime)
	// Имя файла нужно телефону: без него браузер сохранит apk как «download»,
	// и человек не сможет его поставить.
	w.Header().Set("Content-Disposition", `attachment; filename="`+app.file+`"`)
	http.ServeContent(w, r, app.file, info.ModTime(), f)
}

// appVersion — какая версия приложения выложена.
//
// Читается из файла рядом: marvia-android.apk.version, одна строка. Его
// пишет установщик и обновление панели — они качают приложения из того же
// релиза, что и панель, и знают его версию. Файла нет — версия пустая, и
// клиент про обновления молчит: лучше не сказать, чем сказать не то.
// Разбирать версию из самого apk панель не берётся: это парсер чужого
// формата ради строки, которую и так знает тот, кто файл положил.
func (a *API) appVersion(file string) string {
	raw, err := os.ReadFile(filepath.Join(a.distDir, file+".version"))
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	return strings.TrimSpace(line)
}

// AppOffer — что панель говорит клиенту про выложенное приложение:
// откуда скачать и какая это версия. По ней клиент решает, показать ли
// «есть новая».
type AppOffer struct {
	URL     string `json:"url"`
	Version string `json:"version,omitempty"`
}

// appOffers — ссылки с версиями для нашего клиента. Только то, что лежит на
// диске, по той же причине, что и в appLinks.
func (a *API) appOffers(subToken string) map[string]AppOffer {
	out := map[string]AppOffer{}
	for name := range appFiles {
		app, exists := a.availableApp(name)
		if !exists {
			continue
		}
		out[name] = AppOffer{URL: a.subURL(subToken) + "/app/" + name, Version: a.appVersion(app.file)}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// appLinks — готовые ссылки на приложения для одного покупателя.
//
// Возвращаем только то, что есть на диске: ссылка на отсутствующий файл хуже
// отсутствия ссылки, потому что покупатель по ней сходит и придёт с вопросом.
func (a *API) appLinks(subToken string) map[string]string {
	out := map[string]string{}
	for name := range appFiles {
		if _, exists := a.availableApp(name); !exists {
			continue
		}
		out[name] = a.subURL(subToken) + "/app/" + name
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// AppFile — что панель знает про выложенное приложение.
type AppFile struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	// Version — из файла рядом; пусто, если его нет.
	Version string `json:"version,omitempty"`
}

// listApps говорит продавцу, какие приложения у него выложены.
//
// Нужно ровно затем, чтобы на вопрос «почему покупателю не пришла ссылка на
// приложение» был ответ, а не догадки. Сумма — чтобы сверить, что лежит именно
// то, что скачивал.
func (a *API) listApps(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(appFiles))
	for name := range appFiles {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]AppFile, 0, len(names))
	for _, name := range names {
		app, exists := a.availableApp(name)
		if !exists {
			continue
		}
		path := filepath.Join(a.distDir, app.file)

		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		sum, err := fileSum(path)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, AppFile{Name: name, File: app.file, Size: info.Size(), SHA256: sum, Version: a.appVersion(app.file)})
	}

	answer := map[string]any{"apps": out, "dist": a.distDir}
	if len(out) == 0 {
		answer["hint"] = "положи " + appNames() + " в " + a.distDir + " — покупателям они раздаются с домена панели"
	}
	ok(w, answer)
}

func appNames() string {
	names := make([]string, 0, len(appFiles))
	for _, app := range appFiles {
		names = append(names, app.file)
	}
	sort.Strings(names)
	return strings.Join(names, " и ")
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("сумма %s: %w", filepath.Base(path), err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
