package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/securedns"
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

	// LANOutside — частные адреса (домашняя сеть) мимо туннеля. Выключено по
	// умолчанию по той же причине, что на телефоне: в чужом Wi-Fi «домашняя
	// сеть» — это чужая сеть, и пускать туда трафик мимо туннеля человек
	// решает сам.
	LANOutside bool `json:"lan_outside,omitempty"`

	// DNS — чей резолвер отвечает на запросы имён: адрес IPv4 без порта.
	// Пусто — тот, что задан флагом -dns (по умолчанию Cloudflare). Сами
	// запросы в любом случае идут через туннель; выбор только в том, кто на
	// другом конце: у кого-то есть фильтр рекламы, у кого-то нет.
	DNS string `json:"dns,omitempty"`

	// DNSPlain — имена как раньше: открытым DNS через туннель, и нода их
	// видит. По умолчанию имена к известному резолверу уходят по HTTPS (см.
	// internal/securedns). Хранится как «открыто», чтобы умолчание —
	// шифровать — было нулём и в файле, записанном прежней версией.
	DNSPlain bool `json:"dns_plain,omitempty"`

	// Fragment — резать TLS-приветствие к нодам так, чтобы имя из SNI не
	// лежало целиком ни в одном TCP-сегменте. Против фильтров по имени.
	// Выключено по умолчанию, как на телефоне: без такого фильтра у
	// провайдера это лишние пакеты и своя примета.
	Fragment bool `json:"fragment,omitempty"`

	// IPv6Off — не пускать IPv6 через туннель вовсе. Мимо он при этом тоже не
	// уходит: маршрут IPv6 остаётся в туннеле, мост отвечает «адресов IPv6
	// нет», и программы идут по IPv4 (tunbridge.Config.NoIPv6). Нужен, когда
	// сайты путают страну: IPv6-адрес ноды их базы порой относят не туда, куда
	// IPv4, и один сервис видит человека то здесь, то там. Хранится как
	// «выключен», чтобы умолчание — IPv6 через туннель — было нулём.
	IPv6Off bool `json:"ipv6_off,omitempty"`

	// ReportsOff — не отправлять продавцу замеры нод при подключении. Только
	// для ключа Marvia: чужим панелям отчёты не шлются вовсе. Хранится как
	// «выключено», чтобы умолчание — отправлять, как на телефоне, — было нулём.
	ReportsOff bool `json:"reports_off,omitempty"`
}

// SettingsView — настройки подключения и правда о них для окна.
type SettingsView struct {
	BypassRussian bool `json:"bypass_russian"`

	// RuCount — сколько российских подсетей сейчас уводится мимо туннеля;
	// RuError — почему список не скачался. Оба нужны под переключателем.
	RuCount int    `json:"ru_count"`
	RuError string `json:"ru_error,omitempty"`

	LANOutside bool `json:"lan_outside"`

	// DNS — выбранный резолвер; DNSInUse — тот, с которым поднят туннель.
	// Разные — значит выбор применится при следующем подключении: адрес
	// резолвера стоит на адаптере и в политике имён системы, а их меняют
	// только вместе с адаптером.
	DNS      string `json:"dns"`
	DNSInUse string `json:"dns_in_use,omitempty"`

	// DNSSecure — человек хочет шифровать имена; DNSSecureNow — получится ли
	// это с выбранным резолвером: свой адрес шифровать нечем, и окно должно
	// сказать это прямо, а не показывать включённый переключатель, который
	// ничего не делает. DNSSecureInUse — шифрует ли поднятый туннель.
	DNSSecure      bool  `json:"dns_secure"`
	DNSSecureNow   bool  `json:"dns_secure_now"`
	DNSSecureInUse *bool `json:"dns_secure_in_use,omitempty"`

	// Fragment — выбранное; FragmentInUse — с каким поднят туннель. Дробление
	// решается при дозвоне до ноды, поэтому новое действует со следующего
	// подключения.
	Fragment      bool  `json:"fragment"`
	FragmentInUse *bool `json:"fragment_in_use,omitempty"`

	// IPv6 — пускать ли IPv6 через туннель; IPv6InUse — как у поднятого.
	IPv6      bool  `json:"ipv6"`
	IPv6InUse *bool `json:"ipv6_in_use,omitempty"`

	// Reports — отправлять ли замеры нод панели продавца.
	Reports bool `json:"reports"`
}

