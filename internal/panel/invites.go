package panel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Приглашение — одноразовый пропуск, по которому новая нода записывает себя
// в панель сама.
//
// Без него установщику пришлось бы носить на каждый сервер админский токен, а
// это значит, что взлом любой ноды отдаёт всё хозяйство разом: список
// покупателей, их ключи, право выпускать новые доступы. Ноды стоят на дешёвых
// машинах в чужих странах и живут под постоянным вниманием — считать их
// доверенными нельзя.
//
// Приглашение даёт ровно одно право: один раз назваться нодой. После этого
// оно сгорает, а нода получает свой токен, которым может делать только две
// вещи — забирать свой список пользователей и сдавать расход.

// ErrInviteSpent — приглашение уже использовано или просрочено.
var ErrInviteSpent = errors.New("приглашение уже использовано или просрочено")

// InviteLifetime — сколько живёт приглашение.
//
// Часа хватает на «скопировал команду, пошёл заводить сервер». Вечное
// приглашение было бы постоянной чёрной дверью: попав в историю команд, в
// переписку или в чужие руки, оно работало бы и через год.
const InviteLifetime = time.Hour

// Invite — выданное приглашение. Самого токена здесь нет: в базе лежит только
// его хеш, и показывается токен единственный раз, при выдаче.
type Invite struct {
	ID        int64      `json:"id"`
	Label     string     `json:"label"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	NodeID    *int64     `json:"node_id,omitempty"`

	// ExitID — у приглашения входа каскада: чей это вход. По такому
	// приглашению записывается не нода, а вход (cascade.go).
	ExitID    int64     `json:"exit_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateInvite выпускает приглашение и возвращает его токен — один раз.
// exitID — для входа каскада: чей вход; ноль — обычная нода.
func (s *Store) CreateInvite(ctx context.Context, label string, exitID int64) (Invite, string, error) {
	token, err := NewToken()
	if err != nil {
		return Invite{}, "", err
	}

	now := time.Now().UTC()
	expires := now.Add(InviteLifetime)

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO node_invites (token_hash, label, expires_at, created_at, exit_id) VALUES (?, ?, ?, ?, ?)`,
		HashToken(token), label, format(expires), format(now), nullID(exitID))
	if err != nil {
		return Invite{}, "", fmt.Errorf("выпуск приглашения: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Invite{}, "", err
	}

	return Invite{ID: id, Label: label, ExpiresAt: expires, CreatedAt: now, ExitID: exitID}, token, nil
}

// RedeemInvite обменивает приглашение на запись ноды и её токен.
//
// Всё делается одной транзакцией, и приглашение отмечается использованным до
// фиксации. Иначе две одновременные установки прошли бы по одному
// приглашению: обе увидели бы его свободным, обе завели бы ноду, и вторая
// оказалась бы в панели без ведома продавца.
func (s *Store) RedeemInvite(ctx context.Context, token string, p CreateNodeParams) (Node, string, error) {
	if p.PublicKey != "" {
		if _, err := vp1.DecodeKey(p.PublicKey); err != nil {
			return Node{}, "", fmt.Errorf("публичный ключ ноды: %w", err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Node{}, "", err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()

	var (
		inviteID int64
		expires  string
		exitID   int64
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, expires_at, COALESCE(exit_id, 0) FROM node_invites WHERE token_hash = ? AND used_at IS NULL`,
		HashToken(token)).Scan(&inviteID, &expires, &exitID)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, "", ErrInviteSpent
	}
	if err != nil {
		return Node{}, "", err
	}
	if parse(expires).Before(now) {
		return Node{}, "", ErrInviteSpent
	}

	nodeToken, err := NewToken()
	if err != nil {
		return Node{}, "", err
	}

	if exitID != 0 {
		// Входу ключи и маскировка ни к чему: протокол насквозь говорит
		// выход, и в подписку уходят его параметры. Что бы установщик ни
		// прислал, в запись входа это не попадает.
		p = CreateNodeParams{Name: p.Name, Country: p.Country, Address: p.Address}
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO nodes (name, country, address, sni, public_key, reality_public_key, reality_short_id, ws_path, token_hash, enabled, created_at, exit_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		p.Name, p.Country, p.Address, p.SNI, p.PublicKey, p.RealityPublicKey, p.RealityShortID, p.WSPath,
		HashToken(nodeToken), format(now), nullID(exitID))
	if err != nil {
		return Node{}, "", fmt.Errorf("создание ноды: %w", err)
	}
	nodeID, err := res.LastInsertId()
	if err != nil {
		return Node{}, "", err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE node_invites SET used_at = ?, node_id = ? WHERE id = ? AND used_at IS NULL`,
		format(now), nodeID, inviteID); err != nil {
		return Node{}, "", fmt.Errorf("гашение приглашения: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Node{}, "", err
	}

	return Node{
		ID: nodeID, Name: p.Name, Country: p.Country, Address: p.Address, SNI: p.SNI,
		PublicKey: p.PublicKey, RealityPublicKey: p.RealityPublicKey,
		RealityShortID: p.RealityShortID, WSPath: p.WSPath,
		Enabled: true, CreatedAt: now, ExitID: exitID,
	}, nodeToken, nil
}

// InviteAlive сообщает, можно ли ещё воспользоваться приглашением.
//
// Отдельно от RedeemInvite: раздать установщик надо, не гася приглашение, —
// гасится оно только когда нода действительно записалась.
func (s *Store) InviteAlive(ctx context.Context, token string) (bool, error) {
	var expires string
	err := s.db.QueryRowContext(ctx,
		`SELECT expires_at FROM node_invites WHERE token_hash = ? AND used_at IS NULL`,
		HashToken(token)).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return parse(expires).After(time.Now().UTC()), nil
}

// InviteExit — чей вход ставит приглашение; ноль — обычная нода. Установщик
// по нему решает, что ставить: ноду с ключами или перекладчик без них.
func (s *Store) InviteExit(ctx context.Context, token string) (int64, error) {
	var exitID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(exit_id, 0) FROM node_invites WHERE token_hash = ? AND used_at IS NULL`,
		HashToken(token)).Scan(&exitID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrInviteSpent
	}
	return exitID, err
}
