// Package storetest gives tests an isolated Postgres-backed store.
package storetest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/patrickeason/financial-wellness/backend/internal/store"
)

// EnvURL names the environment variable holding the test database URL.
const EnvURL = "TEST_DATABASE_URL"

// URL returns the configured test database URL ("" when unset).
func URL() string { return os.Getenv(EnvURL) }

// Schema creates a fresh schema in the test database (dropped when the test
// ends) and returns the database URL and the schema name. Tests skip when
// TEST_DATABASE_URL is not set; CI sets it.
func Schema(t testing.TB) (url, schema string) {
	t.Helper()
	url = URL()
	if url == "" {
		t.Skipf("%s is not set; start a throwaway Postgres (see CLAUDE.md) to run database tests", EnvURL)
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	schema = "t_" + hex.EncodeToString(b)

	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	return url, schema
}

// New returns a store confined to its own fresh schema, so tests never see each
// other's data and can run in parallel.
func New(t testing.TB) *store.Store {
	t.Helper()
	url, schema := Schema(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := store.OpenSchema(ctx, url, schema)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() }) // runs before the schema is dropped
	return st
}
