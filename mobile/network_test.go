package mobile

import (
	"errors"
	"testing"
)

func TestRecoveryDoesNotKeepWarningFromLostConnection(t *testing.T) {
	tun := &Tunnel{}
	tun.note(errors.New("сеть потеряна"))
	tun.trouble("network-changing")
	tun.recovered()
	if tun.Trouble() != "" || tun.LastError() != "" {
		t.Fatal("рабочий туннель сохранил предупреждение прежней сети")
	}
}
