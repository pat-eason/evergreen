// Package store implements SQLite persistence.
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
	"strings"
	"time"

	"github.com/patrickeason/financial-wellness/backend/internal/sim"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

// Store wraps the database connection.
type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  email TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS profiles (
  user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  data TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS members (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  first_name TEXT NOT NULL,
  last_name TEXT NOT NULL DEFAULT '',
  relationship TEXT NOT NULL DEFAULT 'other',
  birth_year INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_members_user ON members(user_id);
CREATE TABLE IF NOT EXISTS accounts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  category TEXT NOT NULL,
  config TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_accounts_user ON accounts(user_id);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  type TEXT NOT NULL,
  name TEXT NOT NULL,
  start_age INTEGER NOT NULL,
  years INTEGER NOT NULL DEFAULT 0,
  annual_amount REAL NOT NULL,
  inflation_adjusted INTEGER NOT NULL DEFAULT 1,
  taxable INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_events_user ON events(user_id);
CREATE TABLE IF NOT EXISTS budgets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  month TEXT NOT NULL,
  notes TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  UNIQUE(user_id, month)
);
CREATE TABLE IF NOT EXISTS budget_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  budget_id INTEGER NOT NULL REFERENCES budgets(id) ON DELETE CASCADE,
  section TEXT NOT NULL,
  name TEXT NOT NULL,
  expr TEXT NOT NULL DEFAULT '',
  amount REAL NOT NULL,
  growth_rate REAL NOT NULL DEFAULT 0,
  account_id INTEGER,
  position INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_budget_items_budget ON budget_items(budget_id);
CREATE TABLE IF NOT EXISTS calculations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  inputs TEXT NOT NULL,
  result TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_calcs_user ON calculations(user_id, kind);
`

// Open opens (creating if needed) a SQLite database at path, or an in-memory
// database when path is ":memory:".
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: one writer; keeps in-memory DB coherent
	// Older databases called this column "username".
	var legacy int
	_ = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name='username'`).Scan(&legacy)
	if legacy > 0 {
		if _, err := db.Exec(`ALTER TABLE users RENAME COLUMN username TO email`); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate users.email: %w", err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	// Columns added after the first release.
	for _, m := range []struct{ table, col, ddl string }{
		{"accounts", "owner", "TEXT NOT NULL DEFAULT 'self'"},
		{"events", "owner", "TEXT NOT NULL DEFAULT 'self'"},
	} {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('`+m.table+`') WHERE name=?`, m.col).Scan(&n)
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE ` + m.table + ` ADD COLUMN ` + m.col + ` ` + m.ddl); err != nil {
				db.Close()
				return nil, fmt.Errorf("migrate %s.%s: %w", m.table, m.col, err)
			}
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// ---------- users & sessions ----------

type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateUser(ctx context.Context, email, hash string) (User, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO users(email,password_hash,created_at) VALUES(?,?,?)`, email, hash, now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrConflict
		}
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Email: email, CreatedAt: time.Now().UTC()}, nil
}

// UserByUsername returns the user and their password hash.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, string, error) {
	var u User
	var hash, created string
	err := s.db.QueryRowContext(ctx, `SELECT id,email,password_hash,created_at FROM users WHERE email=?`, email).Scan(&u.ID, &u.Email, &hash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return u, hash, err
}

// UserHashByID returns a user's password hash.
func (s *Store) UserHashByID(ctx context.Context, id int64) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id=?`, id).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return h, err
}

func (s *Store) SetEmail(ctx context.Context, id int64, email string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET email=? WHERE id=?`, email, id)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
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
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, hash, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND token_hash<>?`, id, hashToken(keepToken)); err != nil {
		return err
	}
	return tx.Commit()
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES(?,?,?)`, hashToken(token), userID, exp.Format(time.RFC3339))
	return token, exp, err
}

func (s *Store) UserBySession(ctx context.Context, token string) (User, error) {
	var u User
	var created, exp string
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.email,u.created_at,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=?`, hashToken(token)).Scan(&u.ID, &u.Email, &created, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if t, _ := time.Parse(time.RFC3339, exp); time.Now().After(t) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, hashToken(token))
		return User{}, ErrNotFound
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return u, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, hashToken(token))
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now())
}

