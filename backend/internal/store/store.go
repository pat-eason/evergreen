// Package store implements PostgreSQL persistence.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/patrickeason/financial-wellness/backend/internal/ledger"
	"github.com/patrickeason/financial-wellness/backend/internal/sim"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

// Store wraps the database connection pool.
type Store struct{ db *sql.DB }

// migrations are applied in order, once each, and recorded in schema_migrations.
// Append new entries; never edit one that has shipped.
var migrations = []string{
	// 1: initial schema
	`
CREATE TABLE users (
  id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  email         TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- emails are unique regardless of case
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE profiles (
  user_id    BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  data       JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE members (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  first_name   TEXT NOT NULL,
  last_name    TEXT NOT NULL DEFAULT '',
  relationship TEXT NOT NULL DEFAULT 'other',
  birth_year   INTEGER NOT NULL
);
CREATE INDEX idx_members_user ON members(user_id);

CREATE TABLE accounts (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  category   TEXT NOT NULL,
  owner      TEXT NOT NULL DEFAULT 'self',
  config     JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_accounts_user ON accounts(user_id);

-- Append-only log of account changes. account_id is deliberately not a foreign
-- key: history must outlive the account.
CREATE TABLE ledger (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id   BIGINT NOT NULL,
  account_name TEXT NOT NULL,
  category     TEXT NOT NULL,
  owner        TEXT NOT NULL DEFAULT 'self',
  kind         TEXT NOT NULL,
  reason       TEXT NOT NULL DEFAULT '',
  balance      DOUBLE PRECISION NOT NULL,
  asset_value  DOUBLE PRECISION NOT NULL DEFAULT 0,
  note         TEXT NOT NULL DEFAULT '',
  occurred_at  TIMESTAMPTZ NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ledger_user_time ON ledger(user_id, occurred_at);
CREATE INDEX idx_ledger_account ON ledger(user_id, account_id);

CREATE TABLE events (
  id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id            BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind               TEXT NOT NULL,
  type               TEXT NOT NULL,
  name               TEXT NOT NULL,
  owner              TEXT NOT NULL DEFAULT 'self',
  start_age          INTEGER NOT NULL,
  years              INTEGER NOT NULL DEFAULT 0,
  annual_amount      DOUBLE PRECISION NOT NULL,
  inflation_adjusted BOOLEAN NOT NULL DEFAULT TRUE,
  taxable            BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_events_user ON events(user_id);

CREATE TABLE budgets (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  month      TEXT NOT NULL,
  notes      TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, month)
);

CREATE TABLE budget_items (
  id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  budget_id   BIGINT NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
  section     TEXT NOT NULL,
  name        TEXT NOT NULL,
  expr        TEXT NOT NULL DEFAULT '',
  amount      DOUBLE PRECISION NOT NULL,
  growth_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
  account_id  BIGINT,
  position    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_budget_items_budget ON budget_items(budget_id);

CREATE TABLE calculations (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  name       TEXT NOT NULL,
  inputs     JSONB NOT NULL,
  result     JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_calcs_user ON calculations(user_id, kind);
`,
}

// migrationLock serialises migrators (several app instances, or tests) via a
// Postgres advisory lock.
const migrationLock = 7272741

