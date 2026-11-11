package sim

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
)

// Summary is the headline result of a plan run.
type Summary struct {
	SuccessRate          float64 `json:"success_rate"`
	Verdict              string  `json:"verdict"`
	PlannedMonthly       float64 `json:"planned_monthly"`   // retirement spending / 12, today's dollars
	ProjectedMonthly     float64 `json:"projected_monthly"` // sustainable at ~90% confidence
	PortfolioToday       float64 `json:"portfolio_today"`
	PortfolioAtRetire    float64 `json:"portfolio_at_retirement"`
	PortfolioAtRetireP10 float64 `json:"portfolio_at_retirement_p10"`
	PortfolioAtEnd       float64 `json:"portfolio_at_end"`
	DepletedAge          int     `json:"depleted_age"` // median-market path; 0 = never
	AnnualContribution   float64 `json:"annual_contribution"`
	Trials               int     `json:"trials"`
	LifetimeTaxes        float64 `json:"lifetime_taxes"`
}

// Result is the full output of RunPlan.
type Result struct {
	Summary Summary   `json:"summary"`
	Years   []YearRow `json:"years"`
	Profile Profile   `json:"profile"`
}

// Options control a plan run.
type Options struct {
	Trials int
	Seed   uint64
	// SkipAbility disables the (more expensive) spending-ability search.
	SkipAbility bool
}

// percentile of an already-sorted slice (linear interpolation).
func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	f := pos - float64(lo)
	return sorted[lo]*(1-f) + sorted[hi]*f
}

// monteCarlo runs trials and returns the success rate plus the percentile
// bands of the starting portfolio for each year (when bands is true).
func (pl *Plan) monteCarlo(trials int, seed uint64, scale float64, bands bool) (success float64, p10, p50, p90 []float64) {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	z := make([]float64, pl.N)
	starts := make([]float64, pl.N)
	var cols [][]float64
	if bands {
		cols = make([][]float64, pl.N)
		for i := range cols {
			cols[i] = make([]float64, 0, trials)
		}
	}
	ok := 0
	for k := 0; k < trials; k++ {
		for i := range z {
			z[i] = rng.NormFloat64()
		}
		var s []float64
		if bands {
			s = starts
		}
		_, good, _ := pl.Simulate(z, scale, false, s)
		if good {
			ok++
		}
		if bands {
			for i := range starts {
				cols[i] = append(cols[i], starts[i])
			}
		}
	}
	success = float64(ok) / float64(trials)
	if bands {
		p10 = make([]float64, pl.N)
		p50 = make([]float64, pl.N)
		p90 = make([]float64, pl.N)
		for i := range cols {
			sort.Float64s(cols[i])
			p10[i] = percentile(cols[i], 0.10)
			p50[i] = percentile(cols[i], 0.50)
			p90[i] = percentile(cols[i], 0.90)
		}
	}
	return
}

