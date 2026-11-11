// Package api implements the HTTP JSON API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/patrickeason/financial-wellness/backend/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	cookieName = "fw_session"
	sessionTTL = 30 * 24 * time.Hour
)

// Config holds server settings.
type Config struct {
	CookieSecure bool
}

// Server is the API handler.
type Server struct {
	st  *store.Store
	cfg Config
	mux *http.ServeMux

	mu       sync.Mutex
	failures map[string]*failure
}

type failure struct {
	count int
	until time.Time
}

type ctxKey struct{}

// New builds the API server.
func New(st *store.Store, cfg Config) *Server {
	s := &Server{st: st, cfg: cfg, mux: http.NewServeMux(), failures: map[string]*failure{}}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	// Mutating requests must be JSON (blocks simple cross-site form posts).
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.ContentLength != 0 {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeErr(w, http.StatusUnsupportedMediaType, "content-type must be application/json")
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })

	m.HandleFunc("POST /api/auth/register", s.register)
	m.HandleFunc("POST /api/auth/login", s.login)
	m.HandleFunc("POST /api/auth/logout", s.logout)
	m.HandleFunc("GET /api/me", s.auth(s.me))
	m.HandleFunc("PUT /api/me/email", s.auth(s.changeEmail))
	m.HandleFunc("PUT /api/me/password", s.auth(s.changePassword))

	m.HandleFunc("GET /api/members", s.auth(s.listMembers))
	m.HandleFunc("POST /api/members", s.auth(s.createMember))
	m.HandleFunc("PUT /api/members/{id}", s.auth(s.updateMember))
	m.HandleFunc("DELETE /api/members/{id}", s.auth(s.deleteMember))

	m.HandleFunc("GET /api/profile", s.auth(s.getProfile))
	m.HandleFunc("PUT /api/profile", s.auth(s.putProfile))

	m.HandleFunc("GET /api/accounts", s.auth(s.listAccounts))
	m.HandleFunc("POST /api/accounts", s.auth(s.createAccount))
	m.HandleFunc("PUT /api/accounts/{id}", s.auth(s.updateAccount))
	m.HandleFunc("DELETE /api/accounts/{id}", s.auth(s.deleteAccount))

	m.HandleFunc("GET /api/events", s.auth(s.listEvents))
	m.HandleFunc("POST /api/events", s.auth(s.createEvent))
	m.HandleFunc("PUT /api/events/{id}", s.auth(s.updateEvent))
	m.HandleFunc("DELETE /api/events/{id}", s.auth(s.deleteEvent))

	m.HandleFunc("GET /api/budgets", s.auth(s.listBudgets))
	m.HandleFunc("GET /api/budgets/{month}", s.auth(s.getBudget))
	m.HandleFunc("PUT /api/budgets/{month}", s.auth(s.putBudget))
	m.HandleFunc("DELETE /api/budgets/{month}", s.auth(s.deleteBudget))
	m.HandleFunc("POST /api/budgets/{month}/copy", s.auth(s.copyBudget))
	m.HandleFunc("POST /api/budgets/{month}/apply", s.auth(s.applyBudget))

	m.HandleFunc("POST /api/plan/simulate", s.auth(s.simulate))
	m.HandleFunc("POST /api/freedom/analyze", s.auth(s.freedom))

	m.HandleFunc("GET /api/calculations", s.auth(s.listCalcs))
	m.HandleFunc("POST /api/calculations", s.auth(s.createCalc))
	m.HandleFunc("DELETE /api/calculations/{id}", s.auth(s.deleteCalc))
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func serverErr(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func notFoundOr500(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	serverErr(w, err)
}

func userFrom(ctx context.Context) store.User {
	u, _ := ctx.Value(ctxKey{}).(store.User)
	return u
}

// auth wraps a handler requiring a valid session cookie.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || c.Value == "" {
			writeErr(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		u, err := s.st.UserBySession(r.Context(), c.Value)
		if err != nil {
			s.clearCookie(w)
			writeErr(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func (s *Server) setCookie(w http.ResponseWriter, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", Expires: exp,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.cfg.CookieSecure,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.cfg.CookieSecure})
}

// ---------- auth ----------

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// normalizeEmail trims and lower-cases an address and reports whether it is a
// plain, well-formed email (no display name, a dotted domain).
func normalizeEmail(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 6 || len(s) > 254 {
		return "", false
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s {
		return "", false
	}
	at := strings.LastIndexByte(s, '@')
	domain := s[at+1:]
	if at < 1 || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", false
	}
	return s, true
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !readJSON(w, r, &c) {
		return
	}
	email, ok := normalizeEmail(c.Email)
	if !ok {
		writeErr(w, 400, "enter a valid email address")
		return
	}
	if len(c.Password) < 8 {
		writeErr(w, 400, "password must be at least 8 characters")
		return
	}
	if len(c.Password) > 72 { // bcrypt limit
		writeErr(w, 400, "password must be at most 72 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		serverErr(w, err)
		return
	}
	u, err := s.st.CreateUser(r.Context(), email, string(hash))
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, 409, "an account with that email already exists")
		return
	}
	if err != nil {
		serverErr(w, err)
		return
	}
	s.startSession(w, r, u)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User) {
	token, exp, err := s.st.CreateSession(r.Context(), u.ID, sessionTTL)
	if err != nil {
		serverErr(w, err)
		return
	}
	s.setCookie(w, token, exp)
	writeJSON(w, 200, u)
}

// dummyHash equalises timing for unknown usernames.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)

func (s *Server) throttled(key string) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.failures[key]; ok && time.Now().Before(f.until) {
		return time.Until(f.until), true
	}
	return 0, false
}

func (s *Server) recordFailure(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[key]
	if f == nil {
		f = &failure{}
		s.failures[key] = f
	}
	f.count++
	if f.count >= 5 {
		d := time.Duration(f.count-4) * 15 * time.Second
		if d > 5*time.Minute {
			d = 5 * time.Minute
		}
		f.until = time.Now().Add(d)
	}
	if len(s.failures) > 10000 { // bound memory
		s.failures = map[string]*failure{key: f}
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if !readJSON(w, r, &c) {
		return
	}
	key := strings.ToLower(strings.TrimSpace(c.Email))
	if wait, ok := s.throttled(key); ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try again shortly")
		return
	}
	u, hash, err := s.st.UserByEmail(r.Context(), key)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(c.Password))
		s.recordFailure(key)
		writeErr(w, 401, "invalid email or password")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(c.Password)) != nil {
		s.recordFailure(key)
		writeErr(w, 401, "invalid email or password")
		return
	}
	s.mu.Lock()
	delete(s.failures, key)
	s.mu.Unlock()
	s.startSession(w, r, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		_ = s.st.DeleteSession(r.Context(), c.Value)
	}
	s.clearCookie(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, userFrom(r.Context()))
}

