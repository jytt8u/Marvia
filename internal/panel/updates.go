package panel

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jytt8u/marvia/internal/updater"
)

// Обновления по кнопке.
//
// Панель знает три вещи: свою версию, версии нод (их присылают сами ноды) и
// номер последнего релиза. По кнопке она ничего не ставит сама — у неё нет
// прав, и это правильно, — а кладёт просьбу службе обновления на своей
// машине (internal/updater) и ставит нодам пометку «обнови до такой-то»,
// которую они передают своим службам. Что именно ставить, решают службы:
// официальный релиз, сверенный с SHA256SUMS.

// releaseURL — страница последнего релиза. GitHub отвечает на неё
// переадресацией на .../tag/vX.Y.Z; номер берём из адреса, не трогая API
// с его ограничением на число запросов.
var releaseURL = "https://github.com/jytt8u/marvia/releases/latest"

// releaseEvery — как долго доверяем узнанному номеру в стабильном канале,
// testEvery — в тестовом. Раньше было шесть часов на оба: продавец
// публиковал альфу и видел в панели прежнюю до вечера. Тестовые выпуски
// выходят по нескольку в день, и ждать их дольше двадцати минут незачем;
// стабильные — реже, им хватает часа.
const (
	releaseEvery = time.Hour
	testEvery    = 20 * time.Minute
)

// freshEvery — не чаще этого «Проверить сейчас» идёт в GitHub на самом
// деле. Кнопку жмут и по нескольку раз подряд, а у GitHub есть предел
// запросов с одного адреса — для всей панели, а не для одной кнопки.
// Переменная, а не константа, — ради тестов.
var freshEvery = 20 * time.Second

var releaseTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// prereleaseTag — метки, которые принимает тестовый канал: ещё и alpha,
// beta, rc. Та же, что TEST_TAG в internal/updater/agent.sh.
var prereleaseTag = regexp.MustCompile(`^v\d+\.\d+\.\d+(-(alpha|beta|rc)\.\d+)?$`)

// feedTag вынимает метки из ленты releases.atom.
var feedTag = regexp.MustCompile(`releases/tag/([^"<]+)`)

// releaseCache помнит последний релиз своего канала.
type releaseCache struct {
	mu      sync.Mutex
	channel string
	latest  string
	err     string
	checked time.Time
}

// get отдаёт номер последнего релиза, спрашивая GitHub не чаще срока своего
// канала, а с fresh — не чаще freshEvery. Ошибка — строкой для продавца:
// панель в России может не видеть GitHub, и это надо сказать, а не молча
// показать пустоту.
func (c *releaseCache) get(ctx context.Context, fresh bool) (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Канал читаем каждый раз: root мог переключить его командой, и ждать
	// шесть часов, пока панель это заметит, незачем.
	channel := updater.Channel()
	if channel != c.channel {
		c.channel, c.latest, c.err, c.checked = channel, "", "", time.Time{}
	}
	every := releaseEvery
	if channel == "test" {
		every = testEvery
	}
	if fresh {
		every = freshEvery
	}
	if !c.checked.IsZero() && time.Since(c.checked) < every {
		return c.latest, c.err
	}
	fetch := fetchLatest
	if channel == "test" {
		fetch = fetchNewestPublished
	}
	latest, err := fetch(ctx)
	c.checked = time.Now()
	if err != nil {
		// Прежний известный номер не выбрасываем: он лучше, чем ничего.
		c.err = err.Error()
		return c.latest, c.err
	}
	c.latest, c.err = latest, ""
	return c.latest, ""
}

func fetchLatest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, releaseURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub недоступен: %w", err)
	}
	_ = resp.Body.Close()
	tag := path.Base(resp.Header.Get("Location"))
	if !releaseTag.MatchString(tag) {
		return "", fmt.Errorf("GitHub ответил %s без номера релиза", resp.Status)
	}
	return tag, nil
}

// fetchNewestPublished — самый новый по номеру опубликованный выпуск, включая
// тестовые. Лента releases.atom перечисляет метки, в том числе черновиков:
// у них SHA256SUMS снаружи не отдаётся, этим их и отсеиваем. По номеру, а
// не по дате: правка старой ветки, вышедшая позже, — не «самый свежий».
// Тот же выбор делает служба обновления (internal/updater/agent.sh), иначе
// панель предлагала бы одно, а ставилось бы другое.
func fetchNewestPublished(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	base := strings.TrimSuffix(releaseURL, "/latest")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+".atom", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub недоступен: %w", err)
	}
	feed, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("лента выпусков GitHub: %s", resp.Status)
	}
	var tags []string
	for _, m := range feedTag.FindAllSubmatch(feed, -1) {
		if tag := string(m[1]); prereleaseTag.MatchString(tag) {
			tags = append(tags, tag)
		}
	}
	sort.SliceStable(tags, func(i, j int) bool { return OlderVersion(tags[j], tags[i]) })
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	for _, tag := range tags {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, base+"/download/"+tag+"/SHA256SUMS", nil)
		if err != nil {
			return "", err
		}
		resp, err := noFollow.Do(req)
		if err != nil {
			return "", fmt.Errorf("GitHub недоступен: %w", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode < http.StatusBadRequest {
			return tag, nil
		}
	}
	return "", fmt.Errorf("в ленте GitHub нет опубликованного выпуска")
}