func migrate(ctx context.Context, db *sql.DB) error {
	// Advisory locks belong to a session, so hold one dedicated connection.
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLock); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLock) //nolint:errcheck

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	var current int
	if err := conn.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	if current > len(migrations) {
		return fmt.Errorf("database schema is version %d but this build only knows %d: refusing to run an older release against a newer database", current, len(migrations))
	}
	for i := current; i < len(migrations); i++ {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Open connects to Postgres at url (a libpq-style URL, e.g.
// postgres://user:pass@host:5432/db?sslmode=disable), waits for it to accept
// connections, and applies any pending migrations.
func Open(ctx context.Context, url string) (*Store, error) { return open(ctx, url, "") }

// OpenSchema is Open with every connection confined to one schema. Tests use it
// to get an isolated database per test inside a shared Postgres.
func OpenSchema(ctx context.Context, url, schema string) (*Store, error) {
	return open(ctx, url, schema)
}

func open(ctx context.Context, url, schema string) (*Store, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid database URL: %w", err)
	}
	if schema != "" {
		if cfg.RuntimeParams == nil {
			cfg.RuntimeParams = map[string]string{}
		}
		cfg.RuntimeParams["search_path"] = schema
	}
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	// The database container may still be starting when we are.
	var pingErr error
	for attempt := 0; attempt < 30; attempt++ {
		if pingErr = db.PingContext(ctx); pingErr == nil {
			break
		}
		select {
		case <-ctx.Done():
			db.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if pingErr != nil {
		db.Close()
		return nil, fmt.Errorf("connect to database: %w", pingErr)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// isUnique reports whether err is a Postgres unique-constraint violation.
func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// ---------- users & sessions ----------

type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateUser(ctx context.Context, email, hash string) (User, error) {
	u := User{Email: email}
	err := s.db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES($1,$2) RETURNING id, created_at`, email, hash).Scan(&u.ID, &u.CreatedAt)
	if isUnique(err) {
		return User{}, ErrConflict
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return u, err
}

// UserByEmail returns the user and their password hash (case-insensitive match).
func (s *Store) UserByEmail(ctx context.Context, email string) (User, string, error) {
	var u User
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT id,email,password_hash,created_at FROM users WHERE lower(email)=lower($1)`, email).Scan(&u.ID, &u.Email, &hash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return u, hash, err
}

// UserHashByID returns a user's password hash.
func (s *Store) UserHashByID(ctx context.Context, id int64) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id=$1`, id).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return h, err
}

func (s *Store) SetEmail(ctx context.Context, id int64, email string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET email=$1 WHERE id=$2`, email, id)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

// SetPassword stores a new hash and signs out every session except keepToken.
func (s *Store) SetPassword(ctx context.Context, id int64, hash, keepToken string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=$1 WHERE id=$2`, hash, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1 AND token_hash<>$2`, id, hashToken(keepToken)); err != nil {
		return err
	}
	return tx.Commit()
}

// CountUsers returns how many accounts exist.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// CreateSession issues a new opaque session token (only its hash is stored).
func (s *Store) CreateSession(ctx context.Context, userID int64, ttl time.Duration) (string, time.Time, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(b)
	exp := time.Now().UTC().Add(ttl)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)`, hashToken(token), userID, exp)
	return token, exp, err
}

func (s *Store) UserBySession(ctx context.Context, token string) (User, error) {
	var u User
	var exp time.Time
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.email,u.created_at,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1`, hashToken(token)).Scan(&u.ID, &u.Email, &u.CreatedAt, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if time.Now().After(exp) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1`, hashToken(token))
		return User{}, ErrNotFound
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return u, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1`, hashToken(token))
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
}

// ---------- profile ----------

func (s *Store) GetProfile(ctx context.Context, uid int64) (sim.Profile, bool, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT data::text FROM profiles WHERE user_id=$1`, uid).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return sim.DefaultProfile(), false, nil
	}
	if err != nil {
		return sim.Profile{}, false, err
	}
	p := sim.DefaultProfile()
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return sim.Profile{}, false, err
	}
	p.Normalize()
	return p, true, nil
}

func (s *Store) SaveProfile(ctx context.Context, uid int64, p sim.Profile) error {
	b, _ := json.Marshal(p)
	_, err := s.db.ExecContext(ctx, `INSERT INTO profiles(user_id,data,updated_at) VALUES($1,$2::jsonb,now())
		ON CONFLICT (user_id) DO UPDATE SET data=EXCLUDED.data, updated_at=EXCLUDED.updated_at`, uid, string(b))
	return err
}

// ---------- accounts ----------

func (s *Store) ListAccounts(ctx context.Context, uid int64) ([]sim.Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,category,owner,config::text FROM accounts WHERE user_id=$1 ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sim.Account{}
	for rows.Next() {
		var a sim.Account
		var cfg string
		if err := rows.Scan(&a.ID, &a.Name, &a.Category, &a.Owner, &cfg); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(cfg), &a.Config)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetAccount(ctx context.Context, uid, id int64) (sim.Account, error) {
	var a sim.Account
	var cfg string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,category,owner,config::text FROM accounts WHERE id=$1 AND user_id=$2`, id, uid).Scan(&a.ID, &a.Name, &a.Category, &a.Owner, &cfg)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	_ = json.Unmarshal([]byte(cfg), &a.Config)
	return a, err
}

