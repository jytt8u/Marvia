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

func shellForTest(t *testing.T) string {
	t.Helper()
	if sh, err := exec.LookPath("sh"); err == nil {
		return sh
	}
	if git, err := exec.LookPath("git"); err == nil {
		bash := filepath.Join(filepath.Dir(filepath.Dir(git)), "bin", "bash.exe")
		if _, err := os.Stat(bash); err == nil {
			return bash
		}
	}
	t.Skip("нет POSIX shell")
	return ""
}

func TestFailedServiceStartRestoresBinaryAndDatabase(t *testing.T) {
	raw, err := os.ReadFile("../../scripts/upgrade.sh")
	if err != nil {
		t.Fatal(err)
	}
	extract := func(name string) string {
		t.Helper()
		s := string(raw)
		start := strings.Index(s, name+"() {")
		if start < 0 {
			t.Fatalf("нет функции %s", name)
		}
		end := strings.Index(s[start:], "\n}")
		if end < 0 {
			t.Fatalf("нет конца функции %s", name)
		}
		return s[start:start+end+2] + "\n"
	}
	for _, mode := range []string{"start-failed", "no-listener"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			for _, child := range []string{"panel", "fresh"} {
				if err := os.Mkdir(filepath.Join(dir, child), 0700); err != nil {
					t.Fatal(err)
				}
			}
			files := map[string]string{
				"panel/marvia-panel": "#!/bin/sh\necho installed-file-was-executed > executed\n",
				"fresh/marvia-panel": "#!/bin/sh\necho 'marvia-panel v0.12.0'\n",
				"panel/panel.db":     "old-database", "panel/panel.db-wal": "old-wal",
			}
			for name, value := range files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0700); err != nil {
					t.Fatal(err)
				}
			}
			script := "set -eu\nPANEL_DIR=./panel\nBACKUP_DIR=./backups\nPANEL_DB_COPY=''\ntmp=./fresh\n" +
				extract("installed_version") + extract("as_owner") + extract("backup_panel_db") + extract("restore_panel_db") + extract("swap") + `
say() { :; }
ok() { :; }
bad() { :; }
die() { exit 1; }
sleep() { :; }
ss() { :; }
journalctl() { :; }
systemctl() {
 case "$1" in
 start) echo migrated > panel/panel.db; echo new-wal > panel/panel.db-wal; [ "$TEST_MODE" != start-failed ] ;;
 show) echo 42 ;;
 is-active) echo active ;;
 *) return 0 ;;
 esac
}
swap marvia-panel ./panel marvia-panel backup_panel_db
`
			// Второй запуск после отката должен лишь запустить старую службу,
			// а не имитировать повторную миграцию новой версии.
			script = strings.Replace(script, "start) echo migrated", "start) if [ -f started ]; then return 0; fi; touch started; echo migrated", 1)
			if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(shellForTest(t), "check.sh")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "TEST_MODE="+mode)
			if out, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("сбой не замечен: %s", out)
			}
			// Сценарий должен упасть на откате, а не раньше: иначе файлы
			// «восстановлены» просто потому, что до подмены дело не дошло.
			if _, err := os.Stat(filepath.Join(dir, "started")); err != nil {
				t.Fatal("до запуска новой версии сценарий не дошёл")
			}
			for _, name := range []string{"panel/marvia-panel", "panel/panel.db", "panel/panel.db-wal"} {
				value, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || string(value) != files[name] {
					t.Fatalf("не восстановлен %s: %q %v", name, value, err)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "executed")); !os.IsNotExist(err) {
				t.Fatal("исполнен прежний бинарник от root")
			}
		})
	}
}

