package store_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/patrickeason/financial-wellness/backend/internal/ledger"
	"github.com/patrickeason/financial-wellness/backend/internal/sim"
	"github.com/patrickeason/financial-wellness/backend/internal/store"
	"github.com/patrickeason/financial-wellness/backend/internal/store/storetest"
)

var ctx = context.Background()

func newUser(t *testing.T, st *store.Store, email string) store.User {
	t.Helper()
	u, err := st.CreateUser(ctx, email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestMigrationsAreIdempotentAndRefuseDowngrades(t *testing.T) {
	url, schema := storetest.Schema(t)
	st, err := store.OpenSchema(ctx, url, schema)
	if err != nil {
		t.Fatal(err)
	}
	u := newUser(t, st, "a@example.com")
	st.Close()

	// reopening the same database is a no-op and keeps the data
	st, err = store.OpenSchema(ctx, url, schema)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	if n, _ := st.CountUsers(ctx); n != 1 {
		t.Fatalf("data should survive a reopen, users=%d", n)
	}
	_ = u
	st.Close()

	// a database migrated by a newer release must not be opened by an older one
	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, "INSERT INTO "+schema+".schema_migrations(version) VALUES (999)"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenSchema(ctx, url, schema); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected a refusal to run against a newer schema, got %v", err)
	}
}

func TestEmailUniquenessIsCaseInsensitive(t *testing.T) {
	st := storetest.New(t)
	newUser(t, st, "Pat@Example.com")
	if _, err := st.CreateUser(ctx, "pat@example.com", "h"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate with different case should conflict, got %v", err)
	}
	u, _, err := st.UserByEmail(ctx, "PAT@EXAMPLE.COM")
	if err != nil || u.Email != "Pat@Example.com" {
		t.Fatalf("lookup is case-insensitive: %v %+v", err, u)
	}
	// changing to someone else's address (any case) conflicts too
	other := newUser(t, st, "other@example.com")
	if err := st.SetEmail(ctx, other.ID, "PAT@example.com"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("SetEmail conflict: %v", err)
	}
}

func TestSessionsExpireAndAreRevokedOnPasswordChange(t *testing.T) {
	st := storetest.New(t)
	u := newUser(t, st, "a@example.com")
	keep, _, _ := st.CreateSession(ctx, u.ID, time.Hour)
	other, _, _ := st.CreateSession(ctx, u.ID, time.Hour)
	expired, _, _ := st.CreateSession(ctx, u.ID, -time.Minute)

	if _, err := st.UserBySession(ctx, expired); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an expired session must not authenticate: %v", err)
	}
	if err := st.SetPassword(ctx, u.ID, "newhash", keep); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UserBySession(ctx, keep); err != nil {
		t.Fatalf("the current session should survive: %v", err)
	}
	if _, err := st.UserBySession(ctx, other); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other sessions should be revoked: %v", err)
	}
	if h, _ := st.UserHashByID(ctx, u.ID); h != "newhash" {
		t.Fatalf("hash not updated: %q", h)
	}
}

func TestJSONColumnsRoundTrip(t *testing.T) {
	st := storetest.New(t)
	u := newUser(t, st, "a@example.com")

	p := sim.DefaultProfile()
	p.FirstName, p.RetirementSpending, p.ReturnPre = "Alex", 72000.5, 0.0356
	if err := st.SaveProfile(ctx, u.ID, p); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveProfile(ctx, u.ID, p); err != nil { // upsert path
		t.Fatal(err)
	}
	got, exists, err := st.GetProfile(ctx, u.ID)
	if err != nil || !exists || got.FirstName != "Alex" || got.RetirementSpending != 72000.5 || got.ReturnPre != 0.0356 {
		t.Fatalf("profile round trip: %v %v %+v", err, exists, got)
	}

	a, err := st.CreateAccount(ctx, u.ID, sim.Account{Name: "IRA", Category: "investment", Owner: "self", Config: sim.AccountConfig{Balance: 1234.56, TaxTreatment: "tax_free", ExpectedReturn: 0.07}})
	if err != nil || a.ID == "" {
		t.Fatalf("create account: %v %+v", err, a)
	}
	back, err := st.GetAccount(ctx, u.ID, a.ID)
	if err != nil || back.Config.Balance != 1234.56 || back.Config.TaxTreatment != "tax_free" || back.Config.ExpectedReturn != 0.07 {
		t.Fatalf("account config round trip: %v %+v", err, back)
	}

	c, err := st.CreateCalculation(ctx, u.ID, store.Calculation{Kind: "plan", Name: "snap", Inputs: []byte(`{"a":1}`), Result: []byte(`{"b":[1,2,3]}`)})
	if err != nil || c.CreatedAt == "" {
		t.Fatalf("create calc: %v %+v", err, c)
	}
	cs, _ := st.ListCalculations(ctx, u.ID, "plan")
	if len(cs) != 1 || !strings.Contains(string(cs[0].Result), "[1, 2, 3]") && !strings.Contains(string(cs[0].Result), "[1,2,3]") {
		t.Fatalf("calc result: %+v", cs)
	}

	e, err := st.CreateEvent(ctx, u.ID, sim.Event{Kind: "income", Type: "pension", Name: "P", Owner: "self", StartAge: 65, AnnualAmount: 24000, InflationAdjusted: false, Taxable: true})
	if err != nil {
		t.Fatal(err)
	}
	evs, _ := st.ListEvents(ctx, u.ID)
	if len(evs) != 1 || evs[0].ID != e.ID || evs[0].InflationAdjusted || !evs[0].Taxable {
		t.Fatalf("booleans should round trip: %+v", evs)
	}
}

