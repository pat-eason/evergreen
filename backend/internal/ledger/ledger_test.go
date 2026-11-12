package ledger

import (
	"math"
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t.Add(12 * time.Hour)
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func sample() []Entry {
	return []Entry{
		{ID: "1", AccountID: "1", AccountName: "401k", Category: "investment", Kind: KindCreated, Reason: ReasonInitial, Balance: 10000, OccurredAt: day("2026-01-01")},
		{ID: "2", AccountID: "2", AccountName: "Mortgage", Category: "mortgage", Kind: KindCreated, Reason: ReasonInitial, Balance: 200000, AssetValue: 300000, OccurredAt: day("2026-01-01")},
		{ID: "3", AccountID: "1", AccountName: "401k", Category: "investment", Kind: KindUpdated, Reason: ReasonDeposit, Balance: 12000, OccurredAt: day("2026-02-01")}, // +2000 contribution
		{ID: "4", AccountID: "1", AccountName: "401k", Category: "investment", Kind: KindUpdated, Reason: ReasonValue, Balance: 13500, OccurredAt: day("2026-03-01")},   // +1500 growth
		{ID: "5", AccountID: "1", AccountName: "401k", Category: "investment", Kind: KindUpdated, Reason: ReasonValue, Balance: 12900, OccurredAt: day("2026-04-01")},   // -600 loss
		{ID: "6", AccountID: "2", AccountName: "Mortgage", Category: "mortgage", Kind: KindUpdated, Reason: ReasonPayment, Balance: 198000, AssetValue: 300000, OccurredAt: day("2026-04-01")},
	}
}

func TestAnnotate(t *testing.T) {
	es := Annotate(sample())
	var m4 Entry
	for _, e := range es {
		if e.ID == "5" {
			m4 = e
		}
	}
	if m4.Previous != 13500 || !near(m4.Delta, -600) {
		t.Fatalf("annotate: %+v", m4)
	}
	// input order must not matter
	rev := sample()
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	if Annotate(rev)[5].ID != "6" && Annotate(rev)[4].ID != "6" {
		t.Fatal("annotate should sort chronologically")
	}
}

func TestGrowthVersusContributions(t *testing.T) {
	h := Build(sample(), Options{Now: day("2026-04-02"), Interval: "month"})
	last := h.Points[len(h.Points)-1]
	// 401k: 10k initial + 2k deposit = 12k contributed; balance 12.9k -> growth 900
	if !near(last.NetContributions, 12000) || !near(last.Growth, 900) {
		t.Fatalf("contributions=%v growth=%v", last.NetContributions, last.Growth)
	}
	// net worth = 12.9k + 300k home - 198k mortgage
	if !near(last.NetWorth, 12900+300000-198000) {
		t.Fatalf("net worth = %v", last.NetWorth)
	}
	first := h.Points[0]
	if first.Date != "2026-01-01" || !near(first.NetWorth, 10000+300000-200000) {
		t.Fatalf("first point: %+v", first)
	}
	if !near(h.Summary.Growth, 900) || !near(h.Summary.Contributions, 2000) {
		t.Fatalf("summary: %+v", h.Summary)
	}
	if !near(h.Summary.LiabilityChange, -2000) {
		t.Fatalf("liability change: %+v", h.Summary)
	}
}

func TestDeletionIsAWithdrawalNotALoss(t *testing.T) {
	es := append(sample(), Entry{ID: "7", AccountID: "1", AccountName: "401k", Category: "investment", Kind: KindDeleted, Reason: ReasonRemoved, Balance: 0, OccurredAt: day("2026-05-01")})
	h := Build(es, Options{Now: day("2026-05-02"), Interval: "month"})
	last := h.Points[len(h.Points)-1]
	if last.Investments != 0 {
		t.Fatalf("deleted account should not count: %+v", last)
	}
	// 12.9k leaves as a withdrawal (contributions 12k - 12.9k = -900), so the
	// 900 of growth already earned is preserved rather than erased.
	if !near(last.Growth, 900) {
		t.Fatalf("deleting must not erase earned growth: %+v", last)
	}
}

func TestBackfilledEntryChangesHistoryAtItsDate(t *testing.T) {
	es := append(sample(), Entry{ID: "8", AccountID: "1", AccountName: "401k", Category: "investment", Kind: KindUpdated, Reason: ReasonValue, Balance: 11000, OccurredAt: day("2026-01-15")})
	h := Build(es, Options{Now: day("2026-02-10"), Interval: "day"})
	var jan20, jan10 Point
	for _, p := range h.Points {
		switch p.Date {
		case "2026-01-20":
			jan20 = p
		case "2026-01-10":
			jan10 = p
		}
	}
	if jan10.Investments != 10000 || jan20.Investments != 11000 {
		t.Fatalf("backfill: jan10=%v jan20=%v", jan10.Investments, jan20.Investments)
	}
}

func TestPerAccountSeriesAndFilter(t *testing.T) {
	h := Build(sample(), Options{Now: day("2026-04-02"), Interval: "month", ByAccount: true})
	n := len(h.Points)
	if len(h.Series["1"]) != n || len(h.Series["2"]) != n || len(h.Accounts) != 2 {
		t.Fatalf("series lengths: %d/%d points=%d accounts=%d", len(h.Series["1"]), len(h.Series["2"]), n, len(h.Accounts))
	}
	if h.Series["1"][n-1] != 12900 {
		t.Fatalf("last 401k balance = %v", h.Series["1"][n-1])
	}
	only := Build(sample(), Options{Now: day("2026-04-02"), Interval: "month", AccountID: "2"})
	if only.Points[len(only.Points)-1].Investments != 0 {
		t.Fatal("filter should exclude other accounts")
	}
}

func TestLateStartingAccountPadsZeros(t *testing.T) {
	es := []Entry{
		{ID: "1", AccountID: "1", Category: "cash", Kind: KindCreated, Balance: 100, OccurredAt: day("2026-01-01")},
		{ID: "2", AccountID: "2", Category: "cash", Kind: KindCreated, Balance: 50, OccurredAt: day("2026-03-01")},
	}
	h := Build(es, Options{Now: day("2026-04-01"), Interval: "month", ByAccount: true})
	s := h.Series["2"]
	if len(s) != len(h.Points) || s[0] != 0 || s[len(s)-1] != 50 {
		t.Fatalf("series = %v", s)
	}
}

func TestIntervalSelectionAndPointCap(t *testing.T) {
	es := []Entry{{ID: "1", AccountID: "1", Category: "cash", Kind: KindCreated, Balance: 1, OccurredAt: day("2020-01-01")}}
	h := Build(es, Options{Now: day("2026-01-01"), Interval: "day"})
	if len(h.Points) > maxPoints {
		t.Fatalf("points = %d", len(h.Points))
	}
	if h.Points[len(h.Points)-1].Date != "2026-01-01" {
		t.Fatalf("must end on the latest day: %s", h.Points[len(h.Points)-1].Date)
	}
	auto := Build(es, Options{Now: day("2026-01-01")})
	if auto.Interval != "month" {
		t.Fatalf("auto interval for 6y = %s", auto.Interval)
	}
	if got := Build(nil, Options{}); len(got.Points) != 0 {
		t.Fatal("empty ledger should produce no points")
	}
}

func TestRangeStartAndSummaryOverRange(t *testing.T) {
	h := Build(sample(), Options{Now: day("2026-04-02"), From: day("2026-03-01"), Interval: "day"})
	if h.Points[0].Date != "2026-03-01" {
		t.Fatalf("range start: %s", h.Points[0].Date)
	}
	// Mar 1 -> Apr 2: 13.5k -> 12.9k = -600 growth, no contributions
	if !near(h.Summary.Growth, -600) || !near(h.Summary.Contributions, 0) {
		t.Fatalf("range summary: %+v", h.Summary)
	}
}

// Older values backfilled before an account's "created" entry make that
// backfilled value the opening balance; the later created entry is just a
// snapshot and must not be counted as a second funding.
func TestBackfillBeforeCreationDoesNotDoubleCountFunding(t *testing.T) {
	es := []Entry{
		{ID: "1", AccountID: "1", AccountName: "IRA", Category: "investment", Kind: KindCreated, Reason: ReasonInitial, Balance: 12000, OccurredAt: day("2026-06-01")},
		{ID: "2", AccountID: "1", AccountName: "IRA", Category: "investment", Kind: KindUpdated, Reason: ReasonValue, Balance: 10000, OccurredAt: day("2026-01-01")}, // backfilled opening
		{ID: "3", AccountID: "1", AccountName: "IRA", Category: "investment", Kind: KindUpdated, Reason: ReasonDeposit, Balance: 11000, OccurredAt: day("2026-03-01")},
	}
	ann := Annotate(es)
	if !ann[0].First || ann[0].ID != "2" || ann[1].First || ann[2].First {
		t.Fatalf("first flags: %+v", ann)
	}
	h := Build(es, Options{Now: day("2026-06-02"), Interval: "month"})
	last := h.Points[len(h.Points)-1]
	// funded 10k, deposited 1k; ends at 12k => 1k of growth
	if !near(last.NetContributions, 11000) || !near(last.Growth, 1000) {
		t.Fatalf("contributions=%v growth=%v", last.NetContributions, last.Growth)
	}
	if h.Points[0].Date != "2026-01-01" || !near(h.Points[0].Investments, 10000) {
		t.Fatalf("history should start at the backfilled opening balance: %+v", h.Points[0])
	}
}

func TestExplicitRange(t *testing.T) {
	// sample(): Jan 1 open (10k), Feb 1 deposit (12k), Mar 1 +1.5k value (13.5k), Apr 1 -600 value (12.9k)
	h := Build(sample(), Options{Now: day("2026-06-01"), From: day("2026-02-01"), To: day("2026-03-15"), Interval: "day"})
	if h.Points[0].Date != "2026-02-01" || h.Points[len(h.Points)-1].Date != "2026-03-15" {
		t.Fatalf("range bounds: %s .. %s", h.Points[0].Date, h.Points[len(h.Points)-1].Date)
	}
	last := h.Points[len(h.Points)-1]
	if last.Investments != 13500 {
		t.Fatalf("entries after the end date must be ignored: %v", last.Investments)
	}
	// Feb 1 (12k, after the 2k deposit) -> Mar 15 (13.5k): +1.5k growth, no new contributions
	if !near(h.Summary.Growth, 1500) || !near(h.Summary.Contributions, 0) {
		t.Fatalf("summary over a custom range: %+v", h.Summary)
	}

	// a range entirely before the first entry is empty, not a row of zeros
	early := Build(sample(), Options{Now: day("2026-06-01"), From: day("2025-01-01"), To: day("2025-12-31")})
	if len(early.Points) != 0 {
		t.Fatalf("expected no points, got %d", len(early.Points))
	}

	// a range that starts before the ledger does is clipped to the first entry
	clipped := Build(sample(), Options{Now: day("2026-06-01"), From: day("2025-01-01"), To: day("2026-01-10"), Interval: "day"})
	if clipped.Points[0].Date != "2026-01-01" {
		t.Fatalf("clipped start: %s", clipped.Points[0].Date)
	}
}
