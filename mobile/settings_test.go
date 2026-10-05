package mobile

import (
	"testing"

	"github.com/jytt8u/marvia/internal/tunbridge"
)

// TestBrokenSettingsDoNotStopTheTunnel: сломанная строка настроек означает
// умолчания, а не отказ подключаться; незнакомое поле — не ошибка.
func TestBrokenSettingsDoNotStopTheTunnel(t *testing.T) {
	if got := parseSettings("{не json"); got != (tunnelSettings{}) {
		t.Fatalf("сломанные настройки дали %+v", got)
	}
	got := parseSettings(`{"fragment":true,"no_ipv6":true,"disable_reports":true,"из_будущего":1}`)
	if !got.Fragment || !got.NoIPv6 || !got.DisableReports {
		t.Fatalf("настройки не прочитались: %+v", got)
	}
	if !got.dial().Fragment {
		t.Fatal("дробление не дошло до дозвона нод")
	}
}

// TestKnownResolverIsEncryptedByDefault: по умолчанию — и у приложения,
// которое о шифровании имён ещё не знает и поля не присылает, — имена к
// известному резолверу идут по HTTPS. «Без шифрования» и свой адрес остаются
// открытым DNS через туннель, как раньше.
func TestKnownResolverIsEncryptedByDefault(t *testing.T) {
	old := parseSettings(`{"fragment":false}`)
	if old.names("1.1.1.1:53", tunbridge.DialerFunc{}) == nil {
		t.Fatal("известный резолвер по умолчанию не шифруется")
	}
	if parseSettings(`{"plain_dns":true}`).names("1.1.1.1:53", tunbridge.DialerFunc{}) != nil {
		t.Fatal("«без шифрования» не послушались")
	}
	if old.names("76.76.2.0:53", tunbridge.DialerFunc{}) != nil {
		t.Fatal("свой адрес без известного DoH выдан за шифрованный")
	}
	// Приложение спрашивает то же самое для надписи под переключателем.
	if !NamesEncrypted("1.0.0.1") || NamesEncrypted("76.76.2.0") {
		t.Fatal("надпись под переключателем разошлась с тем, что делает ядро")
	}
}
