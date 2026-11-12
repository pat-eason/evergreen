package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is not set, e.g. postgres://evergreen:password@localhost:5432/evergreen?sslmode=disable")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := api.Config{
		CookieSecure:     env("COOKIE_SECURE", "") == "true",
		CookiePath:       env("COOKIE_PATH", "/"),
		RegistrationMode: os.Getenv("REGISTRATION_MODE"),
		RegistrationCode: os.Getenv("REGISTRATION_INVITE_CODE"),
	}
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	st, err := store.Open(ctx, dbURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer st.Close()

	srv := api.New(st, cfg)
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
	log.Printf("api listening on %s", addr)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