// ---------- profile ----------

func (s *Store) GetProfile(ctx context.Context, uid int64) (sim.Profile, bool, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT data FROM profiles WHERE user_id=?`, uid).Scan(&data)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO profiles(user_id,data,updated_at) VALUES(?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET data=excluded.data, updated_at=excluded.updated_at`, uid, string(b), now())
	return err
}

// ---------- accounts ----------

func (s *Store) ListAccounts(ctx context.Context, uid int64) ([]sim.Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,category,owner,config FROM accounts WHERE user_id=? ORDER BY id`, uid)
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
	err := s.db.QueryRowContext(ctx, `SELECT id,name,category,owner,config FROM accounts WHERE id=? AND user_id=?`, id, uid).Scan(&a.ID, &a.Name, &a.Category, &a.Owner, &cfg)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	_ = json.Unmarshal([]byte(cfg), &a.Config)
	return a, err
}

func (s *Store) CreateAccount(ctx context.Context, uid int64, a sim.Account) (sim.Account, error) {
	b, _ := json.Marshal(a.Config)
	res, err := s.db.ExecContext(ctx, `INSERT INTO accounts(user_id,name,category,owner,config,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, uid, a.Name, a.Category, a.Owner, string(b), now(), now())
	if err != nil {
		return a, err
	}
	a.ID, _ = res.LastInsertId()
	return a, nil
}

