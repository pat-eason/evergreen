// Package ledger models the append-only log of account changes and rebuilds
// net-worth history from it.
//
// Every entry records the balance *after* the event, so history can be
// reconstructed at any date without replaying deltas, and survives the account
// being deleted. Previous/Delta are derived on read.
package ledger

import (
	"sort"
	"time"
)

// Entry kinds.
const (
	KindCreated = "created"
	KindUpdated = "updated" // balance changed
	KindEdited  = "edited"  // name / owner / settings changed, balance did not
	KindDeleted = "deleted"
)

// Reasons for a balance change. Deposits and withdrawals are money moving in
// or out of an account; everything else counts as growth or loss.
const (
	ReasonInitial    = "initial"
	ReasonValue      = "value" // market / valuation change, interest, charges
	ReasonDeposit    = "deposit"
	ReasonWithdrawal = "withdrawal"
	ReasonPayment    = "payment"   // paying down a liability
	ReasonBorrowing  = "borrowing" // taking on more of a liability
	ReasonCorrection = "correction"
	ReasonRemoved    = "removed"
	ReasonEdit       = "edit"
)

// Entry is one ledger row.
type Entry struct {
	ID          int64     `json:"id"`
	AccountID   int64     `json:"account_id"`
	AccountName string    `json:"account_name"`
	Category    string    `json:"category"`
	Owner       string    `json:"owner"`
	Kind        string    `json:"kind"`
	Reason      string    `json:"reason"`
	Balance     float64   `json:"balance"`     // balance after this event (0 once deleted)
	AssetValue  float64   `json:"asset_value"` // home value for mortgages
	Note        string    `json:"note"`
	OccurredAt  time.Time `json:"occurred_at"`

	// Derived by Annotate.
	Previous float64 `json:"previous"`
	Delta    float64 `json:"delta"`
	First    bool    `json:"first"` // the account's earliest entry: its opening balance
}

// IsAsset reports whether a category adds to liquid net worth.
func IsAsset(category string) bool { return category == "cash" || category == "investment" }

// IsLiability reports whether a category is money owed.
func IsLiability(category string) bool {
	return category == "mortgage" || category == "credit_card" || category == "debt"
}

