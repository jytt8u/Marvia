package panel

import (
	"fmt"
	"strings"
)

// Fingerprints — чьё TLS-приветствие клиент изображает, подключаясь к ноде.
//
// Имена те же, что у параметра fp в ссылках Xray, поэтому одно поле ноды
// годится и нашему клиенту, и чужим приложениям. Пусто — Chrome, как было.
//
// Зачем выбирать. По наблюдениям 2026 года (Xray-core #6293, Хабр 1047442)
// фильтр учитывает отпечаток приветствия вместе с адресом и частотой
// подключений, и одни отпечатки на одних сетях проходят, а другие — нет.
// Источник у этих наблюдений один, поэтому отпечаток по умолчанию не меняем,
// а даём продавцу выбрать для каждой ноды. Клиент сам его не перебирает:
// смена отпечатка сразу после заморозки, по тем же наблюдениям, удлиняет её.
var Fingerprints = []string{"chrome", "firefox", "safari", "ios", "android", "edge", "qq", "360"}

// normalFingerprint приводит выбор продавца к имени из списка.
func normalFingerprint(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return "", nil
	}
	for _, known := range Fingerprints {
		if name == known {
			return name, nil
		}
	}
	return "", fmt.Errorf("отпечаток %q: допустимы %s", raw, strings.Join(Fingerprints, ", "))
}