// SettingsPatch — что окно просит поменять; nil — не трогать.
type SettingsPatch struct {
	BypassRussian *bool   `json:"bypass_russian"`
	LANOutside    *bool   `json:"lan_outside"`
	DNS           *string `json:"dns"`
	DNSSecure     *bool   `json:"dns_secure"`
	Fragment      *bool   `json:"fragment"`
	IPv6          *bool   `json:"ipv6"`
	Reports       *bool   `json:"reports"`
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

// homeNetwork — адрес из домашней сети: частные диапазоны IPv4 (10/8,
// 172.16/12, 192.168/16) — те же, что исключает телефон, — и их IPv6-пара
// fc00::/7.
//
// Свою подсеть Windows и без того ведёт мимо туннеля: её маршрут точнее
// половин /1, которыми туннель забирает трафик. Переключатель нужен для
// остального частного — соседней подсети за роутером, сетевого диска,
// камеры, — которое иначе ушло бы на ноду за границей и там пропало.
func homeNetwork(ip netip.Addr) bool {
	return ip.IsPrivate()
}

var (
	errDNSBad   = errors.New("dnsBad")
	errDNSLocal = errors.New("dnsLocal")
)

// checkDNS проверяет свой адрес резолвера — те же правила, что у телефона.
//
// Опечатка здесь означает «интернет не работает» без единой подсказки,
// почему, поэтому строго: четыре числа без ведущих нулей (010 одни читают
// восьмеричным, другие десятичным). Локальный адрес отвергается отдельно и с
// причиной: запрос имени уходит в туннель, и роутер 192.168.1.1 оттуда не
// виден, а нода к частным адресам не ходит. IPv6 не берём: при ноде без
// IPv6 такой резолвер молча перестал бы отвечать.
func checkDNS(address string) error {
	parts := strings.Split(strings.TrimSpace(address), ".")
	if len(parts) != 4 {
		return errDNSBad
	}
	var n [4]int
	for i, p := range parts {
		if p == "" || len(p) > 3 || (len(p) > 1 && p[0] == '0') {
			return errDNSBad
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return errDNSBad
			}
			n[i] = n[i]*10 + int(r-'0')
		}
		if n[i] > 255 {
			return errDNSBad
		}
	}
	a, b := n[0], n[1]
	local := a == 0 || a == 10 || a == 127 || a >= 224 ||
		(a == 100 && b >= 64 && b <= 127) ||
		(a == 169 && b == 254) ||
		(a == 172 && b >= 16 && b <= 31) ||
		(a == 192 && b == 168)
	if local {
		return errDNSLocal
	}
	return nil
}

// resolver — куда слать запросы имён при подключении, в виде host:port.
// Испорченный выбор в файле не ломает имена, а откатывается на флаг.
func (s pcSettings) resolver(fallback string) string {
	if s.DNS != "" && checkDNS(s.DNS) == nil {
		return net.JoinHostPort(strings.TrimSpace(s.DNS), "53")
	}
	return fallback
}

// encrypted — пойдут ли имена по HTTPS при этих настройках к этому резолверу
// (host:port). Нет — если человек выбрал «как раньше» или резолвер свой: для
// своего адреса мы не знаем ни имени сертификата, ни пути DoH.
func (s pcSettings) encrypted(resolver string) bool {
	if s.DNSPlain {
		return false
	}
	_, ok := securedns.For(resolver)
	return ok
}
