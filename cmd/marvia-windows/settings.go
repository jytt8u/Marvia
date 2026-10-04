package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pcSettings — то, что человек выбрал в настройках подключения.
//
// Один файл JSON, а не по файлу на значение, как язык окна: эти настройки
// читает подключение целиком и разом, и половина, записанная без второй
// половины, давала бы туннель, которого человек не выбирал.
//
// Незнакомое поле при чтении пропускается, пропущенное значит «как было по
// умолчанию»: файл переживает и откат на старую версию, и обновление.
type pcSettings struct {
	// BypassRussian — российские сайты мимо туннеля, по списку подсетей от
	// панели продавца. Выключено по умолчанию, как на телефоне: решение,
	// что банк должен видеть настоящий адрес, принимает человек.
	BypassRussian bool `json:"bypass_russian,omitempty"`
}

// SettingsView — настройки подключения и правда о них для окна.
type SettingsView struct {
	BypassRussian bool `json:"bypass_russian"`

	// RuCount — сколько российских подсетей сейчас уводится мимо туннеля;
	// RuError — почему список не скачался. Оба нужны под переключателем.
	RuCount int    `json:"ru_count"`
	RuError string `json:"ru_error,omitempty"`
}

// SettingsPatch — что окно просит поменять; nil — не трогать.
type SettingsPatch struct {
	BypassRussian *bool `json:"bypass_russian"`
}

// settingsFile — где лежат настройки подключения.
func settingsFile(dir string) string { return filepath.Join(dir, "settings.json") }

// loadSettings читает настройки. Нет файла или он испорчен — умолчания:
// туннель должен подниматься и тогда, когда настройки потерялись.
func loadSettings(dir string) pcSettings {
	var s pcSettings
	if dir == "" {
		return s
	}
	raw, err := os.ReadFile(settingsFile(dir))
	if err != nil || len(raw) > 64<<10 {
		return s
	}
	if json.Unmarshal(raw, &s) != nil {
		return pcSettings{}
	}
	return s
}

// saveSettings пишет настройки через временный файл: оборванная запись не
// должна оставить файл, который потом не прочитается.
func saveSettings(dir string, s pcSettings) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := settingsFile(dir) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, settingsFile(dir)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Российский список на диске.
//
// Тот же список, что качает телефон, и с того же адреса /sub/{токен}/bypass:
// панель отдаёт его всем своим приложениям, и подсети меняются раз в месяц.
// Скачанное лежит рядом с ключом: включение туннеля не должно ждать сети, а
// панель бывает недоступна ровно тогда, когда VPN и нужен.
const (
	ruRoutesFile = "ru-routes.txt"

	// ruRoutesMaxAge — как часто спрашиваем панель. Чаще раза в неделю
	// незачем: каждый поход — это запрос имени её домена.
	ruRoutesMaxAge = 7 * 24 * time.Hour
)

func ruRoutesPath(dir string) string { return filepath.Join(dir, ruRoutesFile) }

// loadRuRoutes читает скачанный список и когда он скачан. Нет файла — пусто.
func loadRuRoutes(dir string) ([]string, time.Time) {
	path := ruRoutesPath(dir)
	info, err := os.Stat(path)
	if err != nil || info.Size() > 16<<20 {
		return nil, time.Time{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}
	}
	return strings.Fields(string(raw)), info.ModTime()
}

// ruRoutesStale — пора ли спросить панель заново.
func ruRoutesStale(fetched, now time.Time) bool {
	return fetched.IsZero() || now.Sub(fetched) >= ruRoutesMaxAge
}

// saveRuRoutes кладёт список целиком и разом: половина списка молча
// оставила бы часть российских сайтов в туннеле.
func saveRuRoutes(dir string, prefixes []string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := ruRoutesPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(prefixes, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, ruRoutesPath(dir)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
