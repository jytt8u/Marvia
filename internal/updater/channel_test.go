package updater

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGitHub — GitHub для службы обновления: latest ведёт на stable, лента
// releases.atom перечисляет feed (новые сверху, как у GitHub), а файлы
// отдаются только у опубликованных меток — у черновиков их снаружи нет.
type fakeGitHub struct {
	stable string
	feed   []string
	drafts []string
}

// runAgent запускает настоящий agent.sh против fakeGitHub с заданным
// каналом и записью об установленной версии. Отдаёт метку, которую служба
// передала сценарию обновления (пусто — ничего не запускала), и статус.
func runAgent(t *testing.T, gh fakeGitHub, channel, installed string) (ran, status string) {
	t.Helper()
	dir := t.TempDir()
	for _, child := range []string{"bin", "fixture", "state"} {
		if err := os.Mkdir(filepath.Join(dir, child), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	payload := "#!/bin/sh\nprintf '%s' \"$MARVIA_RELEASE_TAG\" > ran\n"
	write("fixture/upgrade.sh", payload)
	write("fixture/SHA256SUMS", fmt.Sprintf("%x  upgrade.sh\n", sha256.Sum256([]byte(payload))))
	write("fixture/SHA256SUMS.sigstore.json", "fixture signature")
	var feed strings.Builder
	feed.WriteString("<feed>\n")
	for _, tag := range gh.feed {
		fmt.Fprintf(&feed, "<entry><link rel=\"alternate\" type=\"text/html\" href=\"https://github.com/jytt8u/marvia/releases/tag/%s\"/></entry>\n", tag)
	}
	feed.WriteString("</feed>\n")
	write("fixture/releases.atom", feed.String())
	if channel != "" {
		write("state/channel", channel+"\n")
	}
	if installed != "" {
		write("state/installed", installed+"\n")
	}
	write("bin/curl", `#!/bin/sh
out=
while [ "$#" -gt 0 ]; do
 case "$1" in -o) out=$2; shift 2 ;; *) url=$1; shift ;; esac
done
case "$url" in
 */releases/latest) printf 'https://github.com/jytt8u/marvia/releases/tag/%s' "$STABLE" ;;
 */releases.atom) cat fixture/releases.atom ;;
 */releases/download/*)
  tag=${url%/*}; tag=${tag##*/}
  for d in $DRAFTS; do [ "$d" = "$tag" ] && exit 22; done
  cp "fixture/${url##*/}" "$out" ;;
 *) exit 6 ;;
esac
`)
	write("bin/gh", "#!/bin/sh\nexit 0\n")
	script := strings.ReplaceAll(string(agent), "/var/lib/marvia-upgrade", "./state")
	script = strings.ReplaceAll(script, "/opt/marvia-node", "./node")
	script = strings.ReplaceAll(script, "/opt/marvia", "./panel")
	write("agent.sh", script)
	cmd := exec.Command(shellForTest(t), "-c", `export PATH="$PWD/bin:$PATH"; exec sh agent.sh`)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "STABLE="+gh.stable, "DRAFTS="+strings.Join(gh.drafts, " "))
	out, _ := cmd.CombinedOutput()
	got, _ := os.ReadFile(filepath.Join(dir, "ran"))
	st, _ := os.ReadFile(filepath.Join(dir, "state", "status"))
	t.Logf("вывод службы: %s", out)
	return string(got), string(st)
}

// Лента GitHub перечисляет и черновики (у Marvia это снятые 1.0.0 и 1.0.1),
// и правку старой ветки, вышедшую позже тестового выпуска. Тестовый канал
// берёт самый новый по номеру из опубликованных — не черновик и не самый
// свежий по дате.
func TestTestChannelInstallsTheNewestPublishedPrerelease(t *testing.T) {
	gh := fakeGitHub{
		stable: "v0.12.3",
		feed:   []string{"v0.12.3", "v1.0.1", "v0.13.0-alpha.6", "v0.13.0-alpha.10", "v1.0.0", "test-0.12.3", "v0.12.2"},
		drafts: []string{"v1.0.1", "v1.0.0", "v0.13.0-alpha.10"},
	}
	ran, status := runAgent(t, gh, "test", "")
	if ran != "v0.13.0-alpha.6" {
		t.Fatalf("тестовый канал поставил %q (статус %q)", ran, status)
	}
}

// Без явного выбора root служба остаётся на стабильных выпусках: тестовые
// сборки на сервер продавца сами не приезжают.
func TestWithoutAChoiceTheServerStaysOnStableReleases(t *testing.T) {
	gh := fakeGitHub{stable: "v0.12.2", feed: []string{"v0.13.0-alpha.6", "v0.12.2"}}
	if ran, status := runAgent(t, gh, "", ""); ran != "v0.12.2" {
		t.Fatalf("без выбора канала поставлено %q (статус %q)", ran, status)
	}
	gh.stable = "v0.13.0-alpha.6"
	if ran, _ := runAgent(t, gh, "stable", ""); ran != "" {
		t.Fatalf("стабильный канал принял тестовую метку за latest: %q", ran)
	}
}

// Сервер, уже стоящий на тестовом выпуске, не откатывается на стабильный,
// который старше: ни после возврата канала на stable, ни по просьбе ноды,
// у которой панель на другом канале. Откат panel.db через миграции назад —
// потеря данных, а не обновление.
func TestTheServiceNeverRollsTheServerBack(t *testing.T) {
	gh := fakeGitHub{stable: "v0.12.2", feed: []string{"v0.13.0-alpha.6", "v0.12.2"}}
	ran, status := runAgent(t, gh, "stable", "v0.13.0-alpha.6")
	if ran != "" {
		t.Fatalf("сервер на v0.13.0-alpha.6 откачен до %q", ran)
	}
	if !strings.Contains(status, "state=ok") {
		t.Fatalf("отказ от отката не сказан как штатный: %q", status)
	}
	if ran, _ := runAgent(t, gh, "test", "v0.13.0-alpha.5"); ran != "v0.13.0-alpha.6" {
		t.Fatalf("более новый тестовый выпуск не поставлен: %q", ran)
	}
	if ran, _ := runAgent(t, gh, "test", "v0.13.0-alpha.6"); ran != "" {
		t.Fatalf("уже стоящий выпуск ставится заново: %q", ran)
	}
}

// Ручная команда обновления и служба по кнопке выбирают выпуск одинаково:
// иначе сервер, обновлённый руками, служба тут же «поправила» бы на другой.
func TestManualUpgradeAndTheServicePickTheSameRelease(t *testing.T) {
	for _, name := range []string{"older", "newest_published"} {
		manual := upgradeFunctions(t, name)
		s := string(agent)
		start := strings.Index(s, name+"() {")
		if start < 0 {
			t.Fatalf("в службе нет %s", name)
		}
		end := strings.Index(s[start:], "\n}")
		if got := s[start:start+end+2] + "\n"; got != manual {
			t.Fatalf("%s разошлась:\nслужба:\n%s\nсценарий:\n%s", name, got, manual)
		}
	}
	for _, line := range []string{
		`TEST_TAG='^v[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)\.[0-9]+)?$'`,
	} {
		raw, err := os.ReadFile("../../scripts/upgrade.sh")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), line) || !strings.Contains(string(agent), line) {
			t.Fatalf("правило меток разошлось: %s", line)
		}
	}
}
