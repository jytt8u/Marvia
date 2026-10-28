package transport

import utls "github.com/refraction-networking/utls"

// fingerprints — имена отпечатков, как их пишет панель и как их понимает
// параметр fp в ссылках Xray, и приветствия uTLS за ними.
//
// Список панели (panel.Fingerprints) обязан целиком лежать здесь: иначе
// продавец выберет отпечаток, а клиент молча останется на Chrome. За этим
// следит тест.
var fingerprints = map[string]utls.ClientHelloID{
	"chrome":  utls.HelloChrome_Auto,
	"firefox": utls.HelloFirefox_Auto,
	"safari":  utls.HelloSafari_Auto,
	"ios":     utls.HelloIOS_Auto,
	"android": utls.HelloAndroid_11_OkHttp,
	"edge":    utls.HelloEdge_Auto,
	"qq":      utls.HelloQQ_Auto,
	"360":     utls.Hello360_Auto,
}

// Fingerprint находит приветствие по имени. Пустое или незнакомое имя —
// нулевое значение, то есть Chrome по умолчанию: старый клиент на новом
// имени должен подключиться, а не отказаться.
func Fingerprint(name string) (utls.ClientHelloID, bool) {
	id, ok := fingerprints[name]
	return id, ok
}
