package panel

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Подписка в форматах sing-box и Clash (mihomo).
//
// Ссылки vless:// и trojan:// понимают v2rayNG, Hiddify, Happ и v2RayTun, но
// не sing-box (SFA, SFI, SFM), Stash, Clash Verge и Clash Meta: им нужен
// готовый конфиг. Без него покупатель с iPhone или Mac, у которого стоит
// именно такое приложение, остаётся без доступа — а свой клиент под iOS у
// проекта появится не скоро.
//
// Формат выбирается только явным ?format=singbox или ?format=clash. Узнавать
// приложение по User-Agent нельзя: Hiddify, например, представляется
// «…ClashMeta… sing-box», и получил бы не тот формат.
//
// Наш VP1 эти приложения не знают, поэтому в конфиг попадают те же VLESS и
// Trojan, что и в обычную подписку: по одному выходу на ноду и вид доступа.

// stockEndpoint — одна точка входа для чужого приложения.
type stockEndpoint struct {
	Name     string
	Kind     string // vless или trojan
	Server   string
	Port     int
	Secret   string
	SNI      string
	FP       string
	Reality  bool
	PBK, SID string
	WSPath   string
}

func stockEndpoints(nodes []Node, creds []Credential, label string) []stockEndpoint {
	var out []stockEndpoint
	seen := map[string]int{}
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		host, portText, err := net.SplitHostPort(n.Address)
		if err != nil {
			continue
		}
		port, err := strconv.Atoi(portText)
		if err != nil {
			continue
		}
		base := NodeTitle(n)
		if label != "" {
			base += " · " + label
		}
		for _, c := range creds {
			if c.Kind != CredVLESS && c.Kind != CredTrojan {
				continue
			}
			// Имена выходов в обоих форматах обязаны быть уникальными, иначе
			// конфиг не загрузится целиком. Две записи для одной ноды (vless и
			// trojan) различаем видом, совпавшие имена нод — номером.
			name := base + " · " + c.Kind
			seen[name]++
			if seen[name] > 1 {
				name += " " + strconv.Itoa(seen[name])
			}
			out = append(out, stockEndpoint{
				Name: name, Kind: c.Kind, Server: host, Port: port, Secret: c.Secret,
				SNI: n.SNI, FP: fingerprintOf(n),
				Reality: n.RealityPublicKey != "" && n.WSPath == "",
				PBK:     n.RealityPublicKey, SID: n.RealityShortID,
				WSPath: n.WSPath,
			})
		}
	}
	return out
}

