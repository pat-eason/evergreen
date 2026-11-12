package api

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/patrickeason/financial-wellness/backend/internal/ledger"
	"github.com/patrickeason/financial-wellness/backend/internal/sim"
	"github.com/patrickeason/financial-wellness/backend/internal/store"
)

func finite(vs ...float64) bool {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

func pathID(r *http.Request) (string, bool) {
	id := r.PathValue("id")
	return id, store.ValidID(id)
}

// ---------- profile ----------

// loadProfile returns the stored profile with current_age derived from the
// stored birth year, so age advances on its own.
func (s *Server) loadProfile(r *http.Request, uid string) (sim.Profile, bool, error) {
	p, exists, err := s.st.GetProfile(r.Context(), uid)
	if err != nil {
		return p, false, err
	}
	if p.BirthYear > 0 {
		p.CurrentAge = time.Now().Year() - p.BirthYear
		p.Normalize()
	}
	return p, exists, nil
}

func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	p, exists, err := s.loadProfile(r, userFrom(r.Context()).ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"profile": p, "configured": exists})
}

func (s *Server) putProfile(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	p := sim.DefaultProfile()
	if !readJSON(w, r, &p) {
		return
	}
	if !finite(p.Inflation, p.ReturnPre, p.ReturnPost, p.ReturnStdev, p.ContributionGrowth, p.RetirementSpending,
		p.AnnualIncome, p.AnnualExpenses, p.SafeWithdrawalRate, p.StateTaxRate, p.DividendYield) {
		writeErr(w, 400, "invalid number")
		return
	}
	if p.CurrentAge < 18 || p.CurrentAge > 100 {
		writeErr(w, 400, "age must be between 18 and 100")
		return
	}
	if p.RetirementAge < p.CurrentAge || p.RetirementAge > 100 {
		writeErr(w, 400, "retirement age must be between your current age and 100")
		return
	}
	if p.EndAge <= p.RetirementAge && p.EndAge <= p.CurrentAge {
		writeErr(w, 400, "plan end age must be after your current age")
		return
	}
	p.Normalize()
	p.BirthYear = time.Now().Year() - p.CurrentAge
	if err := s.st.SaveProfile(r.Context(), u.ID, p); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"profile": p, "configured": true})
}

// ---------- household members ----------

var validRelationships = map[string]bool{"spouse": true, "partner": true, "child": true, "parent": true, "other": true}

