package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/patrickeason/financial-wellness/backend/internal/api"
	"github.com/patrickeason/financial-wellness/backend/internal/store"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	addr := env("ADDR", ":8081")
	dbPath := env("DB_PATH", "./data/wellness.db")
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		if err := runBackup(dbPath, os.Args[2:]); err != nil {
			log.Fatalf("backup failed: %v", err)
		}
		return
	}
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			log.Fatalf("create data dir: %v", err)
		}
	}
	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := api.New(st, api.Config{
		CookieSecure:       env("COOKIE_SECURE", "") == "true",
		RegistrationClosed: strings.EqualFold(env("ALLOW_REGISTRATION", "true"), "false"),
	})
	srv.StartJanitor(ctx)

	hs := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	log.Printf("api listening on %s (db: %s)", addr, dbPath)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// runBackup writes a timestamped snapshot of the database into a directory
// (default: <db dir>/backups) and prunes old ones, keeping BACKUP_KEEP (14).
// It is safe to run while the server is up:  docker compose exec api api backup
func runBackup(dbPath string, args []string) error {
	dir := filepath.Join(filepath.Dir(dbPath), "backups")
	if len(args) > 0 && args[0] != "" {
		dir = args[0]
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	dest := filepath.Join(dir, "wellness-"+time.Now().UTC().Format("20060102-150405")+".db")
	if err := st.Backup(context.Background(), dest); err != nil {
		return err
	}
	if err := os.Chmod(dest, 0o600); err != nil {
		return err
	}
	fmt.Println(dest)

	keep, _ := strconv.Atoi(env("BACKUP_KEEP", "14"))
	if keep < 1 {
		keep = 14
	}
	files, _ := filepath.Glob(filepath.Join(dir, "wellness-*.db"))
	sort.Strings(files) // timestamped names sort chronologically
	for len(files) > keep {
		if err := os.Remove(files[0]); err != nil {
			log.Printf("could not prune %s: %v", files[0], err)
		}
		files = files[1:]
	}
	return nil
}
