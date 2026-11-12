package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/patrickeason/financial-wellness/backend/internal/store/storetest"
)

type client struct {
	t   *testing.T
	srv http.Handler
	ck  *http.Cookie
}

func newServer(t *testing.T) http.Handler {
	return New(storetest.New(t), Config{})
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

func TestHouseholdAndOwnership(t *testing.T) {
	srv := newServer(t)
	a, b := login(t, srv, "alice"), login(t, srv, "bob")

	// profile: age is stored as a birth year and derived back
	prof := map[string]any{"first_name": "  Pat ", "last_name": "Smith", "current_age": 36, "retirement_age": 60, "end_age": 90, "retirement_spending": 40000}
	if w, out := a.do("PUT", "/api/profile", prof); w.Code != 200 {
		t.Fatalf("profile: %d %v", w.Code, out)
	}
	_, got := a.do("GET", "/api/profile", nil)
	p := got["profile"].(map[string]any)
	if p["first_name"] != "Pat" || p["current_age"].(float64) != 36 || p["birth_year"].(float64) < 1900 {
		t.Fatalf("profile roundtrip: %v", p)
	}

	// members
	if w, _ := a.do("POST", "/api/members", map[string]any{"first_name": "", "age": 30}); w.Code != 400 {
		t.Fatalf("blank name: %d", w.Code)
	}
	if w, _ := a.do("POST", "/api/members", map[string]any{"first_name": "X", "age": 30, "relationship": "cousin"}); w.Code != 400 {
		t.Fatalf("bad relationship: %d", w.Code)
	}
	w, m := a.do("POST", "/api/members", map[string]any{"first_name": "Rachel", "last_name": "Smith", "relationship": "spouse", "age": 30})
	if w.Code != 201 || m["age"].(float64) != 30 {
		t.Fatalf("create member: %d %v", w.Code, m)
	}
	mid := itoa(int(m["id"].(float64)))
	if w, _ := b.do("PUT", "/api/members/"+mid, map[string]any{"first_name": "Hax", "age": 5}); w.Code != 404 {
		t.Fatalf("cross-user member update: %d", w.Code)
	}

	// owners: validated, and not usable across users
	acct := func(owner string) map[string]any {
		return map[string]any{"name": "IRA", "category": "investment", "owner": owner, "config": map[string]any{"balance": 1000}}
	}
	if w, out := a.do("POST", "/api/accounts", acct(mid)); w.Code != 201 || out["owner"] != mid {
		t.Fatalf("member-owned account: %d %v", w.Code, out)
	}
	if w, out := a.do("POST", "/api/accounts", acct("joint")); w.Code != 201 || out["owner"] != "joint" {
		t.Fatalf("joint account: %d %v", w.Code, out)
	}
	if w, out := a.do("POST", "/api/accounts", acct("")); w.Code != 201 || out["owner"] != "self" {
		t.Fatalf("default owner: %d %v", w.Code, out)
	}
	if w, _ := b.do("POST", "/api/accounts", acct(mid)); w.Code != 400 {
		t.Fatalf("other user's member as owner: %d", w.Code)
	}
	if w, _ := a.do("POST", "/api/accounts", acct("999")); w.Code != 400 {
		t.Fatalf("unknown owner: %d", w.Code)
	}
	ev := map[string]any{"kind": "income", "type": "social_security", "name": "Rachel SS", "owner": mid, "start_age": 67, "annual_amount": 30000, "taxable": true}
	if w, _ := a.do("POST", "/api/events", map[string]any{"kind": "income", "name": "x", "owner": "joint", "start_age": 67, "annual_amount": 1}); w.Code != 400 {
		t.Fatalf("joint event should be rejected: %d", w.Code)
	}
	if w, out := a.do("POST", "/api/events", ev); w.Code != 201 || out["owner"] != mid {
		t.Fatalf("member event: %d %v", w.Code, out)
	}

	// engine runs on the primary's timeline: spouse is 6 years younger, so
	// their age-67 benefit starts when the primary is 73.
	_, sim := a.do("POST", "/api/plan/simulate", map[string]any{"trials": 50, "fast": true})
	first := -1
	for _, y := range sim["years"].([]any) {
		r := y.(map[string]any)
		if r["other_income"].(float64) > 0 {
			first = int(r["age"].(float64))
			break
		}
	}
	if first != 73 {
		t.Fatalf("spouse benefit should start at primary age 73, got %d", first)
	}

	// deleting a member reassigns their accounts to joint and events to self
	if w, _ := a.do("DELETE", "/api/members/"+mid, nil); w.Code != 204 {
		t.Fatalf("delete member: %d", w.Code)
	}
	for i := range []int{0} {
		_ = i
	}
	rec, _ := a.do("GET", "/api/accounts", nil)
	var accts []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &accts)
	for _, ac := range accts {
		if ac["owner"] == mid {
			t.Fatalf("account still owned by deleted member: %v", ac)
		}
	}
	rec, _ = a.do("GET", "/api/events", nil)
	var evs []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &evs)
	if len(evs) != 1 || evs[0]["owner"] != "self" {
		t.Fatalf("event owner after delete: %v", evs)
	}
}

