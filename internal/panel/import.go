package panel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/users"
)

// Переезд покупателей с чужой панели — Marzban или 3x-ui.
//
// Продавец держится за старую панель не потому, что она ему нравится, а
// потому, что у каждого покупателя в приложении лежит ссылка. Разослать сотне
// людей новую — неделя переписки и треть покупателей, которые так и не
// ответят. Поэтому переезжают не только срок и квота, но и то, чем человек
// подключается: UUID для VLESS, пароль для Trojan и адрес подписки.
//
// Читать чужие базы — дело internal/importer. Здесь только запись: панели
// незачем знать, как устроены таблицы Marzban.

// Виды прежних адресов подписки.
const (
	// aliasToken — адрес хранится как есть. Так устроен subId у 3x-ui:
	// случайная строка без подписи.
	aliasToken = "token"

	// aliasMarzban — Marzban токены не хранит, а подписывает: внутри имя
	// покупателя и время выдачи, подпись — ключом из его таблицы jwt.
	// Токенов у человека столько, сколько раз панель показала ему ссылку, и
	// перечислить их нельзя. Поэтому переносится право проверить подпись: имя,
	// ключ и момент, раньше которого выданные токены недействительны.
	//
	// Ключ лежит в базе открыто, и это не новая дыра: подделав токен, можно
	// получить подписку, то есть UUID покупателя, — а UUID в этой же базе и
	// так лежат открыто, см. AddCredential.
	aliasMarzban = "marzban"
)

// ErrNoAccess — у покупателя с прежней панели не осталось ни одного набора
// доступа, который понимает нода: все заняты другими или чужого вида.
var ErrNoAccess = errors.New("нечего переносить: ни одного набора доступа, который понимает нода")

// ImportedUser — покупатель с прежней панели в том виде, в каком его заводит
// ImportUser. Собирает его internal/importer.
type ImportedUser struct {
	// ExternalID — «marzban:имя» или «3x-ui:subId». По нему повторный импорт
	// узнаёт уже перенесённых и не заводит их второй раз: переезд можно
	// прервать и запустить снова.
	ExternalID string

	Label        string
	Enabled      bool
	ExpiresAt    *time.Time
	TrafficLimit int64

	// UsedBefore — сколько покупатель израсходовал до переезда. Входит в квоту
	// наравне с расходом на нодах Marvia; иначе переезд дарил бы каждому
	// полный тариф заново.
	UsedBefore int64
	MaxIPs     int

	// CreatedAt — когда покупатель появился на прежней панели. Нулевое время —
	// момент импорта.
	CreatedAt time.Time

	VLESS  []string
	Trojan []string

	// SubTokens — адреса подписки прежней панели, которые должны работать как
	// есть: subId у 3x-ui.
	SubTokens []string

	// MarzbanName, MarzbanSecret и NotBefore — чтобы проверять токены подписки
	// Marzban. NotBefore — позднейшее из «создан» и «подписку отозвали»: токен,
	// выданный раньше, Marzban тоже не принимал.
	MarzbanName   string
	MarzbanSecret string
	NotBefore     time.Time
}

// ImportResult — что стало с покупателем.
type ImportResult struct {
	UserID int64 `json:"user_id"`

	// Existed — покупатель уже был перенесён прошлым запуском; ничего не
	// менялось.
	Existed bool `json:"existed,omitempty"`

	// Skipped — что не перенеслось и почему: секрет уже занят другим
	// покупателем, адрес подписки уже чей-то.
	Skipped []string `json:"skipped,omitempty"`
}

