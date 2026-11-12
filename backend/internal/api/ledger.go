package api

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/patrickeason/financial-wellness/backend/internal/ledger"
	"github.com/patrickeason/financial-wellness/backend/internal/sim"
	"github.com/patrickeason/financial-wellness/backend/internal/store"
)

// ledgerEntry builds a ledger row describing an account at a moment in time.
func ledgerEntry(a sim.Account, kind, reason, note string, at time.Time) ledger.Entry {
	e := ledger.Entry{
		AccountID: a.ID, AccountName: a.Name, Category: a.Category, Owner: a.Owner,
		Kind: kind, Reason: reason, Note: note, OccurredAt: at, Balance: a.Config.Balance,
	}
	if a.Category == sim.CatMortgage {
		e.AssetValue = a.Config.HomeValue
	}
	if kind == ledger.KindDeleted {
		e.Balance, e.AssetValue = 0, 0
	}
	return e
}

// record appends to the ledger. A failure is logged rather than failing the
// user's change: the account write has already happened.
func (s *Server) record(r *http.Request, uid string, e ledger.Entry) {
	if _, err := s.st.AddLedger(r.Context(), uid, e); err != nil {
		log.Printf("ledger write failed (account %s, %s): %v", e.AccountID, e.Kind, err)
	}
}

// recordUpdate logs the right kind of entry for an account edit: a balance
// change, a settings-only edit, or nothing when nothing changed.
func (s *Server) recordUpdate(r *http.Request, uid string, before, after sim.Account) {
	switch {
	case before.Config.Balance != after.Config.Balance ||
		(after.Category == sim.CatMortgage && before.Config.HomeValue != after.Config.HomeValue):
		s.record(r, uid, ledgerEntry(after, ledger.KindUpdated, ledger.ReasonValue, "", time.Now()))
	default:
		b, _ := json.Marshal(before.Config)
		a, _ := json.Marshal(after.Config)
		if before.Name != after.Name || before.Owner != after.Owner || before.Category != after.Category || string(b) != string(a) {
			s.record(r, uid, ledgerEntry(after, ledger.KindEdited, ledger.ReasonEdit, "", time.Now()))
		}
	}
}

var assetReasons = map[string]bool{ledger.ReasonValue: true, ledger.ReasonDeposit: true, ledger.ReasonWithdrawal: true, ledger.ReasonCorrection: true}
var liabilityReasons = map[string]bool{ledger.ReasonValue: true, ledger.ReasonPayment: true, ledger.ReasonBorrowing: true, ledger.ReasonCorrection: true}

// recordValue sets an account's balance as of a date: POST /api/accounts/{id}/value
//
// If the date is on or after the account's latest entry the live balance is
// updated too; an earlier date is a backfill that only adds history.
func (s *Server) recordValue(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	var body struct {
		Balance   float64  `json:"balance"`
		HomeValue *float64 `json:"home_value"`
		Reason    string   `json:"reason"`
		AsOf      string   `json:"as_of"` // YYYY-MM-DD, optional
		Note      string   `json:"note"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	u := userFrom(r.Context())
	a, err := s.st.GetAccount(r.Context(), u.ID, id)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	if !finite(body.Balance) || body.Balance < 0 || body.Balance > 1e11 {
		writeErr(w, 400, "balance must be between 0 and 100,000,000,000")
		return
	}
	if body.HomeValue != nil && (!finite(*body.HomeValue) || *body.HomeValue < 0 || *body.HomeValue > 1e11) {
		writeErr(w, 400, "home value must be between 0 and 100,000,000,000")
		return
	}
	reasons := assetReasons
	if !sim.Account(a).IsAsset() {
		reasons = liabilityReasons
	}
	if body.Reason == "" {
		body.Reason = ledger.ReasonValue
	}
	if !reasons[body.Reason] {
		writeErr(w, 400, "invalid reason for this account type")
		return
	}
	body.Note = strings.TrimSpace(body.Note)
	if len([]rune(body.Note)) > 200 {
		writeErr(w, 400, "note is too long (max 200 characters)")
		return
	}

	at := time.Now()
	if body.AsOf != "" {
		d, err := time.Parse("2006-01-02", body.AsOf)
		if err != nil {
			writeErr(w, 400, "date must be YYYY-MM-DD")
			return
		}
		today := time.Now().UTC().Truncate(24 * time.Hour)
		if d.After(today) {
			writeErr(w, 400, "date can't be in the future")
			return
		}
		if d.Year() < 1990 {
			writeErr(w, 400, "date is too far in the past")
			return
		}
		if d.Equal(today) {
			at = time.Now()
		} else {
			at = d.Add(12 * time.Hour)
		}
	}

	latest, has, err := s.st.LatestLedgerTime(r.Context(), u.ID, id)
	if err != nil {
		serverErr(w, err)
		return
	}
	applied := !has || !at.Before(latest.Truncate(24*time.Hour))
	next := a
	next.Config.Balance = body.Balance
	if body.HomeValue != nil && a.Category == sim.CatMortgage {
		next.Config.HomeValue = *body.HomeValue
	}
	if applied {
		if err := s.st.UpdateAccount(r.Context(), u.ID, next); err != nil {
			notFoundOr500(w, err)
			return
		}
		a = next
	}
	// history reflects the recorded value even for a backfill
	s.record(r, u.ID, ledgerEntry(next, ledger.KindUpdated, body.Reason, body.Note, at))
	writeJSON(w, 200, map[string]any{"account": a, "applied": applied})
}

// dateRange is an optional inclusive UTC day range from ?from=YYYY-MM-DD&to=YYYY-MM-DD.
type dateRange struct{ from, to time.Time }

func parseDateRange(q url.Values) (dateRange, string) {
	var dr dateRange
	parse := func(name string) (time.Time, string) {
		v := q.Get(name)
		if v == "" {
			return time.Time{}, ""
		}
		d, err := time.Parse("2006-01-02", v)
		if err != nil || d.Year() < 1990 || d.Year() > 2200 {
			return time.Time{}, name + " must be a date like 2026-03-31"
		}
		return d, ""
	}
	var msg string
	if dr.from, msg = parse("from"); msg != "" {
		return dr, msg
	}
	if dr.to, msg = parse("to"); msg != "" {
		return dr, msg
	}
	if !dr.from.IsZero() && !dr.to.IsZero() && dr.to.Before(dr.from) {
		return dr, "the end date can't be before the start date"
	}
	return dr, ""
}

func (s *Server) listLedger(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	q := r.URL.Query()
	dr, bad := parseDateRange(q)
	if bad != "" {
		writeErr(w, 400, bad)
		return
	}
	accountID := q.Get("account_id")
	if accountID != "" && !store.ValidID(accountID) {
		writeErr(w, http.StatusBadRequest, "invalid account_id")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	all, err := s.st.ListLedger(r.Context(), u.ID, "")
	if err != nil {
		serverErr(w, err)
		return
	}
	// annotate across the whole log so Previous/Delta are right, then filter
	annotated := ledger.Annotate(all)
	filtered := make([]ledger.Entry, 0, len(annotated))
	for i := len(annotated) - 1; i >= 0; i-- { // newest first
		e := annotated[i]
		if accountID != "" && e.AccountID != accountID {
			continue
		}
		if !dr.from.IsZero() && e.OccurredAt.Before(dr.from) {
			continue
		}
		if !dr.to.IsZero() && !e.OccurredAt.Before(dr.to.AddDate(0, 0, 1)) { // "to" is inclusive
			continue
		}
		filtered = append(filtered, e)
	}
	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	writeJSON(w, 200, map[string]any{"entries": filtered[offset:end], "total": total})
}

func (s *Server) ledgerHistory(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	q := r.URL.Query()
	dr, bad := parseDateRange(q)
	if bad != "" {
		writeErr(w, 400, bad)
		return
	}
	accountID := q.Get("account_id")
	if accountID != "" && !store.ValidID(accountID) {
		writeErr(w, http.StatusBadRequest, "invalid account_id")
		return
	}
	months, _ := strconv.Atoi(q.Get("months"))
	opt := ledger.Options{Now: time.Now(), Interval: q.Get("interval"), AccountID: accountID, ByAccount: q.Get("by_account") == "1", From: dr.from, To: dr.to}
	if months > 0 && months <= 600 && dr.from.IsZero() {
		opt.From = opt.Now.AddDate(0, -months, 0)
	}
	all, err := s.st.ListLedger(r.Context(), u.ID, "")
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, ledger.Build(all, opt))
}