func sorted(es []Entry) []Entry {
	out := append([]Entry(nil), es...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].OccurredAt.Equal(out[j].OccurredAt) {
			return out[i].OccurredAt.Before(out[j].OccurredAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Annotate fills Previous and Delta by walking each account's entries in time
// order. The result is chronological.
func Annotate(es []Entry) []Entry {
	out := sorted(es)
	last := map[int64]float64{}
	for i := range out {
		prev, seen := last[out[i].AccountID]
		out[i].First = !seen
		out[i].Previous = prev
		out[i].Delta = out[i].Balance - prev
		last[out[i].AccountID] = out[i].Balance
	}
	return out
}

// flow is the amount of money an entry moved into (+) or out of (-) a liquid
// account, as opposed to growth. An account's opening balance (its first entry,
// whether it was added or backfilled) counts as funding; deleting one withdraws
// everything.
func flow(e Entry) float64 {
	if !IsAsset(e.Category) {
		return 0
	}
	switch {
	case e.First && e.Kind != KindDeleted:
		return e.Balance
	case e.Kind == KindDeleted:
		return -e.Previous
	case e.Kind == KindUpdated && (e.Reason == ReasonDeposit || e.Reason == ReasonWithdrawal):
		return e.Delta
	}
	return 0
}

// Point is the household position at the end of a day.
type Point struct {
	Date             string  `json:"date"` // YYYY-MM-DD
	NetWorth         float64 `json:"net_worth"`
	Assets           float64 `json:"assets"`      // cash + investments + home value
	Liabilities      float64 `json:"liabilities"` // mortgage + cards + debts
	Cash             float64 `json:"cash"`
	Investments      float64 `json:"investments"`
	Home             float64 `json:"home"`
	Mortgage         float64 `json:"mortgage"`
	CreditCards      float64 `json:"credit_cards"`
	Debts            float64 `json:"debts"`
	NetContributions float64 `json:"net_contributions"` // cumulative money moved into cash+investments
	Growth           float64 `json:"growth"`            // cash+investments minus net contributions
}

// AccountInfo labels one per-account series.
type AccountInfo struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Owner    string `json:"owner"`
	Alive    bool   `json:"alive"`
}

// Summary describes change across the returned range.
type Summary struct {
	From            string  `json:"from"`
	To              string  `json:"to"`
	StartNetWorth   float64 `json:"start_net_worth"`
	EndNetWorth     float64 `json:"end_net_worth"`
	Change          float64 `json:"change"`
	ChangePct       float64 `json:"change_pct"`
	Growth          float64 `json:"growth"`        // market gains/losses on cash + investments
	Contributions   float64 `json:"contributions"` // net money added to cash + investments
	LiabilityChange float64 `json:"liability_change"`
}

// History is the reconstructed timeline.
type History struct {
	Interval string              `json:"interval"`
	Points   []Point             `json:"points"`
	Summary  Summary             `json:"summary"`
	Accounts []AccountInfo       `json:"accounts,omitempty"`
	Series   map[int64][]float64 `json:"series,omitempty"` // account id -> balance per point
}

// Options control Build.
type Options struct {
	Now       time.Time
	From      time.Time // zero = first entry
	Interval  string    // auto | day | week | month
	AccountID int64     // 0 = whole household
	ByAccount bool
}

func endOfDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 23, 59, 59, 999_000_000, time.UTC)
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

type acct struct {
	info  AccountInfo
	bal   float64
	asset float64
}

const maxPoints = 400

// Build reconstructs history from ledger entries.
func Build(entries []Entry, opt Options) History {
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	es := Annotate(entries)
	if opt.AccountID != 0 {
		keep := es[:0:0]
		for _, e := range es {
			if e.AccountID == opt.AccountID {
				keep = append(keep, e)
			}
		}
		es = keep
	}
	h := History{Points: []Point{}}
	if len(es) == 0 {
		h.Interval = opt.Interval
		return h
	}

	end := startOfDay(opt.Now)
	if last := startOfDay(es[len(es)-1].OccurredAt); last.After(end) {
		end = last
	}
	start := startOfDay(es[0].OccurredAt)
	if !opt.From.IsZero() && startOfDay(opt.From).After(start) {
		start = startOfDay(opt.From)
	}
	if start.After(end) {
		start = end
	}

	// choose a step
	span := int(end.Sub(start).Hours()/24) + 1
	interval := opt.Interval
	if interval != "day" && interval != "week" && interval != "month" {
		switch {
		case span <= 62:
			interval = "day"
		case span <= 400:
			interval = "week"
		default:
			interval = "month"
		}
	}
	h.Interval = interval

	var dates []time.Time
	for i := 0; ; i++ {
		var d time.Time
		switch interval {
		case "day":
			d = start.AddDate(0, 0, i)
		case "week":
			d = start.AddDate(0, 0, 7*i)
		default:
			d = start.AddDate(0, i, 0)
		}
		if !d.Before(end) || len(dates) >= maxPoints-1 {
			break
		}
		dates = append(dates, d)
	}
	dates = append(dates, end) // always finish on the latest day

	// replay
	state := map[int64]*acct{}
	order := []int64{}
	var contributions float64
	idx := 0
	apply := func(upto time.Time) {
		for idx < len(es) && !es[idx].OccurredAt.After(upto) {
			e := es[idx]
			a := state[e.AccountID]
			if a == nil {
				a = &acct{}
				state[e.AccountID] = a
				order = append(order, e.AccountID)
			}
			a.info = AccountInfo{ID: e.AccountID, Name: e.AccountName, Category: e.Category, Owner: e.Owner, Alive: e.Kind != KindDeleted}
			a.bal, a.asset = e.Balance, e.AssetValue
			if e.Kind == KindDeleted {
				a.bal, a.asset = 0, 0
			}
			contributions += flow(e)
			idx++
		}
	}

	if opt.ByAccount {
		h.Series = map[int64][]float64{}
	}
	for pi, d := range dates {
		apply(endOfDay(d))
		var p Point
		p.Date = d.Format("2006-01-02")
		for _, a := range state {
			switch a.info.Category {
			case "cash":
				p.Cash += a.bal
			case "investment":
				p.Investments += a.bal
			case "mortgage":
				p.Mortgage += a.bal
				p.Home += a.asset
			case "credit_card":
				p.CreditCards += a.bal
			case "debt":
				p.Debts += a.bal
			}
		}
		p.Assets = p.Cash + p.Investments + p.Home
		p.Liabilities = p.Mortgage + p.CreditCards + p.Debts
		p.NetWorth = p.Assets - p.Liabilities
		p.NetContributions = contributions
		p.Growth = p.Cash + p.Investments - contributions
		h.Points = append(h.Points, p)

		if opt.ByAccount {
			for _, id := range order {
				s := h.Series[id]
				for len(s) < pi { // account didn't exist yet at earlier points
					s = append(s, 0)
				}
				h.Series[id] = append(s, state[id].bal)
			}
		}
	}
	if opt.ByAccount {
		for _, id := range order {
			s := h.Series[id]
			for len(s) < len(h.Points) {
				s = append(s, s[len(s)-1])
			}
			h.Series[id] = s
			h.Accounts = append(h.Accounts, state[id].info)
		}
	}

	first, last := h.Points[0], h.Points[len(h.Points)-1]
	h.Summary = Summary{
		From: first.Date, To: last.Date,
		StartNetWorth: first.NetWorth, EndNetWorth: last.NetWorth,
		Change:          last.NetWorth - first.NetWorth,
		Contributions:   last.NetContributions - first.NetContributions,
		Growth:          last.Growth - first.Growth,
		LiabilityChange: last.Liabilities - first.Liabilities,
	}
	if first.NetWorth != 0 {
		base := first.NetWorth
		if base < 0 {
			base = -base
		}
		h.Summary.ChangePct = h.Summary.Change / base
	}
	return h
}