func ledgerEntries(t *testing.T, c *client, query string) []map[string]any {
	t.Helper()
	w, out := c.do("GET", "/api/ledger"+query, nil)
	if w.Code != 200 {
		t.Fatalf("ledger: %d", w.Code)
	}
	var es []map[string]any
	for _, e := range out["entries"].([]any) {
		es = append(es, e.(map[string]any))
	}
	return es
}

func TestLedgerRecordsAccountChanges(t *testing.T) {
	srv := newServer(t)
	a, b := login(t, srv, "alice"), login(t, srv, "bob")

	w, acc := a.do("POST", "/api/accounts", map[string]any{"name": "Brokerage", "category": "investment", "config": map[string]any{"balance": 10000, "tax_treatment": "taxable"}})
	if w.Code != 201 {
		t.Fatalf("create: %d", w.Code)
	}
	id := int(acc["id"].(float64))
	path := "/api/accounts/" + itoa(id)

	es := ledgerEntries(t, a, "")
	if len(es) != 1 || es[0]["kind"] != "created" || es[0]["balance"].(float64) != 10000 {
		t.Fatalf("after create: %v", es)
	}

	// a balance edit through the normal form is a value change
	update := func(name string, bal float64) {
		w, _ := a.do("PUT", path, map[string]any{"name": name, "category": "investment", "config": map[string]any{"balance": bal, "tax_treatment": "taxable"}})
		if w.Code != 200 {
			t.Fatalf("update: %d", w.Code)
		}
	}
	update("Brokerage", 11500)
	es = ledgerEntries(t, a, "")
	if es[0]["kind"] != "updated" || es[0]["reason"] != "value" || es[0]["delta"].(float64) != 1500 || es[0]["previous"].(float64) != 10000 {
		t.Fatalf("after balance edit: %v", es[0])
	}
	update("Main brokerage", 11500) // rename only
	es = ledgerEntries(t, a, "")
	if es[0]["kind"] != "edited" || es[0]["delta"].(float64) != 0 || len(es) != 3 {
		t.Fatalf("after rename: %v", es)
	}
	update("Main brokerage", 11500) // no change at all
	if got := len(ledgerEntries(t, a, "")); got != 3 {
		t.Fatalf("a no-op save must not add an entry, have %d", got)
	}

	// dedicated value endpoint: deposit
	w, out := a.do("POST", path+"/value", map[string]any{"balance": 13500, "reason": "deposit", "note": "bonus"})
	if w.Code != 200 || out["applied"] != true {
		t.Fatalf("deposit: %d %v", w.Code, out)
	}
	if w, _ := a.do("POST", path+"/value", map[string]any{"balance": 1, "reason": "payment"}); w.Code != 400 {
		t.Fatalf("liability reason on an asset should be rejected: %d", w.Code)
	}
	if w, _ := a.do("POST", path+"/value", map[string]any{"balance": 1, "as_of": "2999-01-01"}); w.Code != 400 {
		t.Fatalf("future date should be rejected: %d", w.Code)
	}
	if w, _ := a.do("POST", path+"/value", map[string]any{"balance": -5}); w.Code != 400 {
		t.Fatalf("negative balance: %d", w.Code)
	}

	// a backfill dated before the latest entry adds history but leaves the live balance alone
	past := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	w, out = a.do("POST", path+"/value", map[string]any{"balance": 9000, "as_of": past})
	if w.Code != 200 || out["applied"] != false {
		t.Fatalf("backfill: %d %v", w.Code, out)
	}
	rec, _ := a.do("GET", "/api/accounts", nil)
	var accts []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &accts)
	if accts[0]["config"].(map[string]any)["balance"].(float64) != 13500 {
		t.Fatalf("live balance must be unchanged by a backfill: %v", accts[0])
	}

	// history: growth vs contributions
	w, h := a.do("GET", "/api/ledger/history?interval=day&by_account=1", nil)
	if w.Code != 200 {
		t.Fatalf("history: %d", w.Code)
	}
	pts := h["points"].([]any)
	last := pts[len(pts)-1].(map[string]any)
	// The backfilled 9,000 (30 days ago) is the opening balance, then a 2,000
	// deposit: 11,000 put in. Balance 13,500 -> 2,500 of growth.
	if last["net_contributions"].(float64) != 11000 || last["growth"].(float64) != 2500 || last["net_worth"].(float64) != 13500 {
		t.Fatalf("history end point: %v", last)
	}
	if first := pts[0].(map[string]any); first["net_worth"].(float64) != 9000 {
		t.Fatalf("history should start at the backfilled value: %v", first)
	}
	if _, ok := h["series"].(map[string]any)[itoa(id)]; !ok {
		t.Fatalf("per-account series missing: %v", h["series"])
	}

	// isolation
	if es := ledgerEntries(t, b, ""); len(es) != 0 {
		t.Fatalf("bob sees alice's ledger: %v", es)
	}
	if w, _ := b.do("POST", path+"/value", map[string]any{"balance": 1}); w.Code != 404 {
		t.Fatalf("cross-user value update: %d", w.Code)
	}

	// deleting keeps the history and records the removal
	if w, _ := a.do("DELETE", path, nil); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	es = ledgerEntries(t, a, "?account_id="+itoa(id))
	if es[0]["kind"] != "deleted" || es[0]["balance"].(float64) != 0 || es[0]["account_name"] != "Main brokerage" {
		t.Fatalf("after delete: %v", es[0])
	}
	_, h = a.do("GET", "/api/ledger/history?interval=day", nil)
	pts = h["points"].([]any)
	if end := pts[len(pts)-1].(map[string]any); end["net_worth"].(float64) != 0 || end["growth"].(float64) != 2500 {
		t.Fatalf("after delete the account stops counting but earned growth stays: %v", end)
	}
}

