//go:build windows

package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

// directBackend — «туннель», который ходит напрямую и считает, сколько раз
// через него ходили: проверки обязаны идти через ядро, а не мимо.
type directBackend struct {
	slowBackend
	dials atomic.Int32
}

func (b *directBackend) DialTarget(ctx context.Context, t vp1.Address) (net.Conn, error) {
	b.dials.Add(1)
	var d net.Dialer
	return d.DialContext(ctx, "tcp", t.String())
}

func connectedTo(b client.Backend) *Controller {
	return &Controller{state: StateConnected, dialer: b, log: newJournal()}
}

// TestWhereShowsTheAddressSeenThroughTheTunnel: адрес и страна берутся через
// ядро туннеля — то, что видит интернет, а не то, что у человека дома.
func TestWhereShowsTheAddressSeenThroughTheTunnel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fl=1\nip=95.216.1.2\nloc=FI\ntls=TLSv1.3\n"))
	}))
	defer srv.Close()
	old := whereURL
	whereURL = srv.URL
	defer func() { whereURL = old }()

	b := &directBackend{slowBackend: slowBackend{nodes: []client.Node{{ID: 1}}}}
	v := connectedTo(b).Where(context.Background())
	if v.IP != "95.216.1.2" || v.Country != "fi" || b.dials.Load() == 0 {
		t.Fatalf("где: %+v, через ядро %d раз", v, b.dials.Load())
	}
}

// TestSiteCheckTellsAnOpenSiteFromADeadOne: открывшийся сайт — с временем,
// неоткрывшийся — «нет», и одна мёртвая проверка не валит остальные.
func TestSiteCheckTellsAnOpenSiteFromADeadOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/login")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	dead, _ := net.Listen("tcp", "127.0.0.1:0")
	deadAddr := dead.Addr().String()
	_ = dead.Close()
	oldList, oldScheme := siteChecks, siteScheme
	siteChecks = []struct{ Name, Host string }{{"Живой", strings.TrimPrefix(srv.URL, "http://")}, {"Мёртвый", deadAddr}}
	siteScheme = "http://"
	defer func() { siteChecks, siteScheme = oldList, oldScheme }()

	b := &directBackend{slowBackend: slowBackend{nodes: []client.Node{{ID: 1}}}}
	sites, err := connectedTo(b).CheckSites(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !sites[0].OK || sites[0].MS <= 0 || sites[1].OK {
		t.Fatalf("сайты: %+v", sites)
	}
}

// TestSpeedIsCountedFromWhatArrived: скорость — по пришедшим байтам, и без
// туннеля замер не начинается вовсе: трафик тратить не на что.
func TestSpeedIsCountedFromWhatArrived(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 2<<20))
	}))
	defer srv.Close()
	old := speedURL
	speedURL = srv.URL
	defer func() { speedURL = old }()

	b := &directBackend{slowBackend: slowBackend{nodes: []client.Node{{ID: 1}}}}
	v, err := connectedTo(b).SpeedTest(context.Background())
	if err != nil || v.Bytes != 2<<20 || v.Mbps <= 0 {
		t.Fatalf("скорость: %+v, %v", v, err)
	}
	if _, err := (&Controller{state: StateIdle, log: newJournal()}).SpeedTest(context.Background()); err == nil {
		t.Fatal("замер скорости без туннеля начался")
	}
}
