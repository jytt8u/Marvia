package securedns

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

// Разбор и сборка сообщений DNS.
//
// Разбирает x/net/dns/dnsmessage, а не самодельный код: сообщение приходит от
// любого приложения на устройстве и от резолвера из интернета, и ошибка в
// ручном разборе здесь — это паника моста на кривом пакете.

const (
	// maxMessage — больше в DNS не бывает: длина в TCP-заголовке двухбайтовая.
	maxMessage = 65535

	// classicUDP — сколько влезает в датаграмму клиенту без EDNS (RFC 1035).
	classicUDP = 512

	// optPadding — код опции EDNS(0) «добивка» (RFC 7830).
	optPadding = 12

	// paddingBlock — до скольких байт добиваем запрос (RFC 8467, рекомендация
	// для клиента). Нода не видит имён, но видит длину TLS-записей, а длина
	// запроса без добивки — это почти длина имени: короткий список популярных
	// сайтов по ней угадывается. С добивкой все обычные запросы весят одинаково.
	paddingBlock = 128
)

var errNotQuery = errors.New("это не запрос имени")

// query — запрос приложения в разобранном виде.
type query struct {
	msg dnsmessage.Message

	// key — по нему ответ лежит в кэше; пусто — ответ не кэшируется.
	key string

	// edns — прислало ли приложение EDNS. Если нет, в ответе его быть не
	// должно (RFC 6891, 7): старый разборщик на нём споткнётся.
	edns bool

	// limit — сколько байт ответа влезает в датаграмму этому приложению.
	limit int
}

func parseQuery(raw []byte) (*query, error) {
	var m dnsmessage.Message
	if err := m.Unpack(raw); err != nil {
		return nil, fmt.Errorf("запрос имени не разобрался: %w", err)
	}
	if m.Header.Response {
		return nil, errNotQuery
	}
	q := &query{msg: m, limit: classicUDP}
	dnssec := false
	for _, rr := range m.Additionals {
		if rr.Header.Type == dnsmessage.TypeOPT {
			q.edns = true
			dnssec = rr.Header.DNSSECAllowed()
			q.limit = max(classicUDP, int(rr.Header.Class))
		}
	}
	// Кэшируем только обычный вопрос про одно имя. Флаги DNSSEC входят в
	// ключ: с ними ответ другой — с подписями или без проверки.
	if len(m.Questions) == 1 && m.Header.OpCode == 0 {
		qq := m.Questions[0]
		q.key = fmt.Sprintf("%s|%d|%d|%t|%t",
			strings.ToLower(qq.Name.String()), qq.Type, qq.Class, dnssec, m.Header.CheckingDisabled)
	}
	return q, nil
}

// outgoing собирает запрос к резолверу.
//
// ID — ноль (RFC 8484, 4.1): по HTTPS он не нужен для сопоставления, а
// одинаковые запросы с одинаковым ID кэшируются по пути одинаково. EDNS —
// всегда, ради добивки; если приложение его не присылало, из ответа он
// потом будет убран (см. reply).
func (q *query) outgoing() ([]byte, error) {
	m := q.msg
	m.Header.ID = 0
	m.Additionals = nil

	var opt dnsmessage.Resource
	found := false
	for _, rr := range q.msg.Additionals {
		if rr.Header.Type != dnsmessage.TypeOPT {
			m.Additionals = append(m.Additionals, rr)
			continue
		}
		opt = withoutPadding(rr)
		found = true
	}
	if !found {
		if err := opt.Header.SetEDNS0(maxMessage, dnsmessage.RCodeSuccess, false); err != nil {
			return nil, err
		}
		opt.Body = &dnsmessage.OPTResource{}
	}
	// Размер ответа объявляем наибольший. По HTTPS резать ответ незачем, а
	// размер датаграммы приложения резолвера не касается: в него ответ
	// укладывает reply. Объяви мы размер приложения — резолвер мог бы
	// прислать обрезанный ответ, приложение повторило бы вопрос по TCP с тем
	// же размером и получило бы обрезанный снова.
	opt.Header.Class = dnsmessage.Class(maxMessage)
	body := opt.Body.(*dnsmessage.OPTResource)
	m.Additionals = append(m.Additionals, opt)

	bare, err := m.Pack()
	if err != nil {
		return nil, fmt.Errorf("сборка запроса: %w", err)
	}
	// Опция добавляет четыре байта заголовка и сколько надо нулей.
	pad := (paddingBlock - (len(bare)+4)%paddingBlock) % paddingBlock
	body.Options = append(body.Options, dnsmessage.Option{Code: optPadding, Data: make([]byte, pad)})
	return m.Pack()
}

