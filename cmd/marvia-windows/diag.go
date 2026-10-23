package main

// «Проверить всё» в разделе «Диагностика».
//
// Человек с «не работает» раньше видел зелёное «Подключено» и не знал, куда
// смотреть дальше: интернет ли у него вообще, нода ли молчит, браузер ли
// мимо туннеля. Проверка идёт по шагам от провода к сайту, и у каждого
// плохого шага — то, что можно нажать прямо здесь. Шаг, где нажать нечего,
// кнопки не получает: «Починить», которое ничего не чинит, хуже честного
// «проверьте кабель».
//
// Программа отдаёт коды и числа, а слова — у окна, на его языке: так шаг
// не говорит по-русски в английском окне, а тест сверяет, что у каждого кода
// есть слова в обоих словарях и кнопка с делом.

// diagSteps — шаги по порядку: от того, что не зависит от Marvia, к тому, что
// зависит. Окно спрашивает их по одному и показывает каждый, как только он
// ответил: проверка сайтов идёт секунды, и пустой экран всё это время
// выглядел бы зависанием.
var diagSteps = []string{"net", "dns", "node", "tunnel", "system", "where", "leak", "sites", "interference"}

// diagActs — что окно умеет сделать по плохому шагу. За каждым кодом в окне
// стоит кнопка с настоящим действием; код без неё тест не пропустит.
var diagActs = []string{"fix", "nodes", "fragment", "dns", "reconnect", "connect", "proxy", "killswitch"}

// Состояние шага. Off — проверять нечего: сайты через туннель, пока туннеля
// нет, — это не беда, а пропуск.
const (
	diagOK   = "ok"
	diagWarn = "warn"
	diagBad  = "bad"
	diagOff  = "off"
)

// diagSlowMS — отклик ноды, после которого стоит предложить другую: дальше
// трёхсот миллисекунд страницы ощутимо подтормаживают, тот же порог, что у
// списка стран («медленно»).
const diagSlowMS = 300

// DiagStep — итог одного шага.
type DiagStep struct {
	ID string `json:"id"`
	St string `json:"st"`
	// MS — время ответа; N из Of — сколько ответило из скольких.
	MS int64 `json:"ms,omitempty"`
	N  int   `json:"n,omitempty"`
	Of int   `json:"of,omitempty"`
	// IP и Country — под каким адресом и в какой стране видит интернет.
	IP      string `json:"ip,omitempty"`
	Country string `json:"country,omitempty"`
	// Detail — уже словами программы: причина отказа подключения, названия
	// мешающих программ. Их знает только она, и на язык окна она их уже
	// перевела (say).
	Detail []string `json:"detail,omitempty"`
	// Act — коды кнопок из diagActs; у хорошего шага их нет.
	Act []string `json:"act,omitempty"`
	// Leak — какой путь мимо туннеля нашёлся: v4 — браузер виден под другим
	// адресом, v6 — по IPv6 соединение уходит наружу, хотя в туннеле IPv6
	// выключен. Самого адреса здесь нет: снимок окна человек пересылает
	// продавцу, и его настоящий адрес туда попадать не должен.
	Leak string `json:"leak,omitempty"`
}

// judgeNet — интернет мимо VPN. Нет его — Marvia тут ничего не починит, и
// кнопки нет: только слова про кабель и Wi-Fi.
func judgeNet(ok bool, ms int64) DiagStep {
	if !ok {
		return DiagStep{ID: "net", St: diagBad}
	}
	return DiagStep{ID: "net", St: diagOK, MS: ms}
}

// judgeDNS — отвечают ли запросы имён так, как их задаёт браузер. С туннелем
// они идут к резолверу из настроек — его и предлагаем сменить. Без туннеля
// отвечает резолвер провайдера, и лучшее, что можно сделать, — подключиться:
// имена пойдут через туннель.
func judgeDNS(ok, up bool, ms int64) DiagStep {
	switch {
	case ok:
		return DiagStep{ID: "dns", St: diagOK, MS: ms}
	case up:
		return DiagStep{ID: "dns", St: diagBad, Act: []string{"dns"}}
	default:
		return DiagStep{ID: "dns", St: diagBad, Act: []string{"connect"}}
	}
}

// judgeNode — отвечают ли ноды по TCP мимо туннеля. Молчат все — сменить
// не на что, разве что подключиться заново позже; молчит часть или отклик
// большой — на «Серверах» есть другая.
func judgeNode(answered, total int, best int64) DiagStep {
	s := DiagStep{ID: "node", N: answered, Of: total, MS: best}
	switch {
	case total == 0:
		s.St = diagOff
	case answered == 0:
		s.St = diagBad
		s.MS = 0
		if total > 1 {
			s.Act = []string{"nodes"}
		}
	case answered < total || best > diagSlowMS:
		s.St = diagWarn
		s.Act = []string{"nodes"}
	default:
		s.St = diagOK
	}
	return s
}

// judgeTunnel — поднят ли туннель. Не поднялся — попробовать ещё раз и, если
// дробление выключено, включить его: провайдер, режущий по имени, — самая
// частая причина, по которой нода жива, а хендшейк не проходит.
func judgeTunnel(state string, fragment bool, reason string) DiagStep {
	s := DiagStep{ID: "tunnel"}
	if reason != "" {
		s.Detail = []string{reason}
	}
	switch state {
	case "connected":
		s.St = diagOK
		s.Detail = nil
	case "connecting", "disconnecting":
		s.St = diagWarn
	case "stalled":
		s.St = diagBad
		s.Act = []string{"reconnect", "nodes"}
	case "failed":
		s.St = diagBad
		s.Act = []string{"connect"}
		if !fragment {
			s.Act = append(s.Act, "fragment")
		}
	default:
		s.St = diagOff
		s.Act = []string{"connect"}
	}
	return s
}

// judgeSystem — открывает ли страницу обычная программа, через адаптер и DNS
// Windows. Нет, а через ядро туннеля да — мешает что-то на компьютере, и
// «Починить» как раз про это. Нет обоими путями — нода.
func judgeSystem(up, systemOK, tunnelOK bool) DiagStep {
	switch {
	case !up:
		return DiagStep{ID: "system", St: diagOff}
	case systemOK:
		return DiagStep{ID: "system", St: diagOK}
	case tunnelOK:
		return DiagStep{ID: "system", St: diagBad, Act: []string{"fix"}}
	default:
		return DiagStep{ID: "system", St: diagBad, Act: []string{"nodes"}}
	}
}

// judgeWhere — под каким адресом человека видит интернет.
func judgeWhere(up bool, ip, country string) DiagStep {
	switch {
	case !up:
		return DiagStep{ID: "where", St: diagOff}
	case ip == "":
		return DiagStep{ID: "where", St: diagBad, Act: []string{"nodes"}}
	default:
		return DiagStep{ID: "where", St: diagOK, IP: ip, Country: country}
	}
}

// judgeLeak — не виден ли человек под своим адресом. Сравниваются два
// ответа: как видит интернет обычную программу (маршруты Windows) и как —
// туннель. Разные адреса значат, что часть трафика идёт мимо VPN, и сайт
// видит настоящий адрес. Отдельно — IPv6: если в туннеле он выключен, а
// соединение по нему наружу всё же открылось, это та же утечка другим путём.
// Чинит обе kill switch: пока туннель включён, мимо него не выпускается
// ничего.
func judgeLeak(up bool, tunnelIP, systemIP string, v6Leak bool) DiagStep {
	s := DiagStep{ID: "leak"}
	switch {
	case !up:
		s.St = diagOff
	case v6Leak:
		s.St, s.Leak, s.Act = diagBad, "v6", []string{"killswitch", "fix"}
	case systemIP == "" || tunnelIP == "":
		s.St, s.Act = diagWarn, []string{"reconnect"}
	case systemIP != tunnelIP:
		s.St, s.Leak, s.Act = diagBad, "v4", []string{"killswitch", "fix"}
	default:
		s.St = diagOK
	}
	return s
}

// judgeSites — сколько главных сайтов открылось через туннель. Туннель
// поднят, а сайт не открылся — дело в выходе ноды, а не в пути до неё:
// помогает другая нода, а не дробление.
func judgeSites(up bool, opened, total int) DiagStep {
	s := DiagStep{ID: "sites", N: opened, Of: total}
	switch {
	case !up:
		s.St = diagOff
	case opened == total:
		s.St = diagOK
	case opened == 0:
		s.St = diagBad
		s.Act = []string{"nodes"}
	default:
		s.St = diagWarn
		s.Act = []string{"nodes"}
	}
	return s
}

// judgeInterference — помехи рядом: второй VPN, Hiddify, WARP и системный
// прокси. Прокси из переменных окружения снять нечем — его кнопку не даём.
func judgeInterference(conflicts []string, proxy string, proxyEnv bool) DiagStep {
	s := DiagStep{ID: "interference", St: diagOK, N: len(conflicts), Detail: conflicts}
	if len(conflicts) > 0 {
		s.St = diagBad
		s.Act = append(s.Act, "fix")
	}
	if proxy != "" {
		s.Detail = append(s.Detail, proxy)
		if s.St == diagOK {
			s.St = diagWarn
		}
		if !proxyEnv {
			s.Act = append(s.Act, "proxy")
		}
	}
	return s
}
