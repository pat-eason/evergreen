package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/patrickeason/financial-wellness/backend/internal/store"
)

func TestRunBackupWritesPrivateSnapshotsAndPrunes(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "wellness.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(context.Background(), "a@example.com", "h"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	out := filepath.Join(dir, "out")
	t.Setenv("BACKUP_KEEP", "2")
	// pre-existing old snapshots: the oldest should be pruned
	os.MkdirAll(out, 0o700)
	for _, n := range []string{"wellness-20200101-000000.db", "wellness-20210101-000000.db"} {
		if err := os.WriteFile(filepath.Join(out, n), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := runBackup(db, []string{out}); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(out, "wellness-*.db"))
	if len(files) != 2 {
		t.Fatalf("expected 2 kept snapshots, got %v", files)
	}
	if _, err := os.Stat(filepath.Join(out, "wellness-20200101-000000.db")); err == nil {
		t.Fatal("oldest snapshot should have been pruned")
	}
	newest := files[len(files)-1]
	if fi, _ := os.Stat(newest); fi.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot permissions %v, want 0600", fi.Mode().Perm())
	}
	snap, err := store.Open(newest)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	if n, _ := snap.CountUsers(context.Background()); n != 1 {
		t.Fatalf("snapshot should contain the user, has %d", n)
	}
}