type memberDTO struct {
	ID           string `json:"id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Relationship string `json:"relationship"`
	Age          int    `json:"age"`
}

func toDTO(m store.Member) memberDTO {
	return memberDTO{ID: m.ID, FirstName: m.FirstName, LastName: m.LastName, Relationship: m.Relationship, Age: time.Now().Year() - m.BirthYear}
}

func sanitizeMember(d memberDTO) (store.Member, string) {
	m := store.Member{ID: d.ID, FirstName: strings.TrimSpace(d.FirstName), LastName: strings.TrimSpace(d.LastName), Relationship: d.Relationship}
	if m.FirstName == "" || len([]rune(m.FirstName)) > 40 || len([]rune(m.LastName)) > 40 {
		return m, "first name is required (max 40 characters)"
	}
	if m.Relationship == "" {
		m.Relationship = "other"
	}
	if !validRelationships[m.Relationship] {
		return m, "invalid relationship"
	}
	if d.Age < 0 || d.Age > 120 {
		return m, "age must be between 0 and 120"
	}
	m.BirthYear = time.Now().Year() - d.Age
	return m, ""
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	ms, err := s.st.ListMembers(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	out := make([]memberDTO, 0, len(ms))
	for _, m := range ms {
		out = append(out, toDTO(m))
	}
	writeJSON(w, 200, out)
}

func (s *Server) createMember(w http.ResponseWriter, r *http.Request) {
	var d memberDTO
	if !readJSON(w, r, &d) {
		return
	}
	m, msg := sanitizeMember(d)
	if msg != "" {
		writeErr(w, 400, msg)
		return
	}
	ms, err := s.st.ListMembers(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	if len(ms) >= 12 {
		writeErr(w, 400, "household is limited to 12 members")
		return
	}
	m, err = s.st.CreateMember(r.Context(), userFrom(r.Context()).ID, m)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 201, toDTO(m))
}

func (s *Server) updateMember(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	var d memberDTO
	if !readJSON(w, r, &d) {
		return
	}
	d.ID = id
	m, msg := sanitizeMember(d)
	if msg != "" {
		writeErr(w, 400, msg)
		return
	}
	if err := s.st.UpdateMember(r.Context(), userFrom(r.Context()).ID, m); err != nil {
		notFoundOr500(w, err)
		return
	}
	writeJSON(w, 200, toDTO(m))
}

func (s *Server) deleteMember(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := s.st.DeleteMember(r.Context(), userFrom(r.Context()).ID, id); err != nil {
		notFoundOr500(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveOwner validates an owner value: "self", "joint" (accounts only) or the
// id of one of the user's household members. Empty means "self".
func (s *Server) resolveOwner(r *http.Request, uid string, owner string, allowJoint bool) (string, string) {
	switch owner {
	case "", "self":
		return "self", ""
	case "joint":
		if allowJoint {
			return "joint", ""
		}
		return "", "events belong to a person, not the household"
	}
	if !store.ValidID(owner) {
		return "", "invalid owner"
	}
	ms, err := s.st.ListMembers(r.Context(), uid)
	if err != nil {
		return "", "could not verify owner"
	}
	for _, m := range ms {
		if m.ID == owner {
			return owner, ""
		}
	}
	return "", "owner is not in your household"
}

// ---------- accounts ----------

var validCategories = map[string]bool{
	sim.CatCash: true, sim.CatInvestment: true, sim.CatMortgage: true, sim.CatCreditCard: true, sim.CatDebt: true,
}

// sanitizeAccount validates an account and strips fields that don't belong to
// its category.
func sanitizeAccount(a sim.Account) (sim.Account, string) {
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" || len(a.Name) > 80 {
		return a, "name is required (max 80 characters)"
	}
	if !validCategories[a.Category] {
		return a, "invalid category"
	}
	c := a.Config
	if !finite(c.Balance, c.APY, c.MonthlyContribution, c.ExpectedReturn, c.EmployerMatch, c.CostBasis, c.ExpenseRatio,
		c.InterestRate, c.MonthlyPayment, c.RemainingYears, c.HomeValue, c.HomeAppreciation, c.PropertyTax, c.Insurance, c.APR, c.CreditLimit) {
		return a, "invalid number"
	}
	for _, v := range []float64{c.Balance, c.MonthlyContribution, c.EmployerMatch, c.CostBasis, c.MonthlyPayment, c.HomeValue, c.PropertyTax, c.Insurance, c.CreditLimit, c.RemainingYears} {
		if v < 0 || v > 1e11 {
			return a, "amounts must be between 0 and 100,000,000,000"
		}
	}
	for _, v := range []float64{c.APY, c.ExpectedReturn, c.ExpenseRatio, c.InterestRate, c.APR} {
		if v < 0 || v > 1 {
			return a, "rates must be between 0% and 100%"
		}
	}
	if c.HomeAppreciation < -0.5 || c.HomeAppreciation > 0.5 {
		return a, "appreciation must be between -50% and 50%"
	}
	out := sim.AccountConfig{Balance: c.Balance}
	switch a.Category {
	case sim.CatCash:
		out.APY, out.MonthlyContribution = c.APY, c.MonthlyContribution
	case sim.CatInvestment:
		switch c.TaxTreatment {
		case sim.TaxTaxable, sim.TaxTaxDeferred, sim.TaxTaxFree:
			out.TaxTreatment = c.TaxTreatment
		case "":
			out.TaxTreatment = sim.TaxTaxable
		default:
			return a, "invalid tax treatment"
		}
		out.MonthlyContribution, out.EmployerMatch = c.MonthlyContribution, c.EmployerMatch
		out.ExpectedReturn, out.ExpenseRatio = c.ExpectedReturn, c.ExpenseRatio
		if out.TaxTreatment == sim.TaxTaxable {
			out.CostBasis = c.CostBasis
		}
	case sim.CatMortgage:
		out.InterestRate, out.MonthlyPayment, out.RemainingYears = c.InterestRate, c.MonthlyPayment, c.RemainingYears
		out.HomeValue, out.HomeAppreciation, out.PropertyTax, out.Insurance = c.HomeValue, c.HomeAppreciation, c.PropertyTax, c.Insurance
	case sim.CatCreditCard:
		out.APR, out.MonthlyPayment, out.CreditLimit, out.PayInFull = c.APR, c.MonthlyPayment, c.CreditLimit, c.PayInFull
	case sim.CatDebt:
		out.InterestRate, out.MonthlyPayment, out.RemainingYears, out.DebtKind = c.InterestRate, c.MonthlyPayment, c.RemainingYears, strings.TrimSpace(c.DebtKind)
		if len(out.DebtKind) > 40 {
			out.DebtKind = out.DebtKind[:40]
		}
	}
	a.Config = out
	return a, ""
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	as, err := s.st.ListAccounts(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, as)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var a sim.Account
	if !readJSON(w, r, &a) {
		return
	}
	a, msg := sanitizeAccount(a)
	if msg == "" {
		a.Owner, msg = s.resolveOwner(r, userFrom(r.Context()).ID, a.Owner, true)
	}
	if msg != "" {
		writeErr(w, 400, msg)
		return
	}
	out, err := s.st.CreateAccount(r.Context(), userFrom(r.Context()).ID, a)
	if err != nil {
		serverErr(w, err)
		return
	}
	s.record(r, userFrom(r.Context()).ID, ledgerEntry(out, ledger.KindCreated, ledger.ReasonInitial, "", time.Now()))
	writeJSON(w, 201, out)
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	var a sim.Account
	if !readJSON(w, r, &a) {
		return
	}
	a.ID = id
	a, msg := sanitizeAccount(a)
	if msg == "" {
		a.Owner, msg = s.resolveOwner(r, userFrom(r.Context()).ID, a.Owner, true)
	}
	if msg != "" {
		writeErr(w, 400, msg)
		return
	}
	before, err := s.st.GetAccount(r.Context(), userFrom(r.Context()).ID, id)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	if err := s.st.UpdateAccount(r.Context(), userFrom(r.Context()).ID, a); err != nil {
		notFoundOr500(w, err)
		return
	}
	s.recordUpdate(r, userFrom(r.Context()).ID, before, a)
	writeJSON(w, 200, a)
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	uid := userFrom(r.Context()).ID
	before, err := s.st.GetAccount(r.Context(), uid, id)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	if err := s.st.DeleteAccount(r.Context(), uid, id); err != nil {
		notFoundOr500(w, err)
		return
	}
	s.record(r, uid, ledgerEntry(before, ledger.KindDeleted, ledger.ReasonRemoved, "", time.Now()))
	w.WriteHeader(http.StatusNoContent)
}

// ---------- events ----------

func sanitizeEvent(e sim.Event) (sim.Event, string) {
	e.Name = strings.TrimSpace(e.Name)
	e.Type = strings.TrimSpace(e.Type)
	if e.Name == "" || len(e.Name) > 80 {
		return e, "name is required (max 80 characters)"
	}
	if e.Kind != "income" && e.Kind != "spending" {
		return e, "kind must be income or spending"
	}
	if e.Type == "" {
		e.Type = "other"
	}
	if len(e.Type) > 40 {
		return e, "invalid type"
	}
	if e.StartAge < 0 || e.StartAge > 120 || e.Years < 0 || e.Years > 100 {
		return e, "invalid age or duration"
	}
	if !finite(e.AnnualAmount) || e.AnnualAmount < 0 || e.AnnualAmount > 1e10 {
		return e, "amount must be between 0 and 10,000,000,000"
	}
	if e.Kind == "spending" {
		e.Taxable = false
	}
	return e, ""
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	es, err := s.st.ListEvents(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, es)
}

func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) {
	e := sim.Event{InflationAdjusted: true}
	if !readJSON(w, r, &e) {
		return
	}
	e, msg := sanitizeEvent(e)
	if msg == "" {
		e.Owner, msg = s.resolveOwner(r, userFrom(r.Context()).ID, e.Owner, false)
	}
	if msg != "" {
		writeErr(w, 400, msg)
		return
	}
	out, err := s.st.CreateEvent(r.Context(), userFrom(r.Context()).ID, e)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 201, out)
}

func (s *Server) updateEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	var e sim.Event
	if !readJSON(w, r, &e) {
		return
	}
	e.ID = id
	e, msg := sanitizeEvent(e)
	if msg == "" {
		e.Owner, msg = s.resolveOwner(r, userFrom(r.Context()).ID, e.Owner, false)
	}
	if msg != "" {
		writeErr(w, 400, msg)
		return
	}
	if err := s.st.UpdateEvent(r.Context(), userFrom(r.Context()).ID, e); err != nil {
		notFoundOr500(w, err)
		return
	}
	writeJSON(w, 200, e)
}

func (s *Server) deleteEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := s.st.DeleteEvent(r.Context(), userFrom(r.Context()).ID, id); err != nil {
		notFoundOr500(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- budgets ----------

var monthRe = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// BudgetSummary mirrors the totals block in the household budget spreadsheet.
type BudgetSummary struct {
	Income                 float64 `json:"income"`
	Savings                float64 `json:"savings"`
	Expenses               float64 `json:"expenses"`
	Remainder              float64 `json:"remainder"`
	SavingsPct             float64 `json:"savings_pct"`
	ExpensesPct            float64 `json:"expenses_pct"`
	RemainderPct           float64 `json:"remainder_pct"`
	AnnualIncome           float64 `json:"annual_income"`
	AnnualSavings          float64 `json:"annual_savings"`
	AnnualExpenses         float64 `json:"annual_expenses"`
	AnnualRemainder        float64 `json:"annual_remainder"`
	AnnualGrowth           float64 `json:"annual_growth"`
	AnnualIncomeWithGrowth float64 `json:"annual_income_with_growth"`
}

type budgetResponse struct {
	store.Budget
	Summary BudgetSummary `json:"summary"`
}

func summarize(items []store.BudgetItem) BudgetSummary {
	var s BudgetSummary
	for _, it := range items {
		switch it.Section {
		case "income":
			s.Income += it.Amount
		case "savings":
			s.Savings += it.Amount
			s.AnnualGrowth += it.Amount * 12 * it.GrowthRate
		case "expense":
			s.Expenses += it.Amount
		}
	}
	s.Remainder = s.Income - s.Savings - s.Expenses
	if s.Income > 0 {
		s.SavingsPct, s.ExpensesPct, s.RemainderPct = s.Savings/s.Income, s.Expenses/s.Income, s.Remainder/s.Income
	}
	s.AnnualIncome, s.AnnualSavings, s.AnnualExpenses, s.AnnualRemainder = s.Income*12, s.Savings*12, s.Expenses*12, s.Remainder*12
	s.AnnualIncomeWithGrowth = s.AnnualIncome + s.AnnualGrowth
	return s
}

func respondBudget(w http.ResponseWriter, b store.Budget) {
	writeJSON(w, 200, budgetResponse{Budget: b, Summary: summarize(b.Items)})
}

func (s *Server) listBudgets(w http.ResponseWriter, r *http.Request) {
	ms, err := s.st.ListBudgetMonths(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, ms)
}

func (s *Server) getBudget(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if !monthRe.MatchString(month) {
		writeErr(w, 400, "month must be YYYY-MM")
		return
	}
	b, err := s.st.GetBudget(r.Context(), userFrom(r.Context()).ID, month)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	respondBudget(w, b)
}

type budgetBody struct {
	Notes string             `json:"notes"`
	Items []store.BudgetItem `json:"items"`
}

func sanitizeBudgetItems(in []store.BudgetItem) ([]store.BudgetItem, string) {
	if len(in) > 500 {
		return nil, "too many items (max 500)"
	}
	out := make([]store.BudgetItem, 0, len(in))
	for _, it := range in {
		it.Name = strings.TrimSpace(it.Name)
		it.Expr = strings.TrimSpace(it.Expr)
		if it.Section != "income" && it.Section != "savings" && it.Section != "expense" {
			return nil, "invalid section"
		}
		if it.Name == "" || len(it.Name) > 80 || len(it.Expr) > 200 {
			return nil, "each item needs a name (max 80 characters)"
		}
		if it.Expr != "" {
			v, err := EvalExpr(it.Expr)
			if err != nil {
				return nil, "invalid formula for " + it.Name + ": " + err.Error()
			}
			it.Amount = math.Round(v*100) / 100
		}
		if !finite(it.Amount, it.GrowthRate) || math.Abs(it.Amount) > 1e9 || it.GrowthRate < -1 || it.GrowthRate > 1 {
			return nil, "invalid amount or growth rate for " + it.Name
		}
		if it.Section != "savings" {
			it.GrowthRate, it.AccountID = 0, nil
		}
		out = append(out, it)
	}
	return out, ""
}

func (s *Server) putBudget(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if !monthRe.MatchString(month) {
		writeErr(w, 400, "month must be YYYY-MM")
		return
	}
	var body budgetBody
	if !readJSON(w, r, &body) {
		return
	}
	items, msg := sanitizeBudgetItems(body.Items)
	if msg != "" {
		writeErr(w, 400, msg)
		return
	}
	if len(body.Notes) > 2000 {
		writeErr(w, 400, "notes too long")
		return
	}
	u := userFrom(r.Context())
	if err := s.st.SaveBudget(r.Context(), u.ID, store.Budget{Month: month, Notes: body.Notes, Items: items}); err != nil {
		serverErr(w, err)
		return
	}
	b, err := s.st.GetBudget(r.Context(), u.ID, month)
	if err != nil {
		serverErr(w, err)
		return
	}
	respondBudget(w, b)
}

func (s *Server) deleteBudget(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if !monthRe.MatchString(month) {
		writeErr(w, 400, "month must be YYYY-MM")
		return
	}
	if err := s.st.DeleteBudget(r.Context(), userFrom(r.Context()).ID, month); err != nil {
		notFoundOr500(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// copyBudget creates month {month} by copying the items of another month.
func (s *Server) copyBudget(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	var body struct {
		From string `json:"from"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !monthRe.MatchString(month) || !monthRe.MatchString(body.From) {
		writeErr(w, 400, "month must be YYYY-MM")
		return
	}
	u := userFrom(r.Context())
	if _, err := s.st.GetBudget(r.Context(), u.ID, month); err == nil {
		writeErr(w, 409, "that month already has a budget")
		return
	}
	src, err := s.st.GetBudget(r.Context(), u.ID, body.From)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	src.Month = month
	for i := range src.Items {
		src.Items[i].ID = ""
	}
	if err := s.st.SaveBudget(r.Context(), u.ID, src); err != nil {
		serverErr(w, err)
		return
	}
	b, _ := s.st.GetBudget(r.Context(), u.ID, month)
	respondBudget(w, b)
}

