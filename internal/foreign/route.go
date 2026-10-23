package foreign

// Route — как устроено соединение с чужой нодой: протокол, транспорт,
// защита, имя прикрытия и отпечаток TLS.
//
// Окно на ПК показывает это в «Диагностике» рядом с тем же для ключа Marvia.
// Раньше чужая нода выглядела там как «VP1 · TLS»: у её client.Node нет ни
// маскировки, ни имени, и окно додумывало. Берём ровно то, что разбор ссылки
// положил в настройки Xray, — то, с чем движок и дозванивается.
type Route struct {
	// Protocol — vless, vmess, trojan, shadowsocks, hysteria2, wireguard.
	Protocol string
	// Network — транспорт Xray: raw, ws, grpc, httpupgrade, xhttp, hysteria.
	Network string
	// Security — tls, reality или none.
	Security string
	// SNI — имя в приветствии; Fingerprint — чьё приветствие изображается.
	SNI         string
	Fingerprint string
}

// Route достаёт описание соединения из ссылки. Чего в ссылке нет, то пусто:
// у WireGuard и Shadowsocks нет ни SNI, ни отпечатка.
func (l Link) Route() Route {
	r := Route{Protocol: l.Protocol}
	stream, _ := l.Outbound["streamSettings"].(map[string]any)
	if stream == nil {
		return r
	}
	r.Network, _ = stream["network"].(string)
	r.Security, _ = stream["security"].(string)
	for _, key := range []string{"realitySettings", "tlsSettings"} {
		if t, ok := stream[key].(map[string]any); ok {
			r.SNI, _ = t["serverName"].(string)
			r.Fingerprint, _ = t["fingerprint"].(string)
			break
		}
	}
	return r
}

// Route — описание соединения текущей ноды; пусто, пока движок не поднят.
func (s *Supervisor) Route() Route {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine == nil {
		return Route{}
	}
	return s.engine.Link().Route()
}