// StartJanitor periodically removes expired sessions until ctx is done.
func (s *Server) StartJanitor(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.st.PurgeExpiredSessions(ctx)
			}
		}
	}()
}

// checkPassword verifies the signed-in user's current password, applying the
// same failure throttle as login.
func (s *Server) checkPassword(w http.ResponseWriter, r *http.Request, u store.User, pw string) bool {
	key := "uid:" + strconv.FormatInt(u.ID, 10)
	if wait, ok := s.throttled(key); ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try again shortly")
		return false
	}
	hash, err := s.st.UserHashByID(r.Context(), u.ID)
	if err != nil {
		serverErr(w, err)
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) != nil {
		s.recordFailure(key)
		writeErr(w, http.StatusForbidden, "current password is incorrect")
		return false
	}
	s.mu.Lock()
	delete(s.failures, key)
	s.mu.Unlock()
	return true
}

func (s *Server) changeEmail(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	email, ok := normalizeEmail(body.Email)
	if !ok {
		writeErr(w, 400, "enter a valid email address")
		return
	}
	if !s.checkPassword(w, r, u, body.Password) {
		return
	}
	if err := s.st.SetEmail(r.Context(), u.ID, email); errors.Is(err, store.ErrConflict) {
		writeErr(w, 409, "an account with that email already exists")
		return
	} else if err != nil {
		serverErr(w, err)
		return
	}
	u.Email = email
	writeJSON(w, 200, u)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	var body struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.New) < 8 || len(body.New) > 72 {
		writeErr(w, 400, "new password must be 8-72 characters")
		return
	}
	if !s.checkPassword(w, r, u, body.Current) {
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.New), bcrypt.DefaultCost)
	if err != nil {
		serverErr(w, err)
		return
	}
	c, _ := r.Cookie(cookieName)
	if err := s.st.SetPassword(r.Context(), u.ID, string(hash), c.Value); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