// applyBudget pushes a month's budget into the plan: linked savings items
// update their account's monthly contribution, and the profile's annual
// income / expenses are set from the budget totals.
func (s *Server) applyBudget(w http.ResponseWriter, r *http.Request) {
	month := r.PathValue("month")
	if !monthRe.MatchString(month) {
		writeErr(w, 400, "month must be YYYY-MM")
		return
	}
	u := userFrom(r.Context())
	b, err := s.st.GetBudget(r.Context(), u.ID, month)
	if err != nil {
		notFoundOr500(w, err)
		return
	}
	sum := summarize(b.Items)
	perAccount := map[string]float64{}
	for _, it := range b.Items {
		if it.Section == "savings" && it.AccountID != nil {
			perAccount[*it.AccountID] += it.Amount
		}
	}
	updated := 0
	for id, amt := range perAccount {
		a, err := s.st.GetAccount(r.Context(), u.ID, id)
		if err != nil || (a.Category != sim.CatCash && a.Category != sim.CatInvestment) {
			continue
		}
		a.Config.MonthlyContribution = amt
		if err := s.st.UpdateAccount(r.Context(), u.ID, a); err == nil {
			updated++
		}
	}
	p, _, err := s.loadProfile(r, u.ID)
	if err != nil {
		serverErr(w, err)
		return
	}
	if sum.Income > 0 {
		p.AnnualIncome = sum.AnnualIncome
	}
	p.AnnualExpenses = sum.AnnualExpenses
	if err := s.st.SaveProfile(r.Context(), u.ID, p); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"accounts_updated": updated, "profile": p})
}

