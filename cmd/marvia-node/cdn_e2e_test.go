package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/fallback"
	"github.com/jytt8u/marvia/internal/mux"
	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/relay"
	"github.com/jytt8u/marvia/internal/users"
	"github.com/jytt8u/marvia/internal/vp1"
)

const (
	cdnE2EAdminToken = "тестовый-админский-токен-достаточной-длины"
	cdnE2EPath       = "/assets/app.js"
	cdnE2ECover      = "обычный сайт за CDN"
)

func cdnE2EJSON(t *testing.T, ctx context.Context, httpClient *http.Client, base, method, path, token string, body, out any) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("сериализация запроса: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		t.Fatalf("создание запроса: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("запрос %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		t.Fatalf("запрос %s %s: код %d, тело %q", method, path, resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("разбор ответа %s %s: %v", method, path, err)
		}
	}
}

func startCDNE2EPanel(t *testing.T) *httptest.Server {
	t.Helper()

	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база панели: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Адрес подписки должен родиться в самой панели: собранная тестом ссылка
	// обошла бы ровно ту границу, ради которой нужен сквозной сценарий.
	server := httptest.NewUnstartedServer(nil)
	subBase := "https://" + server.Listener.Addr().String()
	server.Config.Handler = panel.NewAPI(store, cdnE2EAdminToken, subBase, t.TempDir()).Handler()
	server.StartTLS()
	t.Cleanup(server.Close)

	if server.URL != subBase {
		t.Fatalf("адрес панели изменился при запуске: %q вместо %q", server.URL, subBase)
	}
	return server
}

// serveCDNE2EVP1 оставляет боевыми хендшейк, допуск и мультиплексирование, но
// разрешает тестовую цель на 127.0.0.1. Штатный egress ноды намеренно запрещает
// частные сети; без этой узкой подмены автономный тест мог бы ходить лишь в
// интернет, а это сделало бы результат зависимым от чужой инфраструктуры.
func serveCDNE2EVP1(conn net.Conn, nodeKey vp1.KeyPair, registry *users.Registry) {
	defer conn.Close()

	var admission *users.Session
	tunnel, _, err := vp1.ServerHandshake(conn, nodeKey, vp1.NewReplayGuard(vp1.ClockSkew), func(publicKey []byte) error {
		var err error
		admission, err = registry.Admit(users.KindVP1, publicKey, conn.RemoteAddr())
		return err
	})
	if err != nil {
		if admission != nil {
			admission.Close()
		}
		return
	}
	defer tunnel.Close()
	defer admission.Close()

	session, err := mux.Server(tunnel)
	if err != nil {
		return
	}
	defer session.Close()

	for {
		stream, err := mux.Accept(session)
		if err != nil {
			return
		}
		go serveCDNE2EStream(stream)
	}
}

func serveCDNE2EStream(stream net.Conn) {
	defer stream.Close()

	target, kind, err := vp1.ReadRequestOf(stream)
	if err != nil {
		return
	}
	if kind != vp1.KindTCP {
		_ = vp1.WriteStatus(stream, vp1.StatusForbidden)
		return
	}

	upstream, err := net.DialTimeout("tcp", target.String(), 2*time.Second)
	if err != nil {
		_ = vp1.WriteStatus(stream, vp1.StatusUnreachable)
		return
	}
	defer upstream.Close()
	if err := vp1.WriteStatus(stream, vp1.StatusOK); err != nil {
		return
	}
	_ = relay.Bidirectional(stream, upstream)
}

