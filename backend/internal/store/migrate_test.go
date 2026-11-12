package store

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

// TestMigrateToUUIDs builds the v1 (BIGINT identity) schema, fills it with
// representative data, then applies the remaining migrations and checks that
// every row, relationship and owner reference survived.
func TestMigrateToUUIDs(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "t_mig_uuid"
	admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // one connection so search_path sticks
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	must("SET search_path TO " + schema)

	// v1 schema + schema_migrations bookkeeping, as a production DB has it.
	must(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`)
	must(migrations[0])
	must(`INSERT INTO schema_migrations(version) VALUES (1)`)

	must(`INSERT INTO users(email,password_hash) VALUES ('a@x.com','h'),('b@x.com','h')`)
	must(`INSERT INTO sessions VALUES ('tok', 1, now() + interval '1 day')`)
	must(`INSERT INTO profiles(user_id,data) VALUES (1,'{"first_name":"A"}')`)
	must(`INSERT INTO members(user_id,first_name,birth_year) VALUES (1,'Rachel',1990),(1,'Sam',2015),(2,'Other',1980)`)
	// accounts 1..3: 1 = self, 2 = owned by member 1, 3 = user b's. account 9 is deleted (ledger only).
	must(`INSERT INTO accounts(user_id,name,category,owner,config) VALUES
		(1,'401k','investment','self','{}'),
		(1,'IRA','investment','1','{}'),
		(2,'B acct','cash','3','{}')`)
	must(`INSERT INTO events(user_id,kind,type,name,owner,start_age,annual_amount) VALUES
		(1,'income','pension','P','2',65,1000),(1,'income','other','Q','self',60,5)`)
	must(`INSERT INTO ledger(user_id,account_id,account_name,category,owner,kind,balance,occurred_at) VALUES
		(1,1,'401k','investment','self','created',10,'2026-01-01'),
		(1,2,'IRA','investment','1','created',20,'2026-01-01'),
		(1,2,'IRA','investment','1','updated',25,'2026-02-01'),
		(1,9,'Old','cash','self','created',5,'2026-01-01'),
		(1,9,'Old','cash','self','deleted',0,'2026-02-01')`)
	must(`INSERT INTO budgets(user_id,month) VALUES (1,'2026-03'),(2,'2026-03')`)
	must(`INSERT INTO budget_items(budget_id,section,name,amount,account_id,position) VALUES
		(1,'savings','401k',100,1,0),(1,'expense','Rent',900,NULL,1),(1,'savings','gone',5,9,2),(2,'income','Pay',1,NULL,0)`)
	must(`INSERT INTO calculations(user_id,kind,name,inputs,result) VALUES (1,'k','n','{}','{}')`)

	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	count := func(q string) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
		return n
	}
	want := func(q string, n int) {
		t.Helper()
		if got := count(q); got != n {
			t.Errorf("got %d, want %d: %s", got, n, q)
		}
	}
	// ids are uuids now
	for _, tb := range []string{"users", "members", "accounts", "events", "budgets", "budget_items", "calculations", "ledger"} {
		var typ string
		db.QueryRowContext(ctx, `SELECT data_type FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2 AND column_name='id'`, schema, tb).Scan(&typ)
		if typ != "uuid" {
			t.Errorf("%s.id is %q, want uuid", tb, typ)
		}
	}
	// row counts unchanged
	want(`SELECT COUNT(*) FROM users`, 2)
	want(`SELECT COUNT(*) FROM sessions`, 1)
	want(`SELECT COUNT(*) FROM profiles`, 1)
	want(`SELECT COUNT(*) FROM members`, 3)
	want(`SELECT COUNT(*) FROM accounts`, 3)
	want(`SELECT COUNT(*) FROM events`, 2)
	want(`SELECT COUNT(*) FROM ledger`, 5)
	want(`SELECT COUNT(*) FROM budget_items`, 4)
	want(`SELECT COUNT(*) FROM calculations`, 1)
	// user ownership preserved
	want(`SELECT COUNT(*) FROM accounts a JOIN users u ON u.id=a.user_id WHERE u.email='a@x.com'`, 2)
	want(`SELECT COUNT(*) FROM sessions s JOIN users u ON u.id=s.user_id WHERE u.email='a@x.com'`, 1)
	want(`SELECT COUNT(*) FROM profiles p JOIN users u ON u.id=p.user_id WHERE u.email='a@x.com'`, 1)
	want(`SELECT COUNT(*) FROM members m JOIN users u ON u.id=m.user_id WHERE u.email='a@x.com'`, 2)
	// text owner references now hold the member uuid
	want(`SELECT COUNT(*) FROM accounts a JOIN members m ON a.owner=m.id::text WHERE a.name='IRA' AND m.first_name='Rachel'`, 1)
	want(`SELECT COUNT(*) FROM accounts a JOIN members m ON a.owner=m.id::text WHERE a.name='B acct' AND m.first_name='Other' AND m.user_id=a.user_id`, 1)
	want(`SELECT COUNT(*) FROM events e JOIN members m ON e.owner=m.id::text WHERE e.name='P' AND m.first_name='Sam'`, 1)
	want(`SELECT COUNT(*) FROM events WHERE name='Q' AND owner='self'`, 1)
	want(`SELECT COUNT(*) FROM ledger l JOIN members m ON l.owner=m.id::text WHERE m.first_name='Rachel'`, 2)
	// ledger groups by account, matches live accounts, and keeps deleted history together
	want(`SELECT COUNT(*) FROM ledger l JOIN accounts a ON a.id=l.account_id WHERE a.name='IRA' AND l.account_name='IRA'`, 2)
	want(`SELECT COUNT(DISTINCT account_id) FROM ledger WHERE account_name='Old'`, 1)
	want(`SELECT COUNT(*) FROM ledger l WHERE account_name='Old' AND NOT EXISTS (SELECT 1 FROM accounts a WHERE a.id=l.account_id)`, 2)
	// budget items stay attached to their budget and account
	want(`SELECT COUNT(*) FROM budget_items i JOIN budgets b ON b.id=i.budget_id JOIN users u ON u.id=b.user_id WHERE u.email='a@x.com'`, 3)
	want(`SELECT COUNT(*) FROM budget_items i JOIN accounts a ON a.id=i.account_id WHERE i.name='401k' AND a.name='401k'`, 1)
	want(`SELECT COUNT(*) FROM budget_items WHERE account_id IS NULL AND name='Rent'`, 1)
	// order preserved
	var first string
	db.QueryRowContext(ctx, `SELECT first_name FROM members WHERE user_id=(SELECT id FROM users WHERE email='a@x.com') ORDER BY created_at,id LIMIT 1`).Scan(&first)
	if first != "Rachel" {
		t.Errorf("member order lost: first is %q", first)
	}
	// constraints are back: cascade delete, unique month, default ids
	must(`INSERT INTO members(user_id,first_name,birth_year) SELECT id,'New',2000 FROM users WHERE email='a@x.com'`)
	if _, err := db.ExecContext(ctx, `INSERT INTO budgets(user_id,month) SELECT id,'2026-03' FROM users WHERE email='a@x.com'`); err == nil {
		t.Error("budgets (user_id, month) uniqueness was lost")
	}
	must(`DELETE FROM users WHERE email='a@x.com'`)
	want(`SELECT COUNT(*) FROM accounts WHERE name IN ('401k','IRA')`, 0)
	want(`SELECT COUNT(*) FROM budget_items`, 1)
	want(`SELECT COUNT(*) FROM schema_migrations`, 2)
}
