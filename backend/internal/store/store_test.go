package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/patrickeason/financial-wellness/backend/internal/sim"
)

// Accounts that predate the ledger get exactly one starting entry, once.
func TestLedgerBackfillIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "t.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := st.CreateUser(ctx, "a@example.com", "x")
	// the store layer doesn't write the ledger, which simulates a pre-ledger account
	if _, err := st.CreateAccount(ctx, u.ID, sim.Account{Name: "Old", Category: "mortgage", Owner: "self",
		Config: sim.AccountConfig{Balance: 250000, HomeValue: 400000}}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	for i := 0; i < 3; i++ {
		st, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		es, err := st.ListLedger(ctx, u.ID, 0)
		if err != nil || len(es) != 1 {
			t.Fatalf("open #%d: %v entries=%v", i+1, err, es)
		}
		e := es[0]
		if e.Kind != "created" || e.Balance != 250000 || e.AssetValue != 400000 || e.AccountName != "Old" || e.OccurredAt.IsZero() {
			t.Fatalf("backfilled entry: %+v", e)
		}
		st.Close()
	}
}