// ---------- plan & freedom ----------

func (s *Server) loadInputs(r *http.Request) (sim.Inputs, error) {
	uid := userFrom(r.Context()).ID
	p, _, err := s.loadProfile(r, uid)
	if err != nil {
		return sim.Inputs{}, err
	}
	as, err := s.st.ListAccounts(r.Context(), uid)
	if err != nil {
		return sim.Inputs{}, err
	}
	es, err := s.st.ListEvents(r.Context(), uid)
	if err != nil {
		return sim.Inputs{}, err
	}
	ms, err := s.st.ListMembers(r.Context(), uid)
	if err != nil {
		return sim.Inputs{}, err
	}
	// An event's start age is the owner's age. The engine runs on the primary
	// person's timeline, so shift member events by the age difference.
	year := time.Now().Year()
	offset := map[string]int{}
	for _, m := range ms {
		offset[m.ID] = p.CurrentAge - (year - m.BirthYear)
	}
	for i := range es {
		if d, ok := offset[es[i].Owner]; ok {
			es[i].StartAge += d
		}
	}
	return sim.Inputs{Profile: p, Accounts: as, Events: es, BaseYear: year}, nil
}

func (s *Server) simulate(w http.ResponseWriter, r *http.Request) {
	in, err := s.loadInputs(r)
	if err != nil {
		serverErr(w, err)
		return
	}
	var body struct {
		// Any profile field present here overrides the stored profile for this
		// run only ("what-if"), without persisting.
		Profile json.RawMessage `json:"profile"`
		Trials  int             `json:"trials"`
		// Fast skips the sustainable-spending search (used for comparisons).
		Fast bool `json:"fast"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.Profile) > 0 {
		if err := json.Unmarshal(body.Profile, &in.Profile); err != nil {
			writeErr(w, 400, "invalid profile overrides")
			return
		}
	}
	if body.Trials <= 0 || body.Trials > 5000 {
		body.Trials = 1000
	}
	res := sim.RunPlan(in, sim.Options{Trials: body.Trials, SkipAbility: body.Fast})
	writeJSON(w, 200, res)
}

func freedomDefaults(in sim.Inputs) sim.FreedomInputs {
	p := in.Profile
	f := sim.FreedomInputs{
		CurrentAge: p.CurrentAge, RetirementAge: p.RetirementAge,
		ContributionGrowth: p.ContributionGrowth, ReturnRate: p.ReturnPre, Inflation: p.Inflation,
		AnnualSpending: p.RetirementSpending, CurrentExpenses: p.AnnualExpenses, SafeWithdrawalRate: p.SafeWithdrawalRate,
	}
	for _, a := range in.Accounts {
		switch a.Category {
		case sim.CatCash, sim.CatInvestment:
			f.Investable += a.Config.Balance
			f.AnnualContribution += 12 * (a.Config.MonthlyContribution + a.Config.EmployerMatch)
		case sim.CatMortgage:
			// property tax & insurance continue after the loan is paid off
			f.AnnualSpending += a.Config.PropertyTax + a.Config.Insurance
		}
	}
	// Ongoing spending goals (health care etc.) that are active at retirement.
	for _, e := range in.Events {
		if e.Kind == "spending" && e.StartAge <= p.RetirementAge && (e.Years == 0 || e.StartAge+e.Years > p.RetirementAge) {
			f.AnnualSpending += e.AnnualAmount
		}
	}
	return f
}

func (s *Server) freedom(w http.ResponseWriter, r *http.Request) {
	in, err := s.loadInputs(r)
	if err != nil {
		serverErr(w, err)
		return
	}
	f := freedomDefaults(in)
	if !readJSON(w, r, &f) { // body fields override the stored-data defaults
		return
	}
	if !finite(f.Investable, f.AnnualContribution, f.ContributionGrowth, f.ReturnRate, f.Inflation, f.AnnualSpending, f.CurrentExpenses, f.SafeWithdrawalRate) ||
		f.CurrentAge < 0 || f.CurrentAge > 100 || f.RetirementAge > 110 || f.ReturnRate < -0.5 || f.ReturnRate > 0.5 ||
		f.Inflation < -0.05 || f.Inflation > 0.2 || f.SafeWithdrawalRate < 0 || f.SafeWithdrawalRate > 0.2 {
		writeErr(w, 400, "invalid inputs")
		return
	}
	writeJSON(w, 200, sim.AnalyzeFreedom(f))
}

// ---------- saved calculations ----------

func (s *Server) listCalcs(w http.ResponseWriter, r *http.Request) {
	cs, err := s.st.ListCalculations(r.Context(), userFrom(r.Context()).ID, r.URL.Query().Get("kind"))
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 200, cs)
}

func (s *Server) createCalc(w http.ResponseWriter, r *http.Request) {
	var c store.Calculation
	if !readJSON(w, r, &c) {
		return
	}
	c.Name = strings.TrimSpace(c.Name)
	if c.Kind != "freedom" && c.Kind != "plan" {
		writeErr(w, 400, "kind must be freedom or plan")
		return
	}
	if c.Name == "" || len(c.Name) > 80 {
		writeErr(w, 400, "name is required (max 80 characters)")
		return
	}
	if !json.Valid(c.Inputs) || !json.Valid(c.Result) {
		writeErr(w, 400, "inputs and result must be JSON")
		return
	}
	out, err := s.st.CreateCalculation(r.Context(), userFrom(r.Context()).ID, c)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, 201, out)
}

func (s *Server) deleteCalc(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := s.st.DeleteCalculation(r.Context(), userFrom(r.Context()).ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		serverErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