func TestLedgerMortgageTracksHomeValue(t *testing.T) {
	srv := newServer(t)
	a := login(t, srv, "alice")
	_, acc := a.do("POST", "/api/accounts", map[string]any{"name": "Home", "category": "mortgage", "config": map[string]any{"balance": 300000, "home_value": 450000, "interest_rate": 0.04}})
	id := itoa(int(acc["id"].(float64)))
	if w, _ := a.do("POST", "/api/accounts/"+id+"/value", map[string]any{"balance": 298000, "home_value": 460000, "reason": "payment"}); w.Code != 200 {
		t.Fatalf("mortgage value: %d", w.Code)
	}
	_, h := a.do("GET", "/api/ledger/history?interval=day", nil)
	pts := h["points"].([]any)
	last := pts[len(pts)-1].(map[string]any)
	if last["home"].(float64) != 460000 || last["mortgage"].(float64) != 298000 || last["net_worth"].(float64) != 162000 {
		t.Fatalf("mortgage point: %v", last)
	}
}

func newServerWith(t *testing.T, cfg Config) http.Handler {
	return New(storetest.New(t), cfg)
}

func TestClosedRegistrationAllowsOnlyTheFirstAccount(t *testing.T) {
	srv := newServerWith(t, Config{RegistrationMode: RegClosed})
	c := &client{t: t, srv: srv}

	_, cfg := c.do("GET", "/api/config", nil)
	if cfg["registration"] != "open" {
		t.Fatalf("a fresh install must allow bootstrap sign-up: %v", cfg)
	}
	if w, _ := c.do("POST", "/api/auth/register", map[string]string{"email": "owner@example.com", "password": "correct horse"}); w.Code != 200 {
		t.Fatalf("first account: %d", w.Code)
	}
	_, cfg = c.do("GET", "/api/config", nil)
	if cfg["registration"] != "closed" {
		t.Fatalf("registration should lock after the first account: %v", cfg)
	}
	other := &client{t: t, srv: srv}
	if w, _ := other.do("POST", "/api/auth/register", map[string]string{"email": "intruder@example.com", "password": "correct horse"}); w.Code != 403 {
		t.Fatalf("second account on a closed server: %d", w.Code)
	}
	// the owner can still sign in
	if w, _ := (&client{t: t, srv: srv}).do("POST", "/api/auth/login", map[string]string{"email": "owner@example.com", "password": "correct horse"}); w.Code != 200 {
		t.Fatalf("owner login: %d", w.Code)
	}
}