// SpendingAbility finds the largest retirement-spending multiplier that still
// meets the target success rate, and returns it (capped at 8x).
func (pl *Plan) SpendingAbility(target float64, trials int, seed uint64) float64 {
	const hiCap = 8.0
	if s, _, _, _ := pl.monteCarlo(trials, seed, hiCap, false); s >= target {
		return hiCap
	}
	lo, hi := 0.0, hiCap
	for i := 0; i < 14; i++ {
		mid := (lo + hi) / 2
		if s, _, _, _ := pl.monteCarlo(trials, seed, mid, false); s >= target {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

func verdict(rate float64) string {
	switch {
	case rate >= 0.90:
		return "very good shape"
	case rate >= 0.75:
		return "good shape"
	case rate >= 0.50:
		return "some work to do"
	default:
		return "needs attention"
	}
}

// RunPlan executes the full plan: a median-market detailed path, Monte Carlo
// bands, success probability and sustainable spending.
func RunPlan(in Inputs, opt Options) Result {
	if opt.Trials <= 0 {
		opt.Trials = 1000
	}
	if opt.Seed == 0 {
		opt.Seed = 42
	}
	pl := Prepare(in)
	rows, _, depleted := pl.Simulate(nil, 1, true, nil)

	success, p10, p50, p90 := pl.monteCarlo(opt.Trials, opt.Seed, 1, true)
	for i := range rows {
		rows[i].PortfolioP10 = p10[i]
		rows[i].PortfolioP50 = p50[i]
		rows[i].PortfolioP90 = p90[i]
	}
	annotate(pl, rows)

	sum := Summary{
		SuccessRate:    success,
		Verdict:        verdict(success),
		PlannedMonthly: pl.P.RetirementSpending / 12,
		DepletedAge:    depleted,
		Trials:         opt.Trials,
	}
	for b := 0; b < nBuckets; b++ {
		sum.PortfolioToday += pl.start[b]
		sum.AnnualContribution += pl.contrib[b]
	}
	if len(rows) > 0 {
		sum.PortfolioAtEnd = rows[len(rows)-1].End.Total
	}
	for _, r := range rows {
		sum.LifetimeTaxes += r.Taxes.Total
		if r.Age == pl.P.RetirementAge {
			sum.PortfolioAtRetire = r.Start.Total
			sum.PortfolioAtRetireP10 = r.PortfolioP10
		}
	}
	if !opt.SkipAbility {
		scale := pl.SpendingAbility(0.90, 300, opt.Seed)
		sum.ProjectedMonthly = scale * pl.P.RetirementSpending / 12
	}
	return Result{Summary: sum, Years: rows, Profile: pl.P}
}

// annotate adds human-readable insights to the median path rows.
func annotate(pl *Plan, rows []YearRow) {
	if len(rows) == 0 {
		return
	}
	// Highest marginal ordinary rate that occurs later in retirement.
	laterMax := make([]float64, len(rows)+1)
	for i := len(rows) - 1; i >= 0; i-- {
		laterMax[i] = laterMax[i+1]
		if rows[i].Retired && rows[i].Taxes.Marginal > laterMax[i] {
			laterMax[i] = rows[i].Taxes.Marginal
		}
	}
	retiredSeen := 0
	for i := range rows {
		r := &rows[i]
		if !r.Retired {
			continue
		}
		retiredSeen++
		if r.Shortfall > 1 {
			r.Insights = append(r.Insights, fmt.Sprintf("Your portfolio can't fully cover spending at age %d on a median-market path.", r.Age))
		}
		if retiredSeen <= 12 && r.Taxes.Marginal < laterMax[i+1]-1e-9 && r.Taxes.Marginal <= 0.22 && r.Withdrawals.RothConverted == 0 {
			r.Insights = append(r.Insights, "Your tax bracket is likely to be higher in the future. Strategically realized taxable gains or a partial Roth conversion may help.")
		}
		if r.Withdrawals.RothConverted > 0 {
			r.Insights = append(r.Insights, fmt.Sprintf("Converting about $%s to Roth fills your target bracket at today's lower rate.", commas(r.Withdrawals.RothConverted)))
		}
		if r.Age == pl.rmdAge && r.Withdrawals.RMD > 0 {
			r.Insights = append(r.Insights, "Required minimum distributions begin this year.")
		}
		if r.Age == pl.P.RetirementAge {
			r.Insights = append(r.Insights, "First year of retirement: spending is now funded from your portfolio and income events.")
		}
	}
	for i := range rows {
		r := &rows[i]
		for _, ev := range pl.events {
			if ev.Kind == "income" && ev.StartAge == r.Age {
				r.Insights = append(r.Insights, fmt.Sprintf("%s begins.", ev.Name))
			}
		}
		for _, l := range pl.liabs {
			t := i
			if t < len(l.sched) && l.sched[t].EndBalance == 0 && l.sched[t].Payment > 0 {
				r.Insights = append(r.Insights, fmt.Sprintf("%s is paid off this year.", l.name))
			}
		}
	}
}

func commas(v float64) string {
	n := int64(math.Round(v))
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	out := make([]byte, 0, len(s)+len(s)/3)
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