func (s *Store) CreateAccount(ctx context.Context, uid int64, a sim.Account) (sim.Account, error) {
	b, _ := json.Marshal(a.Config)
	err := s.db.QueryRowContext(ctx, `INSERT INTO accounts(user_id,name,category,owner,config) VALUES($1,$2,$3,$4,$5::jsonb) RETURNING id`,
		uid, a.Name, a.Category, a.Owner, string(b)).Scan(&a.ID)
	return a, err
}

func (s *Store) UpdateAccount(ctx context.Context, uid int64, a sim.Account) error {
	b, _ := json.Marshal(a.Config)
	res, err := s.db.ExecContext(ctx, `UPDATE accounts SET name=$1,category=$2,owner=$3,config=$4::jsonb,updated_at=now() WHERE id=$5 AND user_id=$6`,
		a.Name, a.Category, a.Owner, string(b), a.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAccount(ctx context.Context, uid, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1 AND user_id=$2`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// Unlink any budget items pointing at it.
	_, _ = s.db.ExecContext(ctx, `UPDATE budget_items SET account_id=NULL WHERE account_id=$1 AND budget_id IN (SELECT id FROM budgets WHERE user_id=$2)`, id, uid)
	return nil
}

// ---------- household members ----------

type Member struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Relationship string `json:"relationship"`
	BirthYear    int    `json:"birth_year"`
}

func (s *Store) ListMembers(ctx context.Context, uid int64) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,first_name,last_name,relationship,birth_year FROM members WHERE user_id=$1 ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.FirstName, &m.LastName, &m.Relationship, &m.BirthYear); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CreateMember(ctx context.Context, uid int64, m Member) (Member, error) {
	err := s.db.QueryRowContext(ctx, `INSERT INTO members(user_id,first_name,last_name,relationship,birth_year) VALUES($1,$2,$3,$4,$5) RETURNING id`,
		uid, m.FirstName, m.LastName, m.Relationship, m.BirthYear).Scan(&m.ID)
	return m, err
}

func (s *Store) UpdateMember(ctx context.Context, uid int64, m Member) error {
	res, err := s.db.ExecContext(ctx, `UPDATE members SET first_name=$1,last_name=$2,relationship=$3,birth_year=$4 WHERE id=$5 AND user_id=$6`,
		m.FirstName, m.LastName, m.Relationship, m.BirthYear, m.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteMember removes a member; their accounts become joint and their events
// fall back to the primary person's timeline.
func (s *Store) DeleteMember(ctx context.Context, uid, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM members WHERE id=$1 AND user_id=$2`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	key := strconv.FormatInt(id, 10)
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET owner='joint' WHERE user_id=$1 AND owner=$2`, uid, key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE events SET owner='self' WHERE user_id=$1 AND owner=$2`, uid, key); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------- events ----------

func (s *Store) ListEvents(ctx context.Context, uid int64) ([]sim.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,type,name,owner,start_age,years,annual_amount,inflation_adjusted,taxable FROM events WHERE user_id=$1 ORDER BY start_age,id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sim.Event{}
	for rows.Next() {
		var e sim.Event
		if err := rows.Scan(&e.ID, &e.Kind, &e.Type, &e.Name, &e.Owner, &e.StartAge, &e.Years, &e.AnnualAmount, &e.InflationAdjusted, &e.Taxable); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) CreateEvent(ctx context.Context, uid int64, e sim.Event) (sim.Event, error) {
	err := s.db.QueryRowContext(ctx, `INSERT INTO events(user_id,kind,type,name,owner,start_age,years,annual_amount,inflation_adjusted,taxable) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		uid, e.Kind, e.Type, e.Name, e.Owner, e.StartAge, e.Years, e.AnnualAmount, e.InflationAdjusted, e.Taxable).Scan(&e.ID)
	return e, err
}

func (s *Store) UpdateEvent(ctx context.Context, uid int64, e sim.Event) error {
	res, err := s.db.ExecContext(ctx, `UPDATE events SET kind=$1,type=$2,name=$3,owner=$4,start_age=$5,years=$6,annual_amount=$7,inflation_adjusted=$8,taxable=$9 WHERE id=$10 AND user_id=$11`,
		e.Kind, e.Type, e.Name, e.Owner, e.StartAge, e.Years, e.AnnualAmount, e.InflationAdjusted, e.Taxable, e.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteEvent(ctx context.Context, uid, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE id=$1 AND user_id=$2`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- budgets ----------

type BudgetItem struct {
	ID         int64   `json:"id"`
	Section    string  `json:"section"` // income | savings | expense
	Name       string  `json:"name"`
	Expr       string  `json:"expr"`
	Amount     float64 `json:"amount"`
	GrowthRate float64 `json:"growth_rate"`
	AccountID  *int64  `json:"account_id"`
}

type Budget struct {
	Month     string       `json:"month"`
	Notes     string       `json:"notes"`
	UpdatedAt string       `json:"updated_at"`
	Items     []BudgetItem `json:"items"`
}

type BudgetMonth struct {
	Month    string  `json:"month"`
	Income   float64 `json:"income"`
	Savings  float64 `json:"savings"`
	Expenses float64 `json:"expenses"`
}

func (s *Store) ListBudgetMonths(ctx context.Context, uid int64) ([]BudgetMonth, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT b.month,
  COALESCE(SUM(CASE WHEN i.section='income' THEN i.amount END),0),
  COALESCE(SUM(CASE WHEN i.section='savings' THEN i.amount END),0),
  COALESCE(SUM(CASE WHEN i.section='expense' THEN i.amount END),0)
FROM budgets b LEFT JOIN budget_items i ON i.budget_id=b.id
WHERE b.user_id=$1 GROUP BY b.id, b.month ORDER BY b.month DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BudgetMonth{}
	for rows.Next() {
		var m BudgetMonth
		if err := rows.Scan(&m.Month, &m.Income, &m.Savings, &m.Expenses); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetBudget(ctx context.Context, uid int64, month string) (Budget, error) {
	b := Budget{Month: month, Items: []BudgetItem{}}
	var id int64
	var updated time.Time
	err := s.db.QueryRowContext(ctx, `SELECT id,notes,updated_at FROM budgets WHERE user_id=$1 AND month=$2`, uid, month).Scan(&id, &b.Notes, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	b.UpdatedAt = updated.UTC().Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `SELECT id,section,name,expr,amount,growth_rate,account_id FROM budget_items WHERE budget_id=$1 ORDER BY position,id`, id)
	if err != nil {
		return b, err
	}
	defer rows.Close()
	for rows.Next() {
		var it BudgetItem
		var acc sql.NullInt64
		if err := rows.Scan(&it.ID, &it.Section, &it.Name, &it.Expr, &it.Amount, &it.GrowthRate, &acc); err != nil {
			return b, err
		}
		if acc.Valid {
			v := acc.Int64
			it.AccountID = &v
		}
		b.Items = append(b.Items, it)
	}
	return b, rows.Err()
}

// SaveBudget replaces a month's budget atomically.
func (s *Store) SaveBudget(ctx context.Context, uid int64, b Budget) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM budgets WHERE user_id=$1 AND month=$2 FOR UPDATE`, uid, b.Month).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := tx.QueryRowContext(ctx, `INSERT INTO budgets(user_id,month,notes) VALUES($1,$2,$3) RETURNING id`, uid, b.Month, b.Notes).Scan(&id); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if _, err := tx.ExecContext(ctx, `UPDATE budgets SET notes=$1,updated_at=now() WHERE id=$2`, b.Notes, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM budget_items WHERE budget_id=$1`, id); err != nil {
			return err
		}
	}
	for pos, it := range b.Items {
		var acc any
		if it.AccountID != nil {
			// only link to accounts the user owns
			var n int
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE id=$1 AND user_id=$2`, *it.AccountID, uid).Scan(&n)
			if n == 1 {
				acc = *it.AccountID
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_items(budget_id,section,name,expr,amount,growth_rate,account_id,position) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
			id, it.Section, it.Name, it.Expr, it.Amount, it.GrowthRate, acc, pos); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteBudget(ctx context.Context, uid int64, month string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM budgets WHERE user_id=$1 AND month=$2`, uid, month)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- calculations (saved snapshots) ----------

type Calculation struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	Name      string          `json:"name"`
	Inputs    json.RawMessage `json:"inputs"`
	Result    json.RawMessage `json:"result"`
	CreatedAt string          `json:"created_at"`
}

func (s *Store) ListCalculations(ctx context.Context, uid int64, kind string) ([]Calculation, error) {
	q := `SELECT id,kind,name,inputs::text,result::text,created_at FROM calculations WHERE user_id=$1`
	args := []any{uid}
	if kind != "" {
		q += ` AND kind=$2`
		args = append(args, kind)
	}
	q += ` ORDER BY id DESC LIMIT 100`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Calculation{}
	for rows.Next() {
		var c Calculation
		var in, res string
		var at time.Time
		if err := rows.Scan(&c.ID, &c.Kind, &c.Name, &in, &res, &at); err != nil {
			return nil, err
		}
		c.Inputs, c.Result = json.RawMessage(in), json.RawMessage(res)
		c.CreatedAt = at.UTC().Format(time.RFC3339)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CreateCalculation(ctx context.Context, uid int64, c Calculation) (Calculation, error) {
	var at time.Time
	err := s.db.QueryRowContext(ctx, `INSERT INTO calculations(user_id,kind,name,inputs,result) VALUES($1,$2,$3,$4::jsonb,$5::jsonb) RETURNING id, created_at`,
		uid, c.Kind, c.Name, string(c.Inputs), string(c.Result)).Scan(&c.ID, &at)
	c.CreatedAt = at.UTC().Format(time.RFC3339)
	return c, err
}

func (s *Store) DeleteCalculation(ctx context.Context, uid, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM calculations WHERE id=$1 AND user_id=$2`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- ledger ----------

// AddLedger appends an entry.
func (s *Store) AddLedger(ctx context.Context, uid int64, e ledger.Entry) (ledger.Entry, error) {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	e.OccurredAt = e.OccurredAt.UTC()
	err := s.db.QueryRowContext(ctx, `INSERT INTO ledger(user_id,account_id,account_name,category,owner,kind,reason,balance,asset_value,note,occurred_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		uid, e.AccountID, e.AccountName, e.Category, e.Owner, e.Kind, e.Reason, e.Balance, e.AssetValue, e.Note, e.OccurredAt).Scan(&e.ID)
	return e, err
}

// ListLedger returns the user's entries in chronological order (not yet
// annotated), optionally restricted to one account.
func (s *Store) ListLedger(ctx context.Context, uid, accountID int64) ([]ledger.Entry, error) {
	q := `SELECT id,account_id,account_name,category,owner,kind,reason,balance,asset_value,note,occurred_at FROM ledger WHERE user_id=$1`
	args := []any{uid}
	if accountID != 0 {
		q += ` AND account_id=$2`
		args = append(args, accountID)
	}
	q += ` ORDER BY occurred_at, id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ledger.Entry{}
	for rows.Next() {
		var e ledger.Entry
		if err := rows.Scan(&e.ID, &e.AccountID, &e.AccountName, &e.Category, &e.Owner, &e.Kind, &e.Reason, &e.Balance, &e.AssetValue, &e.Note, &e.OccurredAt); err != nil {
			return nil, err
		}
		e.OccurredAt = e.OccurredAt.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// LatestLedgerTime returns when an account's most recent entry occurred.
func (s *Store) LatestLedgerTime(ctx context.Context, uid, accountID int64) (time.Time, bool, error) {
	var at sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT MAX(occurred_at) FROM ledger WHERE user_id=$1 AND account_id=$2`, uid, accountID).Scan(&at)
	if err != nil || !at.Valid {
		return time.Time{}, false, err
	}
	return at.Time.UTC(), true, nil
}