func TestOpenRegistrationByDefault(t *testing.T) {
	srv := newServer(t)
	for _, n := range []string{"a", "b", "c"} {
		login(t, srv, n)
	}
	_, cfg := (&client{t: t, srv: srv}).do("GET", "/api/config", nil)
	if cfg["registration"] != "open" {
		t.Fatalf("default config: %v", cfg)
	}
}

func TestCookieSecureBehindHTTPSProxy(t *testing.T) {
	srv := newServer(t)
	post := func(proto string) *http.Cookie {
		var buf bytes.Buffer
		json.NewEncoder(&buf).Encode(map[string]string{"email": proto + "@example.com", "password": "correct horse"})
		r := httptest.NewRequest("POST", "/api/auth/register", &buf)
		r.Header.Set("Content-Type", "application/json")
		if proto == "https" {
			r.Header.Set("X-Forwarded-Proto", "https")
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		for _, c := range w.Result().Cookies() {
			if c.Name == cookieName {
				return c
			}
		}
		t.Fatalf("no session cookie for %s", proto)
		return nil
	}
	if c := post("https"); !c.Secure || !c.HttpOnly {
		t.Fatalf("proxied HTTPS cookie must be Secure+HttpOnly: %+v", c)
	}
	if c := post("http"); c.Secure {
		t.Fatalf("plain HTTP (local dev) must not set Secure or the browser would drop it: %+v", c)
	}
	forced := newServerWith(t, Config{CookieSecure: true})
	w, _ := (&client{t: t, srv: forced}).do("POST", "/api/auth/register", map[string]string{"email": "x@example.com", "password": "correct horse"})
	if cs := w.Result().Cookies(); len(cs) == 0 || !cs[0].Secure {
		t.Fatal("COOKIE_SECURE should force the flag")
	}
}

func TestClientIPAndPerIPThrottle(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.9:5555"
	if clientIP(r) != "10.0.0.9" {
		t.Fatalf("remote addr: %s", clientIP(r))
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.2")
	if clientIP(r) != "203.0.113.7" {
		t.Fatalf("forwarded: %s", clientIP(r))
	}

	// one address guessing across many different emails is slowed down even
	// though no single email hits its own limit
	srv := newServer(t)
	var last int
	for i := 0; i < ipFailureLimit+2; i++ {
		var buf bytes.Buffer
		json.NewEncoder(&buf).Encode(map[string]string{"email": itoa(i) + "@example.com", "password": "wrong password"})
		req := httptest.NewRequest("POST", "/api/auth/login", &buf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "198.51.100.4")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		last = w.Code
	}
	if last != 429 {
		t.Fatalf("expected per-IP throttle, got %d", last)
	}
	// a different address is unaffected
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(map[string]string{"email": "z@example.com", "password": "wrong password"})
	req := httptest.NewRequest("POST", "/api/auth/login", &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "198.51.100.99")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("other IP should just get 401, got %d", w.Code)
	}
}

func TestHistoryAndLedgerDateRanges(t *testing.T) {
	srv := newServer(t)
	a := login(t, srv, "alice")
	_, acc := a.do("POST", "/api/accounts", map[string]any{"name": "IRA", "category": "investment", "config": map[string]any{"balance": 1000, "tax_treatment": "tax_free"}})
	path := "/api/accounts/" + itoa(int(acc["id"].(float64))) + "/value"
	rec := func(date string, bal float64) {
		if w, out := a.do("POST", path, map[string]any{"balance": bal, "as_of": date}); w.Code != 200 {
			t.Fatalf("record %s: %d %v", date, w.Code, out)
		}
	}
	rec("2026-01-10", 800)
	rec("2026-02-10", 900)
	rec("2026-03-10", 1100)

	// history clipped to a custom window
	w, h := a.do("GET", "/api/ledger/history?from=2026-02-01&to=2026-02-28&interval=day", nil)
	if w.Code != 200 {
		t.Fatalf("history: %d", w.Code)
	}
	pts := h["points"].([]any)
	if pts[0].(map[string]any)["date"] != "2026-02-01" || pts[len(pts)-1].(map[string]any)["date"] != "2026-02-28" {
		t.Fatalf("window: %v .. %v", pts[0], pts[len(pts)-1])
	}
	if end := pts[len(pts)-1].(map[string]any); end["investments"].(float64) != 900 {
		t.Fatalf("March's value must not leak into a February window: %v", end)
	}
	if sum := h["summary"].(map[string]any); sum["growth"].(float64) != 100 {
		t.Fatalf("growth Jan 31 -> Feb 28 should be +100 (800 -> 900): %v", sum)
	}

	// ledger entries within the window; "to" is inclusive of its whole day
	es := ledgerEntries(t, a, "?from=2026-02-10&to=2026-02-10")
	if len(es) != 1 || es[0]["balance"].(float64) != 900 {
		t.Fatalf("single-day window: %v", es)
	}
	if es := ledgerEntries(t, a, "?from=2026-01-01&to=2026-03-31"); len(es) != 3 {
		t.Fatalf("three dated entries expected in Q1, got %d", len(es))
	}
	if es := ledgerEntries(t, a, "?to=2026-01-31"); len(es) != 1 {
		t.Fatalf("only the January entry should be on or before Jan 31, got %d", len(es))
	}

	for _, bad := range []string{"?from=nope", "?to=2026-13-40", "?from=2026-03-01&to=2026-02-01", "?from=1800-01-01"} {
		if w, _ := a.do("GET", "/api/ledger/history"+bad, nil); w.Code != 400 {
			t.Fatalf("history%s should be rejected, got %d", bad, w.Code)
		}
		if w, _ := a.do("GET", "/api/ledger"+bad, nil); w.Code != 400 {
			t.Fatalf("ledger%s should be rejected, got %d", bad, w.Code)
		}
	}
}

func TestInviteRegistration(t *testing.T) {
	const code = "correct-horse-battery"
	cfg := Config{RegistrationMode: RegInvite, RegistrationCode: code}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	srv := newServerWith(t, cfg)
	reg := func(c *client, email, invite string) int {
		w, _ := c.do("POST", "/api/auth/register", map[string]string{"email": email, "password": "correct horse", "registration_code": invite})
		return w.Code
	}
	c := &client{t: t, srv: srv}
	// an empty server accepts its first account without a code
	if got := reg(c, "owner@example.com", ""); got != 200 {
		t.Fatalf("bootstrap: %d", got)
	}
	_, pub := c.do("GET", "/api/config", nil)
	if pub["registration"] != "invite" {
		t.Fatalf("config: %v", pub)
	}
	guest := &client{t: t, srv: srv}
	if got := reg(guest, "a@example.com", ""); got != 403 {
		t.Fatalf("missing code: %d", got)
	}
	if got := reg(guest, "a@example.com", "wrong"); got != 403 {
		t.Fatalf("wrong code: %d", got)
	}
	if got := reg(guest, "a@example.com", code); got != 200 {
		t.Fatalf("right code: %d", got)
	}
}

func TestInviteGuessingIsThrottled(t *testing.T) {
	cfg := Config{RegistrationMode: RegInvite, RegistrationCode: "correct-horse-battery"}
	srv := newServerWith(t, cfg)
	(&client{t: t, srv: srv}).do("POST", "/api/auth/register", map[string]string{"email": "owner@example.com", "password": "correct horse"})
	c := &client{t: t, srv: srv}
	for i := 0; i < 5; i++ {
		if w, _ := c.do("POST", "/api/auth/register", map[string]string{"email": "x@example.com", "password": "correct horse", "registration_code": "guess"}); w.Code != 403 {
			t.Fatalf("guess %d: %d", i, w.Code)
		}
	}
	// locked out: even the right code is refused until the backoff ends
	if w, _ := c.do("POST", "/api/auth/register", map[string]string{"email": "x@example.com", "password": "correct horse", "registration_code": "correct-horse-battery"}); w.Code != 429 {
		t.Fatalf("lockout: %d", w.Code)
	}
}

func TestConfigValidate(t *testing.T) {
	for _, c := range []Config{{RegistrationMode: "nope"}, {RegistrationMode: RegInvite}, {RegistrationMode: RegInvite, RegistrationCode: "short"}} {
		if c.Validate() == nil {
			t.Fatalf("expected error for %+v", c)
		}
	}
	c := Config{}
	if err := c.Validate(); err != nil || c.RegistrationMode != RegOpen {
		t.Fatalf("default: %v %q", err, c.RegistrationMode)
	}
}
