package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jytt8u/marvia/internal/redact"
)

const reportTimeout = 10 * time.Second

// Report — отчёт об одной ноде в том виде, в каком его ждёт панель.
type Report struct {
	NodeID    int64 `json:"node_id"`
	OK        bool  `json:"ok"`
	LatencyMS int64 `json:"latency_ms"`

	// FetchMS — за сколько нода отдала пробную порцию (Measurement.Fetch).
	// По нему панель видит то, чего не видит по «ответила или нет»: ноду не
	// закрыли, а режут — подключается, но тянет еле-еле. Ноль — не мерили.
	FetchMS int64 `json:"fetch_ms,omitempty"`
}

// ReportsFrom превращает замеры в отчёты.
//
// Ноды без идентификатора пропускаем: сослаться на них панель не сможет, а
// гадать по имени нельзя — продавец волен переименовать ноду когда угодно.
func ReportsFrom(measurements []Measurement) []Report {
	out := make([]Report, 0, len(measurements))

	for _, m := range measurements {
		if m.Node.ID == 0 || errors.Is(m.Err, ErrNotWaited) {
			continue
		}
		report := Report{NodeID: m.Node.ID, OK: m.OK()}
		if m.OK() {
			report.LatencyMS = m.Latency.Milliseconds()
			if m.Fetch > 0 {
				report.FetchMS = max(1, m.Fetch.Milliseconds())
			}
		}
		out = append(out, report)
	}
	return out
}

// SendReports отправляет отчёты панели.
//
// Отправка — дело необязательное: если панель недоступна, туннель всё равно
// должен работать. Поэтому вызывающий вправе просто записать ошибку в журнал
// и жить дальше.
func SendReports(ctx context.Context, subURL string, reports []Report) error {
	if len(reports) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()

	err := sendReports(ctx, subURL, reports)
	if !errors.Is(err, errReportRejected) {
		return err
	}
	// Панель старше поля fetch_ms разбирает отчёт строго и отвечает 400 на
	// незнакомое поле. Отчёт без него ей понятен — лучше половина сведений,
	// чем ни одного: по «ответила или нет» она ещё ставит ноды по порядку.
	plain := make([]Report, len(reports))
	for i, r := range reports {
		r.FetchMS = 0
		plain[i] = r
	}
	return sendReports(ctx, subURL, plain)
}

// errReportRejected — панель не разобрала отчёт (400).
var errReportRejected = errors.New("панель не разобрала отчёт")

func sendReports(ctx context.Context, subURL string, reports []Report) error {
	payload, err := json.Marshal(map[string]any{"reports": reports})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, subURL+"/report", bytes.NewReader(payload))
	if err != nil {
		return redact.Wrap(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("отправка отчётов: %w", withoutSecret(err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusBadRequest {
		return errReportRejected
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("панель ответила %s", resp.Status)
	}
	return nil
}
