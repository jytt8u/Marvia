package panel

import (
	"fmt"
	"net/http"
	"strings"

	"rsc.io/qr"
)

// maxQRText — длиннее ссылки доступа с подпиской текст в QR не бывает; больше —
// уже не ссылка, а попытка нагрузить панель.
const maxQRText = 2048

// qrCode рисует QR-код ссылки для продавца — чтобы покупатель навёл камеру
// или сохранил картинку, а не переписывал строку.
//
// Рисует панель, а не страница: в браузере для этого нужна была бы чужая
// библиотека, а строгий CSP панели не пускает скрипты со стороны. И ссылка
// с секретом не уходит ни в какой внешний сервис картинок.
func (a *API) qrCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &body) {
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" || len(text) > maxQRText {
		fail(w, http.StatusBadRequest, "для QR-кода нужен текст до 2048 байт")
		return
	}
	svg, err := qrSVG(text)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	// Ссылка внутри — тот же доступ, что и в тексте: не кешируем.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(svg))
}

// qrSVG — QR-код одним путём SVG: квадрат на модуль, поле в четыре модуля,
// как требует стандарт, иначе камеры телефонов находят код хуже.
func qrSVG(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", fmt.Errorf("QR-код не собрался: %w", err)
	}
	const quiet = 4
	size := code.Size + 2*quiet
	var path strings.Builder
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&path, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+
		`<rect width="100%%" height="100%%" fill="#fff"/><path fill="#000" d="%s"/></svg>`, size, size, path.String()), nil
}