// parseAnswer разбирает ответ резолвера и проверяет, что он на наш вопрос.
//
// Проверка не от недоверия к резолверу — соединение с ним проверено
// сертификатом, — а от путаницы: ответ на чужой вопрос, попавший в кэш,
// ломал бы имя до истечения TTL.
func parseAnswer(raw []byte, q *query) (dnsmessage.Message, error) {
	var m dnsmessage.Message
	if err := m.Unpack(raw); err != nil {
		return m, fmt.Errorf("ответ резолвера не разобрался: %w", err)
	}
	if !m.Header.Response {
		return m, errors.New("резолвер прислал не ответ")
	}
	if len(m.Questions) != len(q.msg.Questions) {
		return m, errors.New("ответ резолвера не на тот вопрос")
	}
	for i, got := range m.Questions {
		want := q.msg.Questions[i]
		if got.Type != want.Type || got.Class != want.Class || !strings.EqualFold(got.Name.String(), want.Name.String()) {
			return m, errors.New("ответ резолвера не на тот вопрос")
		}
	}
	// Добивку резолвера выбрасываем сразу: приложению она ни к чему, а в
	// кэше занимала бы место.
	for i, rr := range m.Additionals {
		if rr.Header.Type == dnsmessage.TypeOPT {
			m.Additionals[i] = withoutPadding(rr)
		}
	}
	return m, nil
}

// reply собирает ответ приложению из ответа резолвера (или из кэша).
//
// udp — ответ поедет датаграммой. Если он не влезает в объявленный
// приложением размер, уходит пустой ответ с флагом TC: приложение повторит
// вопрос по TCP, и мост ответит на него так же, через резолвер. Обрезать
// записи по живому нельзя — половина адресов выглядит как полный ответ.
func (q *query) reply(answer dnsmessage.Message, udp bool) ([]byte, error) {
	m := answer
	m.Header.ID = q.msg.Header.ID
	// Вопрос — дословно тот, что задало приложение: ответ мог прийти из кэша
	// на то же имя в другом регистре, а некоторые разборщики сверяют вопрос
	// побайтно.
	m.Questions = q.msg.Questions
	m.Additionals = nil
	for _, rr := range answer.Additionals {
		if rr.Header.Type == dnsmessage.TypeOPT && !q.edns {
			continue
		}
		m.Additionals = append(m.Additionals, rr)
	}
	out, err := m.Pack()
	if err != nil {
		return nil, fmt.Errorf("сборка ответа: %w", err)
	}
	if !udp || len(out) <= q.limit {
		return out, nil
	}

	cut := dnsmessage.Message{Header: m.Header, Questions: m.Questions}
	cut.Header.Truncated = true
	for _, rr := range m.Additionals {
		if rr.Header.Type == dnsmessage.TypeOPT {
			cut.Additionals = append(cut.Additionals, rr)
		}
	}
	return cut.Pack()
}

// withoutPadding — та же запись OPT без опции добивки. Тело копируется:
// исходное может лежать в кэше или принадлежать запросу приложения.
func withoutPadding(rr dnsmessage.Resource) dnsmessage.Resource {
	body, ok := rr.Body.(*dnsmessage.OPTResource)
	if !ok {
		return rr
	}
	clean := &dnsmessage.OPTResource{}
	for _, o := range body.Options {
		if o.Code != optPadding {
			clean.Options = append(clean.Options, o)
		}
	}
	rr.Body = clean
	return rr
}