// WithHome сообщает панели её каталог: туда кладётся просьба к службе
// обновления, и его же та сторожит.
func (a *API) WithHome(dir string) *API {
	a.home = dir
	return a
}

// getUpdates — всё для страницы «Обновления»: версия панели, последний
// релиз, состояние службы обновления и ноды с их версиями.
func (a *API) getUpdates(w http.ResponseWriter, r *http.Request) {
	// fresh=1 — кнопка «Проверить сейчас»: продавец только что опубликовал
	// выпуск и не хочет ждать, пока панель спросит GitHub сама.
	latest, latestErr := a.releases.get(r.Context(), r.URL.Query().Get("fresh") == "1")
	nodes, err := a.store.ListNodes(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	type nodeView struct {
		ID        int64      `json:"id"`
		Name      string     `json:"name"`
		Enabled   bool       `json:"enabled"`
		Version   string     `json:"version"`
		Arch      string     `json:"arch,omitempty"`
		UpgradeTo string     `json:"upgrade_to,omitempty"`
		LastSeen  *time.Time `json:"last_seen,omitempty"`
	}
	views := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		views = append(views, nodeView{n.ID, n.Name, n.Enabled, n.Version, n.Arch, n.UpgradeTo, n.LastSeen})
	}
	ok(w, map[string]any{
		"latest":       latest,
		"latest_error": latestErr,
		"checked_at":   a.releases.checkedAt(),
		"channel":      updater.Channel(),
		"panel": map[string]any{
			"version": a.version,
			"updater": updater.ReadStatus(a.home),
		},
		"nodes": views,
	})
}

// upgradePanel кладёт просьбу обновить машину панели.
//
// Служба ставит последний релиз целиком: панель, ноду, если она стоит тут
// же, и приложения для раздачи. Панель на время подмены перезапускается —
// страница это переживает и ждёт.
func (a *API) upgradePanel(w http.ResponseWriter, r *http.Request) {
	if !updater.ReadStatus(a.home).Installed {
		fail(w, http.StatusConflict, updater.ErrNotInstalled.Error())
		return
	}
	if err := updater.Request(a.home); err != nil {
		fail(w, http.StatusInternalServerError, fmt.Sprintf("просьба не легла: %v", err))
		return
	}
	a.record(r, EventPanelUpgrade, Event{Detail: "до последнего релиза"})
	w.WriteHeader(http.StatusAccepted)
}

// upgradeNodes просит обновиться все включённые ноды, отставшие от
// последнего релиза. Ноды узнают об этом на следующей сверке — в течение
// четверти минуты — и передают просьбу своим службам обновления.
func (a *API) upgradeNodes(w http.ResponseWriter, r *http.Request) {
	latest, latestErr := a.releases.get(r.Context(), false)
	if latest == "" {
		msg := "не удалось узнать последний релиз"
		if latestErr != "" {
			msg += ": " + latestErr
		}
		fail(w, http.StatusConflict, msg)
		return
	}
	n, err := a.store.RequestNodeUpgrade(r.Context(), latest)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.record(r, EventNodesUpgrade, Event{Detail: fmt.Sprintf("до %s: %d", latest, n)})
	ok(w, map[string]any{"target": latest, "asked": n})
}

// upgradeAll — «Обновить всё»: просьба отставшим нодам и службе на машине
// панели одним нажатием. Раньше это были две кнопки, и вторую забывали:
// панель уезжала на новую версию, а ноды оставались на старой.
//
// Ноды — первыми: просьба им лежит в базе и переживёт перезапуск панели, а
// панель на время своей подмены перестаёт отвечать. Без службы обновления
// на машине панели ноды всё равно получают просьбу, а про панель ответ
// честно говорит, что её надо обновить командой.
func (a *API) upgradeAll(w http.ResponseWriter, r *http.Request) {
	latest, latestErr := a.releases.get(r.Context(), false)
	if latest == "" {
		msg := "не удалось узнать последний релиз"
		if latestErr != "" {
			msg += ": " + latestErr
		}
		fail(w, http.StatusConflict, msg)
		return
	}
	n, err := a.store.RequestNodeUpgrade(r.Context(), latest)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.record(r, EventNodesUpgrade, Event{Detail: fmt.Sprintf("до %s: %d", latest, n)})

	panel := false
	if OlderVersion(a.version, latest) && updater.ReadStatus(a.home).Installed {
		if err := updater.Request(a.home); err != nil {
			fail(w, http.StatusInternalServerError, fmt.Sprintf("ноды попрошены, а просьба панели не легла: %v", err))
			return
		}
		a.record(r, EventPanelUpgrade, Event{Detail: "до последнего релиза"})
		panel = true
	}
	ok(w, map[string]any{"target": latest, "asked": n, "panel": panel})
}

// checkedAt — когда панель последний раз спрашивала GitHub; пусто — ещё ни
// разу. Окно пишет «проверено N минут назад»: иначе не понять, свежий ли
// номер на экране.
func (c *releaseCache) checkedAt() *time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.checked.IsZero() {
		return nil
	}
	t := c.checked.UTC()
	return &t
}

// clip обрезает строку от ноды: версия и разрядность приходят из сети и в
// базу длиннее разумного не ложатся.
func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