// TestABuyerBehindCDNConnectsWithoutKnowingTheNodeAddress проверяет одно
// обещание целиком: продавец выдаёт доступ, клиент видит только адрес CDN, а
// настоящий HTTP-origin всё же довозит зашифрованный VP1-поток до цели.
func TestABuyerBehindCDNConnectsWithoutKnowingTheNodeAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	coverSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html>"+cdnE2ECover+"</html>")
	}))
	t.Cleanup(coverSite.Close)
	cover, err := fallback.NewReverseProxy(coverSite.URL)
	if err != nil {
		t.Fatalf("сайт-прикрытие: %v", err)
	}

	originTCP, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель ноды: %v", err)
	}
	originAddress := originTCP.Addr().String()
	origin, err := listenWS(originTCP, serverOptions{wsPath: cdnE2EPath, plain: true}, cover)
	if err != nil {
		_ = originTCP.Close()
		t.Fatalf("websocket ноды: %v", err)
	}
	t.Cleanup(func() { _ = origin.Close() })

	originURL, err := url.Parse("http://" + originAddress)
	if err != nil {
		t.Fatalf("адрес origin: %v", err)
	}
	cdnProxy := httputil.NewSingleHostReverseProxy(originURL)
	cdn := httptest.NewTLSServer(cdnProxy)
	t.Cleanup(cdn.Close)

	certificate := cdn.Certificate()
	cdnName := ""
	switch {
	case len(certificate.DNSNames) > 0:
		cdnName = certificate.DNSNames[0]
	case len(certificate.IPAddresses) > 0:
		cdnName = certificate.IPAddresses[0].String()
	default:
		t.Fatal("у сертификата CDN нет имени или адреса")
	}
	cdnAddress := cdn.Listener.Addr().String()

	panelServer := startCDNE2EPanel(t)
	panelClient := panelServer.Client()

	nodeKey, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключ ноды: %v", err)
	}
	var createdNode struct {
		Node  panel.Node `json:"node"`
		Token string     `json:"token"`
	}
	cdnE2EJSON(t, ctx, panelClient, panelServer.URL, http.MethodPost, "/api/v1/nodes", cdnE2EAdminToken, map[string]any{
		"name":       "нода за CDN",
		"address":    cdnAddress,
		"sni":        cdnName,
		"public_key": vp1.EncodeKey(nodeKey.Public),
		"ws_path":    cdnE2EPath,
	}, &createdNode)

	var buyer struct {
		User   panel.User     `json:"user"`
		Issued []panel.Issued `json:"issued"`
		Links  struct {
			Account string `json:"account"`
		} `json:"links"`
	}
	cdnE2EJSON(t, ctx, panelClient, panelServer.URL, http.MethodPost, "/api/v1/users", cdnE2EAdminToken, map[string]any{
		"label":     "покупатель за CDN",
		"max_conns": 4,
		"kinds":     []string{panel.CredVP1},
	}, &buyer)
	if buyer.Links.Account == "" || len(buyer.Issued) != 1 {
		t.Fatalf("панель не выдала полный доступ: %+v", buyer)
	}

	// Нода получает ровно тот публичный ключ, который панель оставила у себя;
	// иначе проверка со свободным допуском доказала бы только работу транспорта.
	var allowed struct {
		Users []users.User `json:"users"`
	}
	cdnE2EJSON(t, ctx, panelClient, panelServer.URL, http.MethodGet, "/api/v1/node/users", createdNode.Token, nil, &allowed)
	if len(allowed.Users) != 1 {
		t.Fatalf("нода получила %d наборов доступа вместо одного", len(allowed.Users))
	}
	registry, err := users.NewRegistry(allowed.Users)
	if err != nil {
		t.Fatalf("реестр ноды: %v", err)
	}

	nodeStopped := make(chan struct{})
	go func() {
		defer close(nodeStopped)
		for {
			conn, err := origin.Accept()
			if err != nil {
				return
			}
			go serveCDNE2EVP1(conn, nodeKey, registry)
		}
	}()
	t.Cleanup(func() {
		_ = origin.Close()
		select {
		case <-nodeStopped:
		case <-time.After(2 * time.Second):
			t.Error("слушатель ноды не остановился")
		}
	})

	account, err := client.ParseAccountLink(buyer.Links.Account)
	if err != nil {
		t.Fatalf("ссылка покупателя: %v", err)
	}
	clientKey, err := vp1.KeyPairFromPrivate(account.PrivateKey)
	if err != nil {
		t.Fatalf("ключ покупателя: %v", err)
	}
	subscription, err := client.FetchSubscriptionWithClient(ctx, account.SubscriptionURL, panelClient)
	if err != nil {
		t.Fatalf("подписка покупателя: %v", err)
	}
	if len(subscription.Nodes) != 1 {
		t.Fatalf("в подписке %d нод вместо одной", len(subscription.Nodes))
	}
	node := subscription.Nodes[0]
	if node.Address != cdnAddress || node.SNI != cdnName || node.WSPath != cdnE2EPath || node.Transport() != client.TransportWS {
		t.Fatalf("клиент получил не websocket-ноду CDN: %+v", node)
	}
	wire, err := json.Marshal(subscription)
	if err != nil {
		t.Fatalf("сериализация подписки: %v", err)
	}
	if strings.Contains(string(wire), originAddress) {
		t.Fatalf("настоящий адрес ноды попал в подписку: %s", wire)
	}

	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	dialer, measurements, err := client.SelectBest(ctx, subscription.Nodes, clientKey, client.Options{RootCAs: roots})
	if err != nil {
		t.Fatalf("клиент не поднял туннель через CDN: %v, замеры %+v", err, measurements)
	}
	defer dialer.Close()

	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("эхо-цель: %v", err)
	}
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		for {
			conn, err := echo.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()

	host, portText, err := net.SplitHostPort(echo.Addr().String())
	if err != nil {
		t.Fatalf("адрес эхо-цели: %v", err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		t.Fatalf("порт эхо-цели: %v", err)
	}
	target, err := vp1.AddressFromHostPort(host, uint16(port))
	if err != nil {
		t.Fatalf("цель VP1: %v", err)
	}
	stream, err := dialer.DialTarget(ctx, target)
	if err != nil {
		t.Fatalf("поток до эхо-цели: %v", err)
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(5 * time.Second))

	payload := []byte("байты прошли через TLS, CDN, WebSocket и VP1")
	if n, err := stream.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("отправка до эхо-цели: записано %d из %d, ошибка %v", n, len(payload), err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(stream, got); err != nil {
		t.Fatalf("ответ эхо-цели: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("эхо-цель вернула %q вместо %q", got, payload)
	}

	// Тот же путь без Upgrade обязан выглядеть как сайт: особый код ответа на
	// нём позволил бы найти туннель простым перебором путей через CDN.
	coverClient := *cdn.Client()
	coverClient.Timeout = 5 * time.Second
	resp, err := coverClient.Get(cdn.URL + cdnE2EPath)
	if err != nil {
		t.Fatalf("обычный запрос через CDN: %v", err)
	}
	defer resp.Body.Close()
	coverBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		t.Fatalf("чтение сайта-прикрытия: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(coverBody), cdnE2ECover) {
		t.Fatalf("путь туннеля без websocket выдал не сайт: код %d, тело %q", resp.StatusCode, coverBody)
	}
}