func TestLedgerIsOrderedByTimeNotInsertion(t *testing.T) {
	st := storetest.New(t)
	u := newUser(t, st, "a@example.com")
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	add := func(at time.Time, bal float64) {
		if _, err := st.AddLedger(ctx, u.ID, ledger.Entry{AccountID: acct7, AccountName: "X", Category: "cash", Owner: "self", Kind: ledger.KindUpdated, Reason: ledger.ReasonValue, Balance: bal, OccurredAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	// inserted out of order, including two entries within the same second
	add(base.Add(48*time.Hour), 3)
	add(base, 1)
	add(base.Add(500*time.Millisecond), 2)

	es, err := st.ListLedger(ctx, u.ID, "")
	if err != nil || len(es) != 3 {
		t.Fatalf("list: %v %v", err, es)
	}
	if es[0].Balance != 1 || es[1].Balance != 2 || es[2].Balance != 3 {
		t.Fatalf("expected chronological order by occurred_at, got %v %v %v", es[0].Balance, es[1].Balance, es[2].Balance)
	}
	if !es[1].OccurredAt.Equal(base.Add(500 * time.Millisecond)) {
		t.Fatalf("sub-second precision lost: %v", es[1].OccurredAt)
	}
	latest, ok, err := st.LatestLedgerTime(ctx, u.ID, acct7)
	if err != nil || !ok || !latest.Equal(base.Add(48*time.Hour)) {
		t.Fatalf("latest: %v %v %v", latest, ok, err)
	}
	if _, ok, _ := st.LatestLedgerTime(ctx, u.ID, "00000000-0000-4000-8000-000000000999"); ok {
		t.Fatal("an account with no entries has no latest time")
	}
}

func TestBudgetsLinkOnlyToOwnAccountsAndUnlinkOnDelete(t *testing.T) {
	st := storetest.New(t)
	alice, bob := newUser(t, st, "alice@example.com"), newUser(t, st, "bob@example.com")
	mine, _ := st.CreateAccount(ctx, alice.ID, sim.Account{Name: "Mine", Category: "cash", Owner: "self", Config: sim.AccountConfig{Balance: 1}})
	theirs, _ := st.CreateAccount(ctx, bob.ID, sim.Account{Name: "Theirs", Category: "cash", Owner: "self", Config: sim.AccountConfig{Balance: 1}})

	err := st.SaveBudget(ctx, alice.ID, store.Budget{Month: "2026-03", Items: []store.BudgetItem{
		{Section: "savings", Name: "mine", Amount: 100, AccountID: &mine.ID},
		{Section: "savings", Name: "theirs", Amount: 50, AccountID: &theirs.ID}, // someone else's account
		{Section: "income", Name: "pay", Amount: 1000},
	}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.GetBudget(ctx, alice.ID, "2026-03")
	if err != nil || len(b.Items) != 3 {
		t.Fatalf("get budget: %v %+v", err, b)
	}
	if b.Items[0].AccountID == nil || *b.Items[0].AccountID != mine.ID {
		t.Fatalf("link to own account should be kept: %+v", b.Items[0])
	}
	if b.Items[1].AccountID != nil {
		t.Fatalf("a link to another user's account must be dropped: %+v", b.Items[1])
	}

	// saving again replaces the items rather than appending
	if err := st.SaveBudget(ctx, alice.ID, store.Budget{Month: "2026-03", Notes: "n", Items: []store.BudgetItem{{Section: "income", Name: "only", Amount: 1}}}); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.GetBudget(ctx, alice.ID, "2026-03"); len(b.Items) != 1 || b.Notes != "n" {
		t.Fatalf("replace: %+v", b)
	}
	if ms, _ := st.ListBudgetMonths(ctx, alice.ID); len(ms) != 1 || ms[0].Income != 1 {
		t.Fatalf("month totals: %+v", ms)
	}

	// deleting the account unlinks the item
	_ = st.SaveBudget(ctx, alice.ID, store.Budget{Month: "2026-04", Items: []store.BudgetItem{{Section: "savings", Name: "x", Amount: 5, AccountID: &mine.ID}}})
	if err := st.DeleteAccount(ctx, alice.ID, mine.ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.GetBudget(ctx, alice.ID, "2026-04"); b.Items[0].AccountID != nil {
		t.Fatalf("deleting an account should unlink budget items: %+v", b.Items[0])
	}
	// and you can't touch another user's data
	if err := st.DeleteAccount(ctx, alice.ID, theirs.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-user delete should be not-found, got %v", err)
	}
}

func TestDeleteMemberReassignsOwnership(t *testing.T) {
	st := storetest.New(t)
	u := newUser(t, st, "a@example.com")
	m, _ := st.CreateMember(ctx, u.ID, store.Member{FirstName: "Rachel", Relationship: "spouse", BirthYear: 1990})
	key := m.ID
	_, _ = st.CreateAccount(ctx, u.ID, sim.Account{Name: "IRA", Category: "cash", Owner: key, Config: sim.AccountConfig{Balance: 1}})
	_, _ = st.CreateEvent(ctx, u.ID, sim.Event{Kind: "income", Type: "pension", Name: "P", Owner: key, StartAge: 65, AnnualAmount: 1})

	if err := st.DeleteMember(ctx, u.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	if as, _ := st.ListAccounts(ctx, u.ID); as[0].Owner != "joint" {
		t.Fatalf("account should become joint: %+v", as[0])
	}
	if es, _ := st.ListEvents(ctx, u.ID); es[0].Owner != "self" {
		t.Fatalf("event should fall back to the primary person: %+v", es[0])
	}
}

const acct7 = "00000000-0000-4000-8000-000000000007"
