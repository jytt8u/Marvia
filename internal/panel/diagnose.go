package panel

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

// Проверка ноды из панели.
//
// «Нода не работает» — самое частое, с чем продавец приходит, и до сих пор
// ответить на это он мог только по SSH и журналу. Здесь панель проходит путь
// по шагам и говорит, на каком он рвётся: порт закрыт, порт отвечает не TLS,
// нода не сверяется с панелью, отстала версия, покупатели не могут
// подключиться, хотя панель её видит. На каждый шаг у окна панели готов
// совет, что сделать.
//
// Проверка идёт с сервера панели, а не из сети покупателя: блокировку у
// российского провайдера она не увидит. Её видят отчёты покупателей — они
// здесь тоже есть отдельным шагом.
//
// Ответ — коды и числа, без слов: слова на языке продавца подставляет окно.

// diagStep — один шаг проверки. State: ok, warn, fail или skip — шаг не
// имел смысла (TLS после закрытого порта).
type diagStep struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Value string `json:"value,omitempty"`
}

// diagTimeout — сколько ждём ноду на каждом сетевом шаге. Дольше — уже
// ответ сам по себе: покупатель ждать столько не станет.
const diagTimeout = 6 * time.Second

func (a *API) checkNode(w http.ResponseWriter, r *http.Request) {
	id, okID := pathID(w, r)
	if !okID {
		return
	}
	node, err := a.store.GetNode(r.Context(), id)
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	ok(w, map[string]any{"steps": a.diagnose(r.Context(), node)})
}

func (a *API) diagnose(ctx context.Context, node Node) []diagStep {
	var steps []diagStep
	add := func(id, state, value string) { steps = append(steps, diagStep{id, state, value}) }

	// Чем маскируется путь: у входа — его выход.
	mask := node
	if node.ExitID != 0 {
		exit, err := a.store.GetNode(ctx, node.ExitID)
		switch {
		case err != nil:
			add("exit", "fail", "")
		case !exit.Enabled:
			add("exit", "fail", exit.Name)
		default:
			add("exit", "ok", exit.Name)
			mask = exit
		}
	}

	// Порт.
	start := time.Now()
	dialer := net.Dialer{Timeout: diagTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", node.Address)
	if err != nil {
		add("tcp", "fail", dialReason(err))
		add("tls", "skip", "")
	} else {
		add("tcp", "ok", ms(time.Since(start)))

		// TLS. Под REALITY нода отдаёт рукопожатие сайта прикрытия, так что
		// сертификат обязан сойтись с его именем: не сошёлся — прикрытие
		// выбрано неудачно и выдаёт ноду при первой же проверке со стороны.
		start = time.Now()
		_ = conn.SetDeadline(time.Now().Add(diagTimeout))
		tc := tls.Client(conn, &tls.Config{ServerName: mask.SNI, MinVersion: tls.VersionTLS12})
		err = tc.HandshakeContext(ctx)
		var badCert *tls.CertificateVerificationError
		switch {
		case err == nil:
			add("tls", "ok", ms(time.Since(start)))
		case mask.SNI == "":
			add("tls", "warn", "nosni")
		case errors.As(err, &badCert):
			add("tls", "warn", "cert")
		default:
			add("tls", "fail", "")
		}
		_ = tc.Close()
	}

	// Сверка с панелью.
	switch {
	case node.LastSeen == nil:
		add("sync", "fail", "")
	default:
		age := time.Since(*node.LastSeen)
		state := "ok"
		if age > 10*time.Minute {
			state = "fail"
		} else if age > time.Minute {
			state = "warn"
		}
		add("sync", state, strconv.Itoa(int(age.Seconds())))
	}

	// Версия. Сравниваем с тем, что панель уже знает, — GitHub из проверки не
	// дёргаем: из России он отвечает по десять секунд.
	latest, _ := a.releases.get(ctx, false)
	switch {
	case node.Version == "":
		add("version", "warn", "")
	case latest != "" && OlderVersion(node.Version, latest):
		add("version", "warn", node.Version+" → "+latest)
	default:
		add("version", "ok", node.Version)
	}

	// Отчёты покупателей — единственный шаг, который видит сеть покупателя.
	if health, err := a.store.NodeHealth(ctx); err == nil {
		h := health[node.ID]
		value := strconv.Itoa(h.OK) + "/" + strconv.Itoa(h.Failed)
		switch {
		case h.OK+h.Failed == 0:
			add("reports", "skip", "")
		case h.Failed >= 3 && h.Failed > h.OK:
			add("reports", "warn", value)
		default:
			add("reports", "ok", value)
		}
	}
	return steps
}

// dialReason — почему не соединились, одним словом для окна.
func dialReason(err error) string {
	var ne net.Error
	switch {
	case errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	}
	return "unreachable"
}

func ms(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }
