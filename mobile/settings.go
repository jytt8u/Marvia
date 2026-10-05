package mobile

import (
	"encoding/json"
	"log"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/foreign"
	"github.com/jytt8u/marvia/internal/securedns"
	"github.com/jytt8u/marvia/internal/tunbridge"
)

// tunnelSettings — то, что человек выбрал в настройках туннеля.
//
// Приходит строкой JSON, а не отдельными параметрами: иначе каждая новая
// настройка меняла бы подпись Start и MeasureNodes, а с ними все вызовы в
// приложении. Незнакомое поле просто не читается, пропущенное значит «как
// было».
type tunnelSettings struct {
	// Fragment — резать TLS-приветствие к нодам так, чтобы имя из SNI не
	// лежало целиком ни в одном TCP-сегменте. Против фильтров по имени.
	Fragment bool `json:"fragment"`

	// NoIPv6 — не пускать IPv6 через туннель вовсе. Маршрут остаётся в
	// туннеле, так что мимо IPv6 тоже не уходит: приложения видят «адресов
	// IPv6 нет» и идут по IPv4.
	NoIPv6 bool `json:"no_ipv6"`

	// DisableReports оставляет замеры на телефоне: панель продавца их не
	// получает. Дозвон и выбор ноды продолжают пользоваться этими замерами.
	DisableReports bool `json:"disable_reports"`

	// PlainDNS — запросы имён как раньше: открытым DNS через туннель, и нода
	// их видит. По умолчанию выключено: имена к известному резолверу уходят
	// по HTTPS, и нода видит только соединение с ним. Хранится как «открыто»,
	// чтобы умолчание — шифровать — было нулём и у приложений, которые этого
	// поля ещё не присылают.
	PlainDNS bool `json:"plain_dns"`
}

// parseSettings разбирает настройки. Сломанная строка — не повод не
// подключаться: туннель поднимется с настройками по умолчанию, а в журнал
// ляжет, что именно не разобралось.
func parseSettings(raw string) tunnelSettings {
	var s tunnelSettings
	if raw == "" {
		return s
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		log.Printf("настройки туннеля не разобрались, беру умолчания: %v", err)
		return tunnelSettings{}
	}
	return s
}

// dial — настройки дозвона до нод VP1.
func (s tunnelSettings) dial() client.Options {
	return client.Options{Fragment: s.Fragment}
}

// foreign — настройки движков чужих протоколов. Свои у каждого движка, а не
// общие на процесс: замер другой подписки не должен переключать работающий
// туннель.
func (s tunnelSettings) foreign() foreign.Options {
	return foreign.Options{Fragment: s.Fragment}
}

// NamesEncrypted говорит, умеет ли ядро ходить к этому резолверу по HTTPS.
// dns — адрес из настроек, с портом или без.
//
// Нужно приложению, чтобы под переключателем «Шифровать запросы имён» была
// правда: свой адрес шифровать нечем. Список знает только ядро, и держать
// его копию в приложении значило бы однажды разойтись — например, на втором
// адресе Cloudflare, который человек вписал как свой.
func NamesEncrypted(dns string) bool {
	_, ok := securedns.For(dns)
	return ok
}

// names — кто отвечает на запросы имён; nil — открытый DNS через туннель,
// как раньше.
//
// Как раньше идут два случая: человек сам выбрал «без шифрования», или
// резолвер — свой адрес, для которого мы не знаем ни имени сертификата, ни
// пути DoH. tunnel — дозвон туннеля, а не что-то в обход него.
func (s tunnelSettings) names(dns string, tunnel tunbridge.Dialer) tunbridge.Names {
	if s.PlainDNS {
		return nil
	}
	servers, ok := securedns.For(dns)
	if !ok {
		return nil
	}
	r, err := securedns.New(securedns.Config{Servers: servers, Dial: tunbridge.DialAddr(tunnel)})
	if err != nil {
		// Сюда попадаем только без списка резолверов или без дозвона — то есть при
		// ошибке в нашем коде, а не в сети. Туннель всё равно поднимаем.
		log.Printf("шифрованные имена не собрались, иду открытым DNS через туннель: %v", err)
		return nil
	}
	return r
}
