package updater

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// argAfter — значение флага из аргументов, с которыми служба звала gh.
func argAfter(args, flag string) string {
	lines := strings.Split(args, "\n")
	for i, line := range lines {
		if line == flag && i+1 < len(lines) {
			return lines[i+1]
		}
	}
	return ""
}

// Репозиторий переименовали из jytt8u/marvia в jytt8u/Marvia, и выпуски с
// alpha.2 подписаны новым написанием. Строгая сверка строкой отвергала их
// все. Подпись принимается под любым регистром имени — GitHub его не
// различает, — но только из этого репозитория, этого workflow и этой метки.
func TestSignatureIsCheckedForThisRepoInAnySpellingAndNothingElse(t *testing.T) {
	r := runAgentFull(t, fakeGitHub{stable: "v0.12.2"}, "", "")
	if r.ran != "v0.12.2" {
		t.Fatalf("подписанный выпуск не поставлен: %+v", r)
	}
	pattern := argAfter(r.args, "--cert-identity-regex")
	if pattern == "" {
		t.Fatalf("подпись сверяется без шаблона личности: %s", r.args)
	}
	// gh написан на Go: тот же regexp, что сверит подпись на сервере.
	identity, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("шаблон %q: %v", pattern, err)
	}
	const wf = "/.github/workflows/release.yml@refs/tags/"
	for _, ok := range []string{
		"https://github.com/jytt8u/Marvia" + wf + "v0.12.2",
		"https://github.com/jytt8u/marvia" + wf + "v0.12.2",
	} {
		if !identity.MatchString(ok) {
			t.Errorf("своя подпись не принята: %s", ok)
		}
	}
	for _, bad := range []string{
		"https://github.com/evil/marvia" + wf + "v0.12.2",
		"https://github.com/jytt8u/marvia-fork" + wf + "v0.12.2",
		"https://github.com/jytt8u/marvia" + wf + "v0.12.20",
		"https://github.com/jytt8u/marvia" + wf + "v0x12x2",
		"https://github.com/jytt8u/marvia/.github/workflows/other.yml@refs/tags/v0.12.2",
		"https://github.com/jytt8u/marvia" + wf + "v0.13.0-alpha.6",
		"https://evil.example/https://github.com/jytt8u/marvia" + wf + "v0.12.2",
	} {
		if identity.MatchString(bad) {
			t.Errorf("чужая подпись принята: %s", bad)
		}
	}
	if argAfter(r.args, "--source-ref") != "refs/tags/v0.12.2" || !strings.Contains(r.args, "--deny-self-hosted-runners") {
		t.Fatalf("не проверено происхождение: %s", r.args)
	}
}

// С части хостингов сервер корней Sigstore (tuf-repo-cdn.sigstore.dev) не
// открывается, и gh отвечает «public good verifier is not available» — так
// встала кнопка обновления на ae-1. Тогда подпись проверяется корнем,
// приехавшим в прошлом проверенном выпуске. Без него — честный отказ.
func TestUnreachableSigstoreIsCheckedAgainstTheRootFromThePreviousRelease(t *testing.T) {
	gh := fakeGitHub{stable: "v0.12.2", sigstore: "offline", root: true}
	if r := runAgentFull(t, gh, "", ""); r.ran != "v0.12.2" || !strings.Contains(r.args, "--custom-trusted-root") {
		t.Fatalf("при недоступном Sigstore корень с машины не использован: %+v", r)
	}
	gh.root = false
	if r := runAgentFull(t, gh, "", ""); r.ran != "" || !strings.Contains(r.status, "state=failed") {
		t.Fatalf("без корня и без Sigstore выпуск поставлен: %+v", r)
	}
}

// Корень с машины — запасной путь проверки, а не обход: поддельная подпись
// не проходит ни со свежими корнями, ни с ним.
func TestForgedSignatureFailsWithTheLocalRootToo(t *testing.T) {
	r := runAgentFull(t, fakeGitHub{stable: "v0.12.2", sigstore: "forged", root: true}, "", "")
	if r.ran != "" {
		t.Fatalf("поддельная подпись принята: %+v", r)
	}
}

// Корень, который служба ставит рядом с собой, — настоящий корень доверия
// Sigstore: с удостоверяющим центром fulcio.sigstore.dev и журналом
// прозрачности. Пустой или чужой файл сделал бы запасную проверку
// бесполезной.
func TestTheServiceCarriesTheSigstoreTrustedRoot(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader(string(trustedRoot)))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	found := false
	for sc.Scan() {
		var root struct {
			MediaType string `json:"mediaType"`
			Tlogs     []struct {
				BaseURL string `json:"baseUrl"`
			} `json:"tlogs"`
			CertificateAuthorities []struct {
				URI string `json:"uri"`
			} `json:"certificateAuthorities"`
		}
		if err := json.Unmarshal(sc.Bytes(), &root); err != nil {
			t.Fatalf("строка корня не разбирается: %v", err)
		}
		if !strings.HasPrefix(root.MediaType, "application/vnd.dev.sigstore.trustedroot") {
			t.Fatalf("не корень доверия: %q", root.MediaType)
		}
		for _, ca := range root.CertificateAuthorities {
			if strings.Contains(ca.URI, "fulcio.sigstore.dev") && len(root.Tlogs) > 0 {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("в корне нет удостоверяющего центра публичного Sigstore")
	}
	if _, err := os.Stat("sigstore-root.jsonl"); err != nil {
		t.Fatal(err)
	}
}

// Ручная команда проверяет подпись так же, как служба: иначе сервер,
// который служба обновить не может, нельзя было бы обновить и руками.
func TestManualUpgradeChecksTheSignatureLikeTheService(t *testing.T) {
	manual := upgradeFunctions(t, "verify_release")
	s := string(agent)
	start := strings.Index(s, "verify_release() {")
	if start < 0 {
		t.Fatal("в службе нет verify_release")
	}
	end := strings.Index(s[start:], "\n}")
	if got := s[start:start+end+2] + "\n"; got != manual {
		t.Fatalf("проверка подписи разошлась:\nслужба:\n%s\nсценарий:\n%s", got, manual)
	}
}