// singBoxConfig — полный профиль sing-box (1.12 и новее): TUN, DNS через
// туннель по HTTPS, выбор ноды и автовыбор по задержке.
func singBoxConfig(eps []stockEndpoint) ([]byte, error) {
	type m = map[string]any
	var outbounds []any
	var tags []string
	for _, e := range eps {
		tls := m{"enabled": true, "utls": m{"enabled": true, "fingerprint": e.FP}}
		if e.SNI != "" {
			tls["server_name"] = e.SNI
		}
		if e.Reality {
			tls["reality"] = m{"enabled": true, "public_key": e.PBK, "short_id": e.SID}
		}
		ob := m{"type": e.Kind, "tag": e.Name, "server": e.Server, "server_port": e.Port, "tls": tls}
		if e.Kind == CredVLESS {
			ob["uuid"] = e.Secret
		} else {
			ob["password"] = e.Secret
		}
		if e.WSPath != "" {
			tr := m{"type": "ws", "path": e.WSPath}
			if e.SNI != "" {
				tr["headers"] = m{"Host": e.SNI}
			}
			ob["transport"] = tr
		}
		outbounds = append(outbounds, ob)
		tags = append(tags, e.Name)
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("нет ни одной ноды с доступом VLESS или Trojan")
	}
	groups := []any{
		m{"type": "selector", "tag": "proxy", "outbounds": append([]string{"auto"}, tags...), "default": "auto"},
		m{"type": "urltest", "tag": "auto", "outbounds": tags},
	}
	cfg := m{
		"log": m{"level": "warn"},
		// Имена спрашиваем через туннель по HTTPS: иначе их видит провайдер,
		// а без шифрования — и нода. Имена самих нод — системным резолвером,
		// иначе до первой ноды не дойти.
		"dns": m{
			"servers": []any{
				m{"type": "https", "tag": "remote", "server": "1.1.1.1", "detour": "proxy"},
				m{"type": "local", "tag": "local"},
			},
			"final": "remote",
		},
		"inbounds": []any{
			m{"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
				"auto_route": true, "strict_route": true},
		},
		"outbounds": append(append(groups, outbounds...), m{"type": "direct", "tag": "direct"}),
		"route": m{
			"rules": []any{
				m{"action": "sniff"},
				m{"protocol": "dns", "action": "hijack-dns"},
				m{"ip_is_private": true, "outbound": "direct"},
			},
			"final":                   "proxy",
			"auto_detect_interface":   true,
			"default_domain_resolver": "local",
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// clashConfig — профиль Clash Meta (mihomo), его понимают Stash, Clash Verge
// и FlClash. YAML пишем сами: строки — в кавычках JSON, а JSON-строка — это
// корректная строка YAML, так что кавычки, двоеточия и кириллица в именах
// ничего не ломают. Тянуть ради этого библиотеку YAML незачем.
func clashConfig(eps []stockEndpoint) ([]byte, error) {
	if len(eps) == 0 {
		return nil, fmt.Errorf("нет ни одной ноды с доступом VLESS или Trojan")
	}
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	var b strings.Builder
	b.WriteString("mixed-port: 7890\nallow-lan: false\nmode: rule\nlog-level: warning\n")
	b.WriteString("dns:\n  enable: true\n  enhanced-mode: fake-ip\n  nameserver:\n    - " + q("https://1.1.1.1/dns-query") + "\n")
	b.WriteString("proxies:\n")
	for _, e := range eps {
		fmt.Fprintf(&b, "  - name: %s\n    type: %s\n    server: %s\n    port: %d\n", q(e.Name), e.Kind, q(e.Server), e.Port)
		if e.Kind == CredVLESS {
			fmt.Fprintf(&b, "    uuid: %s\n    tls: true\n", q(e.Secret))
			if e.SNI != "" {
				fmt.Fprintf(&b, "    servername: %s\n", q(e.SNI))
			}
		} else {
			fmt.Fprintf(&b, "    password: %s\n", q(e.Secret))
			if e.SNI != "" {
				fmt.Fprintf(&b, "    sni: %s\n", q(e.SNI))
			}
		}
		fmt.Fprintf(&b, "    udp: true\n    client-fingerprint: %s\n", q(e.FP))
		switch {
		case e.WSPath != "":
			fmt.Fprintf(&b, "    network: ws\n    ws-opts:\n      path: %s\n", q(e.WSPath))
			if e.SNI != "" {
				fmt.Fprintf(&b, "      headers:\n        Host: %s\n", q(e.SNI))
			}
		case e.Reality:
			fmt.Fprintf(&b, "    network: tcp\n    reality-opts:\n      public-key: %s\n      short-id: %s\n", q(e.PBK), q(e.SID))
		default:
			b.WriteString("    network: tcp\n")
		}
	}
	b.WriteString("proxy-groups:\n")
	fmt.Fprintf(&b, "  - name: %s\n    type: select\n    proxies:\n      - %s\n", q("Marvia"), q("Авто"))
	for _, e := range eps {
		fmt.Fprintf(&b, "      - %s\n", q(e.Name))
	}
	fmt.Fprintf(&b, "  - name: %s\n    type: url-test\n    url: %s\n    interval: 300\n    proxies:\n", q("Авто"), q("https://www.gstatic.com/generate_204"))
	for _, e := range eps {
		fmt.Fprintf(&b, "      - %s\n", q(e.Name))
	}
	b.WriteString("rules:\n  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve\n  - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve\n  - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve\n")
	b.WriteString("  - MATCH,Marvia\n")
	return []byte(b.String()), nil
}