// ImportUser заводит покупателя с прежней панели с его же ключами.
//
// Отличие от CreateUser в одном: секреты не выпускаются, а берутся как есть.
// Поэтому снаружи эта возможность закрыта — в API её нет. Покупатель,
// которому продавец через бота сам назначил UUID, — это покупатель, чей UUID
// знает кто-то ещё.
func (s *Store) ImportUser(ctx context.Context, u ImportedUser) (ImportResult, error) {
	if u.ExternalID == "" {
		return ImportResult{}, errors.New("у покупателя с прежней панели нет ключа")
	}
	existing, err := s.UserByExternalID(ctx, u.ExternalID)
	switch {
	case err == nil:
		return ImportResult{UserID: existing.ID, Existed: true}, nil
	case !errors.Is(err, ErrNotFound):
		return ImportResult{}, err
	}

	subToken, err := NewToken()
	if err != nil {
		return ImportResult{}, err
	}
	created := u.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	enabled := 0
	if u.Enabled {
		enabled = 1
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO users (label, enabled, expires_at, traffic_limit, max_ips, sub_token, created_at, external_id, used_before)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Label, enabled, nullTime(u.ExpiresAt), u.TrafficLimit, u.MaxIPs, subToken, format(created),
		u.ExternalID, u.UsedBefore)
	if err != nil {
		return ImportResult{}, fmt.Errorf("перенос %s: %w", u.ExternalID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{UserID: id}

	type secret struct{ kind, value, shown string }
	var secrets []secret
	for _, v := range u.VLESS {
		raw, err := users.ParseUUID(v)
		if err != nil {
			result.Skipped = append(result.Skipped, fmt.Sprintf("VLESS %s: %v", v, err))
			continue
		}
		secrets = append(secrets, secret{CredVLESS, users.FormatUUID(raw), "VLESS " + v})
	}
	for _, p := range u.Trojan {
		if p == "" {
			continue
		}
		// Пароль в отчёт не попадает: отчёт продавец пересылает, когда
		// просит помощи, а пароль Trojan — это готовый доступ.
		secrets = append(secrets, secret{CredTrojan, p, "пароль Trojan"})
	}

	added := 0
	for _, sec := range secrets {
		taken, err := credentialTaken(ctx, tx, sec.kind, sec.value)
		if err != nil {
			return ImportResult{}, err
		}
		if taken {
			result.Skipped = append(result.Skipped, sec.shown+": уже выдан другому покупателю")
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO credentials (user_id, kind, secret, label, created_at) VALUES (?, ?, ?, '', ?)`,
			id, sec.kind, sec.value, format(now)); err != nil {
			return ImportResult{}, fmt.Errorf("перенос ключа %s: %w", u.ExternalID, err)
		}
		added++
	}
	// Подписчик без единого набора — запись, которая только путает: в
	// списке он есть, подключиться ему нечем.
	if added == 0 {
		return ImportResult{Skipped: result.Skipped}, ErrNoAccess
	}

	for _, token := range u.SubTokens {
		ok, err := addAlias(ctx, tx, aliasToken, token, id, "", time.Time{})
		if err != nil {
			return ImportResult{}, err
		}
		if !ok {
			result.Skipped = append(result.Skipped, "адрес подписки "+token+": уже отдан другому покупателю")
		}
	}
	if u.MarzbanName != "" && u.MarzbanSecret != "" {
		// Marzban сравнивает имена без учёта регистра — и мы так же.
		name := strings.ToLower(u.MarzbanName)
		ok, err := addAlias(ctx, tx, aliasMarzban, name, id, u.MarzbanSecret, u.NotBefore)
		if err != nil {
			return ImportResult{}, err
		}
		if !ok {
			result.Skipped = append(result.Skipped, "подписка Marzban "+name+": такое имя уже перенесено с другой панели")
		}
	}

	if err := tx.Commit(); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func credentialTaken(ctx context.Context, tx *sql.Tx, kind, secret string) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM credentials WHERE kind = ? AND secret = ?`, kind, secret).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// addAlias записывает прежний адрес подписки. false — адрес уже чей-то.
func addAlias(ctx context.Context, tx *sql.Tx, kind, key string, userID int64, secret string, notBefore time.Time) (bool, error) {
	if key == "" {
		return true, nil
	}
	var nb any
	if !notBefore.IsZero() {
		nb = format(notBefore)
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO sub_aliases (kind, key, user_id, secret, not_before) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (kind, key) DO NOTHING`,
		kind, key, userID, secret, nb)
	if err != nil {
		return false, fmt.Errorf("перенос адреса подписки: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// AddLegacySubPath запоминает путь, по которому прежняя панель отдавала
// подписки, например «sub» или случайный путь 3x-ui.
//
// По умолчанию панель отвечает 404 на всё, кроме своих путей, — сканеру
// незачем знать, что здесь живёт. Путь прежней панели открывается только тот,
// что был у неё на самом деле, и только для прежних адресов подписки.
func (s *Store) AddLegacySubPath(ctx context.Context, path string) error {
	segment := strings.Trim(path, "/")
	if segment == "" || strings.Contains(segment, "/") {
		return fmt.Errorf("путь подписки %q: нужен один сегмент вида /sub/", path)
	}
	paths, err := s.legacySubPaths(ctx)
	if err != nil {
		return err
	}
	for _, p := range paths {
		if p == segment {
			return nil
		}
	}
	paths = append(paths, segment)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES ('legacy_sub_paths', ?)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		strings.Join(paths, ","))
	return err
}

// IsLegacySubPath — отдавала ли прежняя панель подписки по этому пути.
func (s *Store) IsLegacySubPath(ctx context.Context, segment string) bool {
	paths, err := s.legacySubPaths(ctx)
	if err != nil {
		return false
	}
	for _, p := range paths {
		if p == segment {
			return true
		}
	}
	return false
}

func (s *Store) legacySubPaths(ctx context.Context) ([]string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'legacy_sub_paths'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(raw, ","), nil
}

// UserByLegacySubToken находит покупателя по адресу подписки прежней панели.
func (s *Store) UserByLegacySubToken(ctx context.Context, token string) (User, error) {
	var userID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT user_id FROM sub_aliases WHERE kind = ? AND key = ?`, aliasToken, token).Scan(&userID)
	switch {
	case err == nil:
		return s.GetUser(ctx, userID)
	case !errors.Is(err, sql.ErrNoRows):
		return User{}, err
	}

	name, issued, signed, ok := parseMarzbanToken(token)
	if !ok {
		return User{}, ErrNotFound
	}
	var (
		secret    string
		notBefore sql.NullString
	)
	err = s.db.QueryRowContext(ctx,
		`SELECT user_id, secret, not_before FROM sub_aliases WHERE kind = ? AND key = ?`,
		aliasMarzban, strings.ToLower(name)).Scan(&userID, &secret, &notBefore)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if !signed(secret) {
		return User{}, ErrNotFound
	}
	// Ровно то же сравнение, что у Marzban: токен, выданный раньше создания
	// покупателя или раньше отзыва подписки, недействителен.
	if nb := parseNullTime(notBefore); nb != nil && nb.After(issued) {
		return User{}, ErrNotFound
	}
	return s.GetUser(ctx, userID)
}

// marzbanJWTHeader — заголовок токенов подписки старых версий Marzban:
// {"alg":"HS256","typ":"JWT"}. По нему Marzban отличает старый формат от
// нового, и мы так же.
const marzbanJWTHeader = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"

// parseMarzbanToken разбирает токен подписки Marzban, не проверяя подписи:
// чтобы проверить, нужен ключ, а какой — зависит от имени внутри.
//
// Повторяет app/utils/jwt.py Marzban 0.8: новый формат — base64url строки
// «имя,время» без «=», за ним десять символов base64url от
// SHA-256(первая часть + ключ). Старый — обычный JWT HS256 с access =
// "subscription". Любое расхождение в мелочах отключило бы подписки всем
// перенесённым покупателям разом, поэтому формат сверяется тестом с
// токенами, собранными по исходнику Marzban.
func parseMarzbanToken(token string) (name string, issued time.Time, signed func(secret string) bool, ok bool) {
	if len(token) < 15 {
		return "", time.Time{}, nil, false
	}

	if strings.HasPrefix(token, marzbanJWTHeader+".") {
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			return "", time.Time{}, nil, false
		}
		raw, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return "", time.Time{}, nil, false
		}
		var claims struct {
			Sub    string      `json:"sub"`
			Access string      `json:"access"`
			Iat    json.Number `json:"iat"`
		}
		if err := json.Unmarshal(raw, &claims); err != nil || claims.Access != "subscription" || claims.Sub == "" {
			return "", time.Time{}, nil, false
		}
		iat, err := claims.Iat.Int64()
		if err != nil {
			return "", time.Time{}, nil, false
		}
		sig, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil {
			return "", time.Time{}, nil, false
		}
		signed = func(secret string) bool {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(parts[0] + "." + parts[1]))
			return hmac.Equal(sig, mac.Sum(nil))
		}
		return claims.Sub, time.Unix(iat, 0), signed, true
	}

	body, sig := token[:len(token)-10], token[len(token)-10:]
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", time.Time{}, nil, false
	}
	fields := strings.Split(string(raw), ",")
	if len(fields) < 2 || fields[0] == "" {
		return "", time.Time{}, nil, false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
	if err != nil {
		return "", time.Time{}, nil, false
	}
	signed = func(secret string) bool {
		sum := sha256.Sum256([]byte(body + secret))
		want := base64.URLEncoding.EncodeToString(sum[:])[:10]
		return subtle.ConstantTimeCompare([]byte(want), []byte(sig)) == 1
	}
	return fields[0], time.Unix(ts, 0), signed, true
}
