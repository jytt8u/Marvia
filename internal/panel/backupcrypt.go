package panel

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/scrypt"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Шифрование копий базы.
//
// Копия — это вся панель целиком: покупатели, их телеграм-идентификаторы,
// токены подписок, секреты vless и trojan. И она специально уезжает с
// сервера: держать копии только там, где стоит панель, значит потерять их
// вместе с ней. Дальше копия живёт в облаке, на ноутбуке, иногда в переписке.
//
// Отсюда требование: копия должна быть нечитаемой без ключа, которого на
// сервере нет.
//
// Поэтому панель знает только **публичный** ключ —
// им можно зашифровать и нельзя расшифровать. Приватный человек держит у себя:
// в менеджере паролей, на бумаге, где угодно вне сервера. Изъятая панель даёт
// изъявшему шифротекст и публичный ключ, то есть ничего.
//
// Парольный режим выводит приватный ключ через готовый scrypt только при
// настройке и восстановлении. На сервере остаются соль и публичный ключ:
// расписание переживает перезапуск, а пароль рядом с копией не появляется.
//
// Примитив взят готовый: crypto_box_seal из nacl/box — X25519, XSalsa20 и
// Poly1305. Отправитель анонимный: на каждую копию берётся одноразовая пара
// ключей, так что по шифротексту нельзя сказать даже того, что две копии сняты
// одной панелью. Своего в криптографии здесь нет ничего, и не будет.

// backupMagic — что это за файл. Первая строка нужна человеку, который нашёл
// файл через год и не помнит, чем его открывать.
var backupMagic = []byte("MARVIA-BACKUP-SEALED-1\n")

// maxSealedBackup — предел размера копии, которую шифруем.
//
// Шифруется файл целиком в памяти: потоковое шифрование потребовало бы
// собственной нарезки на куски, а нарезка — это уже конструкция, в которой
// ошибаются (обрезанный хвост, переставленные куски). Правило проекта про
// «никакой своей криптографии» относится и к ней.
//
// Полгигабайта панель не наберёт и за годы: это сотни тысяч покупателей.
// Упереться в предел — повод не выдумывать нарезку, а прийти и сделать её
// по-человечески.
const maxSealedBackup = 512 << 20

// SealBackup шифрует файл копии на публичный ключ и убирает открытый.
//
// Открытый файл существует на диске между VACUUM INTO и этим вызовом: SQLite
// умеет писать копию только в файл. Окно короткое, но оно есть, и делать вид,
// что его нет, нельзя — на общем сервере с чужими процессами это имеет
// значение.
func SealBackup(path string, recipient []byte) (string, error) {
	return sealBackup(path, recipient, backupMagic)
}

func sealBackup(path string, recipient, header []byte) (string, error) {
	if len(recipient) != 32 {
		return "", fmt.Errorf("ключ для копий: длина %d байт, ожидается 32", len(recipient))
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxSealedBackup {
		return "", fmt.Errorf("копия %d байт — больше предела для шифрования (%d)", info.Size(), maxSealedBackup)
	}

	plain, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var key [32]byte
	copy(key[:], recipient)

	sealed, err := box.SealAnonymous(nil, plain, &key, rand.Reader)
	if err != nil {
		return "", fmt.Errorf("шифрование копии: %w", err)
	}

	out := path + sealedSuffix
	body := append(append([]byte{}, header...), sealed...)
	if err := os.WriteFile(out, body, 0o600); err != nil {
		return "", fmt.Errorf("запись зашифрованной копии: %w", err)
	}

	// Открытый файл убираем только после того, как зашифрованный лёг на диск:
	// иначе сбой записи оставил бы человека вообще без копии.
	if err := os.Remove(path); err != nil {
		return out, fmt.Errorf("открытая копия осталась на диске: %w", err)
	}
	return out, nil
}

// OpenSealedBackup расшифровывает копию приватным ключом.
//
// Живёт здесь, а не только в отдельной утилите, чтобы у расшифровки был тест:
// копия, которую нечем открыть, — это не копия, и выяснять это в день, когда
// она понадобилась, поздно.
func OpenSealedBackup(sealed []byte, private []byte) ([]byte, error) {
	if len(private) != 32 {
		return nil, fmt.Errorf("приватный ключ: длина %d байт, ожидается 32", len(private))
	}
	if !bytes.HasPrefix(sealed, backupMagic) {
		return nil, errors.New("это не зашифрованная копия Marvia")
	}
	body := sealed[len(backupMagic):]

	// Публичный ключ выводим тем же способом, что и везде в проекте: второй
	// способ считать одно и то же — способ однажды получить два разных ответа.
	pair, err := vp1.KeyPairFromPrivate(private)
	if err != nil {
		return nil, err
	}

	var priv, pub [32]byte
	copy(priv[:], pair.Private)
	copy(pub[:], pair.Public)

	plain, ok := box.OpenAnonymous(nil, body, &pub, &priv)
	if !ok {
		return nil, errors.New("копия не расшифровалась: не тот ключ или файл повреждён")
	}
	return plain, nil
}

// sealedSuffix — чем оканчивается зашифрованная копия.
const sealedSuffix = ".sealed"

var passwordBackupMagic = []byte("MARVIA-BACKUP-PASSWORD-1\n")

const backupSaltSize = 32

// PasswordBackupRecipient оставляет панели только способность зашифровать.
// Соль случайна для каждой смены пароля; параметры scrypt закреплены версией
// формата, иначе копию через год было бы нечем открыть.
func PasswordBackupRecipient(password string) (public, salt []byte, err error) {
	if strings.TrimSpace(password) == "" {
		return nil, nil, errors.New("для копий нужен пароль")
	}
	salt = make([]byte, backupSaltSize)
	if _, err = rand.Read(salt); err != nil {
		return nil, nil, fmt.Errorf("соль для копий: %w", err)
	}
	private, err := backupPasswordKey(password, salt)
	if err != nil {
		return nil, nil, err
	}
	defer clear(private)
	pair, err := vp1.KeyPairFromPrivate(private)
	if err != nil {
		return nil, nil, err
	}
	defer clear(pair.Private)
	return pair.Public, salt, nil
}

func backupPasswordKey(password string, salt []byte) ([]byte, error) {
	if strings.TrimSpace(password) == "" || len(salt) != backupSaltSize {
		return nil, errors.New("для копии нужны пароль и соль правильной длины")
	}
	// 32 МиБ памяти замедляют перебор, но не мешают небольшой панели.
	return scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
}

func sealPasswordBackup(path string, recipient, salt []byte) (string, error) {
	if len(salt) != backupSaltSize {
		return "", errors.New("не задан пароль для копий")
	}
	header := append(append([]byte{}, passwordBackupMagic...), salt...)
	return sealBackup(path, recipient, header)
}

// OpenPasswordBackup использует ту же запечатанную коробку, что ключевой режим;
// своего шифра, нарезки или аутентификации здесь нет.
func OpenPasswordBackup(sealed []byte, password string) ([]byte, error) {
	if !bytes.HasPrefix(sealed, passwordBackupMagic) || len(sealed) < len(passwordBackupMagic)+backupSaltSize {
		return nil, errors.New("это не парольная копия Marvia")
	}
	body := sealed[len(passwordBackupMagic):]
	private, err := backupPasswordKey(password, body[:backupSaltSize])
	if err != nil {
		return nil, err
	}
	defer clear(private)
	plain, err := OpenSealedBackup(append(append([]byte{}, backupMagic...), body[backupSaltSize:]...), private)
	if err != nil {
		return nil, errors.New("копия не расшифровалась: не тот пароль или файл повреждён")
	}
	return plain, nil
}