// upgradeFunctions вырезает из upgrade.sh функции по именам, чтобы проверять
// их по отдельности, без systemd и root.
func upgradeFunctions(t *testing.T, names ...string) string {
	t.Helper()
	raw, err := os.ReadFile("../../scripts/upgrade.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	var out strings.Builder
	for _, name := range names {
		start := strings.Index(s, name+"() {")
		if start < 0 {
			t.Fatalf("нет функции %s", name)
		}
		end := strings.Index(s[start:], "\n}")
		if end < 0 {
			t.Fatalf("нет конца функции %s", name)
		}
		out.WriteString(s[start:start+end+2] + "\n")
	}
	return out.String()
}

// Каталог панели принадлежит её службе, а обновление идёт от root. Служба,
// которую взломали, заранее кладёт ссылки на имена, по которым root будет
// писать: на копию базы перед обновлением и на саму базу перед откатом.
func TestUpgradeDoesNotWriteThroughLinksPlantedByService(t *testing.T) {
	dir := t.TempDir()
	for _, child := range []string{"panel", "outside"} {
		if err := os.Mkdir(filepath.Join(dir, child), 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"panel/panel.db": "old-database", "panel/panel.db-wal": "old-wal",
		"outside/backup-target": "untouched", "outside/restore-target": "untouched",
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Имя копии предсказуемо до секунды; date в проверке подменён, так что
	// ссылка ложится ровно туда, куда раньше писал root.
	planted := filepath.Join(dir, "panel", "panel.db.before-upgrade.20261004-120000")
	if err := os.Symlink(filepath.Join(dir, "outside", "backup-target"), planted); err != nil {
		t.Skipf("ссылки здесь не создать: %v", err)
	}

	script := "set -eu\nPANEL_DIR=./panel\nBACKUP_DIR=./backups\nPANEL_DB_COPY=''\n" +
		upgradeFunctions(t, "as_owner", "backup_panel_db", "restore_panel_db") + `
say() { :; }
ok() { :; }
die() { exit 1; }
date() { echo 20261004-120000; }
backup_panel_db
# Новая версия «испортила» базу, а служба успела подменить её ссылкой.
rm -f panel/panel.db panel/panel.db-wal
ln -s "$PWD/outside/restore-target" panel/panel.db
restore_panel_db
`
	if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(shellForTest(t), "check.sh")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("сценарий упал: %v\n%s", err, out)
	}

	for _, name := range []string{"outside/backup-target", "outside/restore-target"} {
		value, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(value) != "untouched" {
			t.Fatalf("root записал через подложенную ссылку в %s: %q %v", name, value, err)
		}
	}
	info, err := os.Lstat(filepath.Join(dir, "panel", "panel.db"))
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("на месте базы осталась ссылка: %v", err)
	}
	for name, want := range map[string]string{"panel/panel.db": "old-database", "panel/panel.db-wal": "old-wal"} {
		value, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(value) != want {
			t.Fatalf("не восстановлен %s: %q %v", name, value, err)
		}
	}
}

func TestAgentRejectsUnsignedOrCorruptedReleaseBeforeExecution(t *testing.T) {
	for _, mode := range []string{"valid", "bad-signature", "bad-checksum", "download-failed", "wrong-origin"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			for _, child := range []string{"bin", "fixture"} {
				if err := os.Mkdir(filepath.Join(dir, child), 0700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(name, text string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0700); err != nil {
					t.Fatal(err)
				}
			}
			payload := "#!/bin/sh\nprintf '%s' \"$MARVIA_RELEASE_TAG\" > ran\n"
			write("fixture/upgrade.sh", payload)
			write("fixture/SHA256SUMS", fmt.Sprintf("%x  upgrade.sh\n", sha256.Sum256([]byte(payload))))
			write("fixture/SHA256SUMS.sigstore.json", "fixture signature")
			if mode == "bad-checksum" {
				write("fixture/upgrade.sh", payload+"# corrupted\n")
			}
			write("bin/curl", `#!/bin/sh
out=
while [ "$#" -gt 0 ]; do
 case "$1" in -o) out=$2; shift 2 ;; *) url=$1; shift ;; esac
done
case "$url" in
 */releases/latest)
  if [ "$TEST_MODE" = wrong-origin ]; then printf 'https://example.com/releases/tag/v0.12.0'; else printf 'https://github.com/jytt8u/marvia/releases/tag/v0.12.0'; fi ;;
 *) [ "$TEST_MODE" = download-failed ] && exit 1
    cp "fixture/${url##*/}" "$out" ;;
esac
`)
			write("bin/gh", "#!/bin/sh\nprintf '%s\\n' \"$@\" > verified-args\n[ \"$TEST_MODE\" != bad-signature ]\n")
			script := strings.ReplaceAll(string(agent), "/var/lib/marvia-upgrade", "./state")
			script = strings.ReplaceAll(script, "/opt/marvia-node", "./node")
			script = strings.ReplaceAll(script, "/opt/marvia", "./panel")
			write("agent.sh", script)
			cmd := exec.Command(shellForTest(t), "-c", `export PATH="$PWD/bin:$PATH"; exec sh agent.sh`)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "TEST_MODE="+mode)
			out, err := cmd.CombinedOutput()
			ran, ranErr := os.ReadFile(filepath.Join(dir, "ran"))
			if mode == "valid" {
				if err != nil || ranErr != nil || string(ran) != "v0.12.0" {
					t.Fatalf("подписанный релиз не выполнен: %s %v %v", out, err, ranErr)
				}
				args, _ := os.ReadFile(filepath.Join(dir, "verified-args"))
				for _, want := range []string{"--bundle", "--repo\njytt8u/marvia", "release.yml@refs/tags/v0.12.0", "--source-ref\nrefs/tags/v0.12.0", "--deny-self-hosted-runners"} {
					if !strings.Contains(string(args), want) {
						t.Fatalf("не проверено происхождение %q: %s", want, args)
					}
				}
			} else if err == nil || !os.IsNotExist(ranErr) {
				t.Fatalf("опасный релиз был выполнен: %s %v %v", out, err, ranErr)
			}
		})
	}
}

func TestVersionCheckNeverExecutesAnInstalledBinary(t *testing.T) {
	raw, err := os.ReadFile("../../scripts/upgrade.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	start := strings.Index(s, "installed_version() {")
	if start < 0 {
		t.Fatal("нет проверки установленной версии")
	}
	end := strings.Index(s[start:], "\n}")
	if end < 0 {
		t.Fatal("нет конца функции")
	}
	dir := t.TempDir()
	// Даже при владельце root нельзя исполнять файл: один из родительских
	// каталогов может принадлежать панели, и проверка владельца гонку не лечит.
	script := s[start:start+end+2] + "\nstat() { echo root; }\ninstalled_version ./installed\n"
	if err := os.WriteFile(filepath.Join(dir, "check.sh"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "installed"), []byte("#!/bin/sh\necho executed > executed\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(shellForTest(t), "check.sh")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("проверка версии: %s %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "executed")); !os.IsNotExist(err) {
		t.Fatal("установленный файл был исполнен")
	}
}
