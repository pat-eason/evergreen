// Package api implements the HTTP JSON API.
package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
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
	// ipFailureLimit is how many failed sign-ins one address gets (across all
	// emails) before it is slowed down; per-email limiting is stricter.
	ipFailureLimit = 20
	cookieName     = "evergreen_session"
	sessionTTL     = 30 * 24 * time.Hour
)

// Config holds server settings.
type Config struct {
	// CookieSecure forces the Secure flag. It is also set automatically for
	// requests that arrive over TLS or through an HTTPS reverse proxy.
	CookieSecure bool
	// CookiePath scopes the session cookie (default "/"). Under a shared origin use the app's
	// prefix, e.g. "/evergreen", so other apps on the host never receive it.
	CookiePath string
	// RegistrationMode is who may create accounts: RegOpen, RegInvite or RegClosed.
	// The very first account on an empty server is always allowed, so an install can be bootstrapped.
	RegistrationMode string
	// RegistrationCode is the site-wide code required to register in RegInvite mode.
	RegistrationCode string
}

// Registration modes.
const (
	RegOpen   = "open"   // anyone can register
	RegInvite = "invite" // registering requires the invite code
	RegClosed = "closed" // nobody can register (after the first account)
)

// minRegistrationCode is the shortest registration code accepted at startup.
const minRegistrationCode = 8

// Validate normalizes the registration settings and rejects unsafe combinations.
func (c *Config) Validate() error {
	c.RegistrationMode = strings.ToLower(strings.TrimSpace(c.RegistrationMode))
	switch c.RegistrationMode {
	case "":
		c.RegistrationMode = RegOpen
	case RegOpen, RegInvite, RegClosed:
	default:
		return errors.New("REGISTRATION_MODE must be open, invite or closed")
	}
	c.RegistrationCode = strings.TrimSpace(c.RegistrationCode)
	if c.RegistrationMode == RegInvite && len(c.RegistrationCode) < minRegistrationCode {
		return errors.New("REGISTRATION_MODE=invite requires REGISTRATION_INVITE_CODE of at least 8 characters")
	}
	return nil
}

// Server is the API handler.
type Server struct {
	st  *store.Store
	cfg Config
	mux *http.ServeMux

	mu        sync.Mutex
	failures  map[string]*failure
	codeGuard *guard
}

type failure struct {
	count int
	until time.Time
}

type ctxKey struct{}

// New builds the API server.
func New(st *store.Store, cfg Config) *Server {
	s := &Server{st: st, cfg: cfg, mux: http.NewServeMux(), failures: map[string]*failure{}, codeGuard: newGuard(15*time.Minute, 5, 50)}
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
	m.HandleFunc("GET /api/config", s.publicConfig)
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

	m.HandleFunc("POST /api/accounts/{id}/value", s.auth(s.recordValue))
	m.HandleFunc("GET /api/ledger", s.auth(s.listLedger))
	m.HandleFunc("GET /api/ledger/history", s.auth(s.ledgerHistory))

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
			s.clearCookie(w, r)
			writeErr(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func (s *Server) cookiePath() string {
	if p := strings.TrimRight(s.cfg.CookiePath, "/"); p != "" {
		return p
	}
	return "/"
}

// secure reports whether the client connection is HTTPS (directly, or via a
// reverse proxy that says so), in which case cookies get the Secure flag.
func (s *Server) secure(r *http.Request) bool {
	return s.cfg.CookieSecure || r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setCookie(w http.ResponseWriter, r *http.Request, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: s.cookiePath(), Expires: exp,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secure(r),
	})
}

func (s *Server) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: s.cookiePath(), MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secure(r)})
}

// clientIP returns the caller's address. The API is only reachable through the
// reverse proxy, which overwrites X-Forwarded-For with the real client address.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			xff = xff[:i]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// registrationMode is the mode in force right now: the configured one, except
// that an empty server always accepts its first account without a code.
func (s *Server) registrationMode(r *http.Request) (string, error) {
	mode := s.cfg.RegistrationMode
	if mode == "" || mode == RegOpen {
		return RegOpen, nil
	}
	n, err := s.st.CountUsers(r.Context())
	if err != nil {
		return "", err
	}
	if n == 0 {
		return RegOpen, nil
	}
	return mode, nil
}

func (s *Server) publicConfig(w http.ResponseWriter, r *http.Request) {
	mode, err := s.registrationMode(r)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"registration": mode})
}

// checkRegistrationCode enforces invite-only mode: a constant-time comparison behind a per-IP lockout
// and a global failure ceiling (5 per address and 50 overall per 15 minutes, then 429).
func (s *Server) checkRegistrationCode(w http.ResponseWriter, r *http.Request, given string) bool {
	ip := clientIP(r)
	if ok, wait := s.codeGuard.allowed(ip); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return false
	}
	a := sha256.Sum256([]byte(strings.TrimSpace(given)))
	b := sha256.Sum256([]byte(s.cfg.RegistrationCode))
	if subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
		s.codeGuard.fail(ip)
		writeErr(w, http.StatusForbidden, "that registration code isn't valid")
		return false
	}
	return true
}

// ---------- auth ----------

type credentials struct {
	Email            string `json:"email"`
	Password         string `json:"password"`
	RegistrationCode string `json:"registration_code,omitempty"`
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
	mode, err := s.registrationMode(r)
	if err != nil {
		serverErr(w, err)
		return
	}
	if mode == RegClosed {
		writeErr(w, http.StatusForbidden, "registration is closed on this server")
		return
	}
	var c credentials
	if !readJSON(w, r, &c) {
		return
	}
	if mode == RegInvite && !s.checkRegistrationCode(w, r, c.RegistrationCode) {
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
	s.setCookie(w, r, token, exp)
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

func (s *Server) recordFailure(key string) { s.recordFailureAfter(key, 5) }

// recordFailureAfter counts a failure and starts backing off once `limit`
// failures have accumulated for the key.
func (s *Server) recordFailureAfter(key string, limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[key]
	if f == nil {
		f = &failure{}
		s.failures[key] = f
	}
	f.count++
	if f.count >= limit {
		d := time.Duration(f.count-limit+1) * 15 * time.Second
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
	ipKey := "ip:" + clientIP(r)
	if wait, ok := s.throttled(ipKey); ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try again shortly")
		return
	}
	if wait, ok := s.throttled(key); ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try again shortly")
		return
	}
	u, hash, err := s.st.UserByEmail(r.Context(), key)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(c.Password))
		s.recordFailure(key)
		s.recordFailureAfter(ipKey, ipFailureLimit)
		writeErr(w, 401, "invalid email or password")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(c.Password)) != nil {
		s.recordFailure(key)
		s.recordFailureAfter(ipKey, ipFailureLimit)
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
	s.clearCookie(w, r)
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
	key := "uid:" + u.ID
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
