package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/patrickeason/financial-wellness/backend/internal/store"
)

type client struct {
	t   *testing.T
	srv http.Handler
	ck  *http.Cookie
}

func newServer(t *testing.T) http.Handler {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, Config{})
}

func (c *client) do(method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, path, &buf)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if c.ck != nil {
		r.AddCookie(c.ck)
	}
	w := httptest.NewRecorder()
	c.srv.ServeHTTP(w, r)
	for _, ck := range w.Result().Cookies() {
		if ck.Name == cookieName && ck.MaxAge >= 0 {
			c.ck = ck
		}
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func TestAuthFlow(t *testing.T) {
	srv := newServer(t)
	c := &client{t: t, srv: srv}
	if w, _ := c.do("GET", "/api/me", nil); w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if w, _ := c.do("POST", "/api/auth/register", map[string]string{"email": "not-an-email", "password": "longenough"}); w.Code != 400 {
		t.Fatalf("short username: %d", w.Code)
	}
	if w, _ := c.do("POST", "/api/auth/register", map[string]string{"email": "pat@example.com", "password": "short"}); w.Code != 400 {
		t.Fatalf("short password: %d", w.Code)
	}
	if w, _ := c.do("POST", "/api/auth/register", map[string]string{"email": "pat@example.com", "password": "correct horse"}); w.Code != 200 {
		t.Fatalf("register: %d", w.Code)
	}
	if w, out := c.do("GET", "/api/me", nil); w.Code != 200 || out["email"] != "pat@example.com" {
		t.Fatalf("me: %d %v", w.Code, out)
	}
	if w, _ := (&client{t: t, srv: srv}).do("POST", "/api/auth/register", map[string]string{"email": "PAT@Example.com", "password": "correct horse"}); w.Code != 409 {
		t.Fatalf("dup (case-insens): %d", w.Code)
	}
	c.do("POST", "/api/auth/logout", nil)
	c.ck = nil
	if w, _ := c.do("POST", "/api/auth/login", map[string]string{"email": "pat@example.com", "password": "wrong password"}); w.Code != 401 {
		t.Fatalf("bad login: %d", w.Code)
	}
	if w, _ := c.do("POST", "/api/auth/login", map[string]string{"email": "pat@example.com", "password": "correct horse"}); w.Code != 200 {
		t.Fatalf("login: %d", w.Code)
	}
}

func TestLoginThrottle(t *testing.T) {
	srv := newServer(t)
	c := &client{t: t, srv: srv}
	c.do("POST", "/api/auth/register", map[string]string{"email": "pat@example.com", "password": "correct horse"})
	c = &client{t: t, srv: srv}
	var last int
	for i := 0; i < 7; i++ {
		w, _ := c.do("POST", "/api/auth/login", map[string]string{"email": "pat@example.com", "password": "nope nope nope"})
		last = w.Code
	}
	if last != 429 {
		t.Fatalf("expected throttle, got %d", last)
	}
}

func login(t *testing.T, srv http.Handler, name string) *client {
	c := &client{t: t, srv: srv}
	if w, _ := c.do("POST", "/api/auth/register", map[string]string{"email": name + "@example.com", "password": "correct horse"}); w.Code != 200 {
		t.Fatalf("register %s: %d", name, w.Code)
	}
	return c
}

func TestDataAndIsolation(t *testing.T) {
	srv := newServer(t)
	a, b := login(t, srv, "alice"), login(t, srv, "bob")

	w, acc := a.do("POST", "/api/accounts", map[string]any{"name": "401k", "category": "investment",
		"config": map[string]any{"balance": 250000, "tax_treatment": "tax_deferred", "monthly_contribution": 2000, "apr": 0.3}})
	if w.Code != 201 {
		t.Fatalf("create account: %d %v", w.Code, acc)
	}
	id := int(acc["id"].(float64))
	if cfg := acc["config"].(map[string]any); cfg["apr"] != nil {
		t.Fatalf("irrelevant field kept: %v", cfg)
	}
	if w, _ := a.do("POST", "/api/accounts", map[string]any{"name": "x", "category": "bogus"}); w.Code != 400 {
		t.Fatalf("bad category: %d", w.Code)
	}
	// bob cannot touch alice's account
	if w, _ := b.do("PUT", "/api/accounts/"+itoa(id), map[string]any{"name": "hax", "category": "cash", "config": map[string]any{}}); w.Code != 404 {
		t.Fatalf("cross-user update: %d", w.Code)
	}
	if w, _ := b.do("DELETE", "/api/accounts/"+itoa(id), nil); w.Code != 404 {
		t.Fatalf("cross-user delete: %d", w.Code)
	}

	// profile + event + budget
	if w, _ := a.do("PUT", "/api/profile", map[string]any{"current_age": 36, "retirement_age": 45, "end_age": 90, "retirement_spending": 24000, "return_pre": 0.07, "return_post": 0.055, "return_stdev": 0.12, "inflation": 0.025, "safe_withdrawal_rate": 0.04}); w.Code != 200 {
		t.Fatalf("profile: %d", w.Code)
	}
	if w, _ := a.do("POST", "/api/events", map[string]any{"kind": "income", "type": "social_security", "name": "SS", "start_age": 67, "annual_amount": 36000, "inflation_adjusted": true}); w.Code != 201 {
		t.Fatalf("event: %d", w.Code)
	}
	items := []map[string]any{
		{"section": "income", "name": "Pay", "expr": "=((1368.23 * 2) + (3192.56*2) + 1988)"},
		{"section": "savings", "name": "401k", "amount": 1988, "growth_rate": 0.07, "account_id": id},
		{"section": "expense", "name": "Mortgage", "amount": 2319.14},
	}
	w, bud := a.do("PUT", "/api/budgets/2026-03", map[string]any{"items": items})
	if w.Code != 200 {
		t.Fatalf("budget: %d %v", w.Code, bud)
	}
	sum := bud["summary"].(map[string]any)
	if sum["income"].(float64) != 11109.58 {
		t.Fatalf("expr eval: %v", sum["income"])
	}
	if w, _ := a.do("PUT", "/api/budgets/2026-13", map[string]any{"items": items}); w.Code != 400 {
		t.Fatalf("bad month: %d", w.Code)
	}
	if w, _ := a.do("PUT", "/api/budgets/2026-04", map[string]any{"items": []map[string]any{{"section": "income", "name": "x", "expr": "1/0"}}}); w.Code != 400 {
		t.Fatalf("bad expr: %d", w.Code)
	}
	if w, _ := a.do("POST", "/api/budgets/2026-04/copy", map[string]any{"from": "2026-03"}); w.Code != 200 {
		t.Fatalf("copy: %d", w.Code)
	}
	if w, _ := b.do("GET", "/api/budgets/2026-03", nil); w.Code != 404 {
		t.Fatalf("budget isolation: %d", w.Code)
	}
	if w, out := a.do("POST", "/api/budgets/2026-03/apply", nil); w.Code != 200 || out["accounts_updated"].(float64) != 1 {
		t.Fatalf("apply: %d %v", w.Code, out)
	}

	// simulation + freedom
	w, sim := a.do("POST", "/api/plan/simulate", map[string]any{"trials": 100})
	if w.Code != 200 || len(sim["years"].([]any)) != 55 {
		t.Fatalf("simulate: %d", w.Code)
	}
	w, fr := a.do("POST", "/api/freedom/analyze", map[string]any{})
	if w.Code != 200 || fr["freedom"] == nil {
		t.Fatalf("freedom: %d", w.Code)
	}
	if w, _ := a.do("POST", "/api/calculations", map[string]any{"kind": "freedom", "name": "snap", "inputs": fr["inputs"], "result": fr}); w.Code != 201 {
		t.Fatalf("save calc: %d", w.Code)
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func TestExpr(t *testing.T) {
	cases := map[string]float64{"1+2*3": 7, "=(1+2)*3": 9, "-(2+3)": -5, "$1,000.50": 1000.5, "10/4": 2.5, "2*-3": -6}
	for in, want := range cases {
		got, err := EvalExpr(in)
		if err != nil || got != want {
			t.Errorf("%q = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "1+", "(1", "1/0", "abc", "1 2 x"} {
		if _, err := EvalExpr(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestAccountChanges(t *testing.T) {
	srv := newServer(t)
	a := login(t, srv, "alice")
	b := login(t, srv, "bob")
	other := &client{t: t, srv: srv, ck: a.ck} // second session for alice

	if w, _ := a.do("PUT", "/api/me/email", map[string]any{"email": "bob@example.com", "password": "correct horse"}); w.Code != 409 {
		t.Fatalf("email conflict: %d", w.Code)
	}
	if w, _ := a.do("PUT", "/api/me/email", map[string]any{"email": "new@example.com", "password": "wrong"}); w.Code != 403 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	if w, _ := a.do("PUT", "/api/me/email", map[string]any{"email": "bad", "password": "correct horse"}); w.Code != 400 {
		t.Fatalf("bad email: %d", w.Code)
	}
	if w, out := a.do("PUT", "/api/me/email", map[string]any{"email": "New@Example.com", "password": "correct horse"}); w.Code != 200 || out["email"] != "new@example.com" {
		t.Fatalf("change email: %d %v", w.Code, out)
	}
	if _, out := a.do("GET", "/api/me", nil); out["email"] != "new@example.com" {
		t.Fatalf("me after change: %v", out)
	}

	// second login session for alice, to confirm password change signs it out
	c2 := &client{t: t, srv: srv}
	if w, _ := c2.do("POST", "/api/auth/login", map[string]string{"email": "new@example.com", "password": "correct horse"}); w.Code != 200 {
		t.Fatalf("login with new email: %d", w.Code)
	}
	_ = other
	if w, _ := a.do("PUT", "/api/me/password", map[string]any{"current_password": "correct horse", "new_password": "short"}); w.Code != 400 {
		t.Fatalf("short new password: %d", w.Code)
	}
	if w, _ := a.do("PUT", "/api/me/password", map[string]any{"current_password": "correct horse", "new_password": "a brand new pass"}); w.Code != 200 {
		t.Fatalf("change password: %d", w.Code)
	}
	if w, _ := a.do("GET", "/api/me", nil); w.Code != 200 {
		t.Fatalf("current session should survive: %d", w.Code)
	}
	if w, _ := c2.do("GET", "/api/me", nil); w.Code != 401 {
		t.Fatalf("other sessions should be signed out: %d", w.Code)
	}
	fresh := &client{t: t, srv: srv}
	if w, _ := fresh.do("POST", "/api/auth/login", map[string]string{"email": "new@example.com", "password": "correct horse"}); w.Code != 401 {
		t.Fatalf("old password should fail: %d", w.Code)
	}
	if w, _ := fresh.do("POST", "/api/auth/login", map[string]string{"email": "new@example.com", "password": "a brand new pass"}); w.Code != 200 {
		t.Fatalf("new password should work: %d", w.Code)
	}
	_ = b
}