func (s *Store) UpdateAccount(ctx context.Context, uid int64, a sim.Account) error {
	b, _ := json.Marshal(a.Config)
	res, err := s.db.ExecContext(ctx, `UPDATE accounts SET name=?,category=?,owner=?,config=?,updated_at=? WHERE id=? AND user_id=?`, a.Name, a.Category, a.Owner, string(b), now(), a.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAccount(ctx context.Context, uid, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id=? AND user_id=?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// Unlink any budget items pointing at it.
	_, _ = s.db.ExecContext(ctx, `UPDATE budget_items SET account_id=NULL WHERE account_id=? AND budget_id IN (SELECT id FROM budgets WHERE user_id=?)`, id, uid)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id,first_name,last_name,relationship,birth_year FROM members WHERE user_id=? ORDER BY id`, uid)
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
	res, err := s.db.ExecContext(ctx, `INSERT INTO members(user_id,first_name,last_name,relationship,birth_year) VALUES(?,?,?,?,?)`, uid, m.FirstName, m.LastName, m.Relationship, m.BirthYear)
	if err != nil {
		return m, err
	}
	m.ID, _ = res.LastInsertId()
	return m, nil
}

func (s *Store) UpdateMember(ctx context.Context, uid int64, m Member) error {
	res, err := s.db.ExecContext(ctx, `UPDATE members SET first_name=?,last_name=?,relationship=?,birth_year=? WHERE id=? AND user_id=?`, m.FirstName, m.LastName, m.Relationship, m.BirthYear, m.ID, uid)
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
	res, err := tx.ExecContext(ctx, `DELETE FROM members WHERE id=? AND user_id=?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	key := strconv.FormatInt(id, 10)
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET owner='joint' WHERE user_id=? AND owner=?`, uid, key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE events SET owner='self' WHERE user_id=? AND owner=?`, uid, key); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------- events ----------

func (s *Store) ListEvents(ctx context.Context, uid int64) ([]sim.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,type,name,owner,start_age,years,annual_amount,inflation_adjusted,taxable FROM events WHERE user_id=? ORDER BY start_age,id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []sim.Event{}
	for rows.Next() {
		var e sim.Event
		var ia, tx int
		if err := rows.Scan(&e.ID, &e.Kind, &e.Type, &e.Name, &e.Owner, &e.StartAge, &e.Years, &e.AnnualAmount, &ia, &tx); err != nil {
			return nil, err
		}
		e.InflationAdjusted, e.Taxable = ia == 1, tx == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) CreateEvent(ctx context.Context, uid int64, e sim.Event) (sim.Event, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO events(user_id,kind,type,name,owner,start_age,years,annual_amount,inflation_adjusted,taxable) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		uid, e.Kind, e.Type, e.Name, e.Owner, e.StartAge, e.Years, e.AnnualAmount, b2i(e.InflationAdjusted), b2i(e.Taxable))
	if err != nil {
		return e, err
	}
	e.ID, _ = res.LastInsertId()
	return e, nil
}

func (s *Store) UpdateEvent(ctx context.Context, uid int64, e sim.Event) error {
	res, err := s.db.ExecContext(ctx, `UPDATE events SET kind=?,type=?,name=?,owner=?,start_age=?,years=?,annual_amount=?,inflation_adjusted=?,taxable=? WHERE id=? AND user_id=?`,
		e.Kind, e.Type, e.Name, e.Owner, e.StartAge, e.Years, e.AnnualAmount, b2i(e.InflationAdjusted), b2i(e.Taxable), e.ID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteEvent(ctx context.Context, uid, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE id=? AND user_id=?`, id, uid)
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
WHERE b.user_id=? GROUP BY b.id ORDER BY b.month DESC`, uid)
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
	err := s.db.QueryRowContext(ctx, `SELECT id,notes,updated_at FROM budgets WHERE user_id=? AND month=?`, uid, month).Scan(&id, &b.Notes, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,section,name,expr,amount,growth_rate,account_id FROM budget_items WHERE budget_id=? ORDER BY position,id`, id)
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
	err = tx.QueryRowContext(ctx, `SELECT id FROM budgets WHERE user_id=? AND month=?`, uid, b.Month).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := tx.ExecContext(ctx, `INSERT INTO budgets(user_id,month,notes,updated_at) VALUES(?,?,?,?)`, uid, b.Month, b.Notes, now())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
	} else if err != nil {
		return err
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE budgets SET notes=?,updated_at=? WHERE id=?`, b.Notes, now(), id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM budget_items WHERE budget_id=?`, id); err != nil {
			return err
		}
	}
	for pos, it := range b.Items {
		var acc any
		if it.AccountID != nil {
			// only link to accounts the user owns
			var n int
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE id=? AND user_id=?`, *it.AccountID, uid).Scan(&n)
			if n == 1 {
				acc = *it.AccountID
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_items(budget_id,section,name,expr,amount,growth_rate,account_id,position) VALUES(?,?,?,?,?,?,?,?)`,
			id, it.Section, it.Name, it.Expr, it.Amount, it.GrowthRate, acc, pos); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteBudget(ctx context.Context, uid int64, month string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM budgets WHERE user_id=? AND month=?`, uid, month)
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
	q := `SELECT id,kind,name,inputs,result,created_at FROM calculations WHERE user_id=?`
	args := []any{uid}
	if kind != "" {
		q += ` AND kind=?`
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
		if err := rows.Scan(&c.ID, &c.Kind, &c.Name, &in, &res, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Inputs, c.Result = json.RawMessage(in), json.RawMessage(res)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CreateCalculation(ctx context.Context, uid int64, c Calculation) (Calculation, error) {
	c.CreatedAt = now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO calculations(user_id,kind,name,inputs,result,created_at) VALUES(?,?,?,?,?,?)`,
		uid, c.Kind, c.Name, string(c.Inputs), string(c.Result), c.CreatedAt)
	if err != nil {
		return c, err
	}
	c.ID, _ = res.LastInsertId()
	return c, nil
}

func (s *Store) DeleteCalculation(ctx context.Context, uid, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM calculations WHERE id=? AND user_id=?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
