package mobile

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/jytt8u/marvia/internal/vp1"
)

// TestRussianListGoesToThePanelThroughTheTunnel: при поднятом туннеле список
// просят у панели через ноду, по имени панели, — а не напрямую, где её адрес
// могут резать. Сам ответ тут не важен: важно, куда ушёл запрос.
func TestRussianListGoesToThePanelThroughTheTunnel(t *testing.T) {
	var asked []vp1.Address
	backend := subscriptionBackend{dial: func(_ context.Context, target vp1.Address) (net.Conn, error) {
		asked = append(asked, target)
		return nil, errors.New("дальше не нужно")
	}}
	tunnel := &Tunnel{dialer: backend, running: true}
	link := "marvia://YH3odubQhSmWQfCRwteeGN6pehcHq3DDcX_uGCvfl_Q@panel.invalid/sub/test"

	if _, err := tunnel.BypassRoutes(link); err == nil {
		t.Fatal("запрос без ответа панели не вернул ошибку")
	}
	if len(asked) == 0 {
		t.Fatal("список пошёл мимо туннеля")
	}
	for _, a := range asked {
		if a.Host != "panel.invalid" || a.Type != vp1.AtypDomain || a.Port != 443 {
			t.Errorf("туннель просили не о панели по имени: %+v", a)
		}
	}
}

// TestStoppedTunnelDoesNotFetchTheList: остановленный туннель не делает
// вид, что качает, — приложение тогда пробует напрямую.
func TestStoppedTunnelDoesNotFetchTheList(t *testing.T) {
	tunnel := &Tunnel{}
	if _, err := tunnel.BypassRoutes("marvia://YH3odubQhSmWQfCRwteeGN6pehcHq3DDcX_uGCvfl_Q@panel.invalid/sub/test"); err == nil {
		t.Fatal("остановленный туннель вернул список")
	}
}
