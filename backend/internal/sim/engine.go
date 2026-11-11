package sim

import (
	"math"
)

// Inputs is everything needed to run a plan.
type Inputs struct {
	Profile  Profile   `json:"profile"`
	Accounts []Account `json:"accounts"`
	Events   []Event   `json:"events"`
	BaseYear int       `json:"base_year"`
}

// Buckets are portfolio balances grouped by tax treatment.
type Buckets struct {
	Cash        float64 `json:"cash"`
	Taxable     float64 `json:"taxable"`
	TaxDeferred float64 `json:"tax_deferred"`
	TaxFree     float64 `json:"tax_free"`
	Total       float64 `json:"total"`
}

// Item is a named amount (an income event or spending line).
type Item struct {
	Name   string  `json:"name"`
	Type   string  `json:"type,omitempty"`
	Amount float64 `json:"amount"`
}

// Withdrawals records where retirement cash came from in a year.
type Withdrawals struct {
	Cash          float64 `json:"cash"`
	Taxable       float64 `json:"taxable"`
	TaxDeferred   float64 `json:"tax_deferred"`
	TaxFree       float64 `json:"tax_free"`
	RMD           float64 `json:"rmd"`
	RothConverted float64 `json:"roth_converted"`
}

// TaxBreakdown is the estimated tax for one year.
type TaxBreakdown struct {
	Federal  float64 `json:"federal"`
	State    float64 `json:"state"`
	Gains    float64 `json:"gains"`
	Total    float64 `json:"total"`
	Marginal float64 `json:"marginal_rate"`
}

// YearRow is the full detail of one simulated year on the deterministic
// (median-market) path.
type YearRow struct {
	Age     int  `json:"age"`
	Year    int  `json:"year"`
	Retired bool `json:"retired"`

	Start Buckets `json:"start"`
	End   Buckets `json:"end"`

	Saving             float64 `json:"saving"`
	RetirementSpending float64 `json:"retirement_spending"`
	OtherSpending      float64 `json:"other_spending"`
	OtherIncome        float64 `json:"other_income"`
	CashFlow           float64 `json:"cash_flow"`
	TotalSpending      float64 `json:"total_spending"`
	Reinvested         float64 `json:"reinvested"`
	Shortfall          float64 `json:"shortfall"`

	IncomeItems   []Item       `json:"income_items"`
	SpendingItems []Item       `json:"spending_items"`
	Withdrawals   Withdrawals  `json:"withdrawals"`
	Taxes         TaxBreakdown `json:"taxes"`
	Insights      []string     `json:"insights"`

	// Monte Carlo percentile bands of the *starting* portfolio.
	PortfolioP10 float64 `json:"portfolio_p10"`
	PortfolioP50 float64 `json:"portfolio_p50"`
	PortfolioP90 float64 `json:"portfolio_p90"`
}

const (
	bCash = iota
	bTaxable
	bTaxDeferred
	bTaxFree
	nBuckets
)

type liab struct {
	name     string
	category string
	sched    []LiabYear
}

// Plan is a prepared, reusable simulation.
type Plan struct {
	P        Profile
	In       Inputs
	N        int // number of simulated years (ages current..end inclusive)
	start    [nBuckets]float64
	basis    float64
	contrib  [nBuckets]float64 // annual contributions today (incl. employer match)
	muPre    [nBuckets]float64 // expected arithmetic return
	muPost   [nBuckets]float64
	liabs    []liab
	housing  float64 // annual property tax + insurance, today's dollars
	events   []Event
	birthYr  int
	rmdAge   int
	logParam [2][nBuckets][2]float64 // [phase][bucket]{m, s}
}

// Prepare converts raw inputs into a runnable plan.
func Prepare(in Inputs) *Plan {
	in.Profile.Normalize()
	p := in.Profile
	pl := &Plan{P: p, In: in}
	pl.N = p.EndAge - p.CurrentAge + 1
	pl.events = in.Events
	pl.birthYr = in.BaseYear - p.CurrentAge
	pl.rmdAge = rmdStartAge(pl.birthYr)

	var wSum, muPreSum, muPostSum [nBuckets]float64
	add := func(b int, bal, contrib, pre, post float64) {
		w := bal + 12*contrib + 1
		wSum[b] += w
		muPreSum[b] += w * pre
		muPostSum[b] += w * post
	}

	for _, a := range in.Accounts {
		c := a.Config
		switch a.Category {
		case CatCash:
			pl.start[bCash] += c.Balance
			pl.contrib[bCash] += 12 * c.MonthlyContribution
			add(bCash, c.Balance, c.MonthlyContribution, c.APY, c.APY)
		case CatInvestment:
			b := bTaxable
			switch c.TaxTreatment {
			case TaxTaxDeferred:
				b = bTaxDeferred
			case TaxTaxFree:
				b = bTaxFree
			}
			pl.start[b] += c.Balance
			pl.contrib[b] += 12 * (c.MonthlyContribution + c.EmployerMatch)
			if b == bTaxable {
				if c.CostBasis > 0 {
					pl.basis += minf(c.CostBasis, c.Balance)
				} else {
					pl.basis += c.Balance
				}
			}
			pre, post := p.ReturnPre, p.ReturnPost
			if c.ExpectedReturn > 0 {
				pre = c.ExpectedReturn
				if p.ReturnPre > 0 {
					post = c.ExpectedReturn * p.ReturnPost / p.ReturnPre
				} else {
					post = c.ExpectedReturn
				}
			}
			pre -= c.ExpenseRatio
			post -= c.ExpenseRatio
			add(b, c.Balance, c.MonthlyContribution+c.EmployerMatch, pre, post)
		case CatMortgage:
			pl.liabs = append(pl.liabs, liab{a.Name, a.Category, a.Amortize(pl.N)})
			pl.housing += c.PropertyTax + c.Insurance
		case CatCreditCard, CatDebt:
			pl.liabs = append(pl.liabs, liab{a.Name, a.Category, a.Amortize(pl.N)})
		}
	}

	for b := 0; b < nBuckets; b++ {
		if wSum[b] > 1.5 || b == bCash && wSum[b] > 0 {
			pl.muPre[b] = muPreSum[b] / wSum[b]
			pl.muPost[b] = muPostSum[b] / wSum[b]
		} else if b == bCash {
			pl.muPre[b], pl.muPost[b] = 0.02, 0.02
		} else {
			pl.muPre[b], pl.muPost[b] = p.ReturnPre, p.ReturnPost
		}
		for phase := 0; phase < 2; phase++ {
			mu := pl.muPre[b]
			if phase == 1 {
				mu = pl.muPost[b]
			}
			sigma := p.ReturnStdev
			if b == bCash {
				sigma = 0
			}
			pl.logParam[phase][b] = lognormal(mu, sigma)
		}
	}
	return pl
}

// lognormal converts an arithmetic mean return and volatility into the
// parameters (m, s) of log(1+r) ~ N(m, s^2).
func lognormal(mu, sigma float64) [2]float64 {
	g := 1 + mu
	if g < 0.05 {
		g = 0.05
	}
	s2 := math.Log(1 + (sigma/g)*(sigma/g))
	return [2]float64{math.Log(g) - s2/2, math.Sqrt(s2)}
}

type alloc struct {
	cash, taxable, td, roth float64
	taxableGains            float64
	shortfall               float64
}

// Simulate runs the plan once. z holds one standard-normal market shock per
// year (nil = median market). spendScale multiplies base retirement spending.
// When detail is true full year rows are returned. starts, if non-nil, is
// filled with the starting total portfolio of each year.
func (pl *Plan) Simulate(z []float64, spendScale float64, detail bool, starts []float64) (rows []YearRow, success bool, depletedAge int) {
	p := pl.P
	success = true
	bal := pl.start
	basis := pl.basis
	if detail {
		rows = make([]YearRow, 0, pl.N)
	}

	for t := 0; t < pl.N; t++ {
		age := p.CurrentAge + t
		year := pl.In.BaseYear + t
		infl := math.Pow(1+p.Inflation, float64(t))
		retired := age >= p.RetirementAge
		phase := 0
		if retired {
			phase = 1
		}
		tp := paramsFor(p.FilingStatus, infl)

		startTotal := bal[0] + bal[1] + bal[2] + bal[3]
		if starts != nil {
			starts[t] = startTotal
		}
		var startB Buckets
		if detail {
			startB = Buckets{bal[bCash], bal[bTaxable], bal[bTaxDeferred], bal[bTaxFree], startTotal}
		}

		// --- contributions (pre-retirement only) ---
		var contrib [nBuckets]float64
		saving := 0.0
		if !retired {
			cg := math.Pow(1+p.ContributionGrowth, float64(t))
			for b := 0; b < nBuckets; b++ {
				contrib[b] = pl.contrib[b] * cg
				saving += contrib[b]
			}
		}

		// --- events ---
		var ordInc, ssInc, nonTaxInc, userSpend float64
		var incItems, spItems []Item
		for _, ev := range pl.events {
			if age < ev.StartAge || (ev.Years > 0 && age >= ev.StartAge+ev.Years) {
				continue
			}
			amt := ev.AnnualAmount
			if ev.InflationAdjusted {
				amt *= infl
			}
			if amt == 0 {
				continue
			}
			if ev.Kind == "income" {
				switch {
				case ev.Type == "social_security":
					ssInc += amt
				case ev.Taxable:
					ordInc += amt
				default:
					nonTaxInc += amt
				}
				if detail {
					incItems = append(incItems, Item{Name: ev.Name, Type: ev.Type, Amount: amt})
				}
			} else {
				userSpend += amt
				if detail {
					spItems = append(spItems, Item{Name: ev.Name, Type: ev.Type, Amount: amt})
				}
			}
		}

		// --- retirement spending, liabilities, housing ---
		baseSpend, liabSpend, housingSpend := 0.0, 0.0, 0.0
		if retired {
			baseSpend = p.RetirementSpending * spendScale * infl
			for _, l := range pl.liabs {
				if t < len(l.sched) && l.sched[t].Payment > 0 {
					liabSpend += l.sched[t].Payment
					if detail {
						spItems = append(spItems, Item{Name: l.name, Type: l.category, Amount: l.sched[t].Payment})
					}
				}
			}
			housingSpend = pl.housing * infl
			if detail && housingSpend > 0 {
				spItems = append(spItems, Item{Name: "Property tax & insurance", Type: "housing", Amount: housingSpend})
			}
		}
		otherSpend := userSpend + liabSpend + housingSpend
		spend := baseSpend + otherSpend

		// --- passive taxable income (interest + dividends) ---
		cashRate := math.Exp(pl.logParam[phase][bCash][0]) - 1
		cashInterest := bal[bCash] * cashRate
		divs := bal[bTaxable] * minf(p.DividendYield, maxf(0, pl.muPre[bTaxable]))

		// --- required minimum distribution ---
		rmd := 0.0
		if age >= pl.rmdAge && bal[bTaxDeferred] > 0 {
			rmd = minf(bal[bTaxDeferred], bal[bTaxDeferred]/rmdDivisor(age))
		}

		cashIn := ordInc + ssInc + nonTaxInc + rmd
		gainFrac := 0.0
		if bal[bTaxable] > 0 {
			gainFrac = maxf(0, 1-basis/bal[bTaxable])
		}

		// --- solve for withdrawals so that after-tax cash meets spending ---
		taxEst := 0.0
		var a alloc
		var tr taxResult
		var conv float64
		for it := 0; it < 60; it++ {
			need := spend + taxEst - cashIn
			ordBase := ordInc + cashInterest + rmd
			a = pl.allocate(bal, gainFrac, need, rmd, ordBase, ssInc, tp)
			conv = 0
			if retired && p.RothConversionCeiling > 0 {
				room := tp.roomInBracket(p.RothConversionCeiling, ordBase+a.td, ssInc)
				conv = maxf(0, minf(room, bal[bTaxDeferred]-rmd-a.td))
			}
			tr = tp.compute(ordBase+a.td+conv, ssInc, divs+a.taxableGains, p.StateTaxRate)
			if math.Abs(tr.Total-taxEst) < 0.5 {
				taxEst = tr.Total
				break
			}
			taxEst = tr.Total
		}
		tr.Total = tr.Federal + tr.Gains + tr.State

		// --- apply flows ---
		bal[bCash] -= a.cash
		if bal[bTaxable] > 0 && a.taxable > 0 {
			basis *= 1 - minf(1, a.taxable/bal[bTaxable])
		}
		bal[bTaxable] -= a.taxable
		bal[bTaxDeferred] -= rmd + a.td + conv
		bal[bTaxFree] += conv - a.roth
		for b := 0; b < nBuckets; b++ {
			if bal[b] < 0 {
				bal[b] = 0
			}
		}
		basis += divs // dividends are taxed up-front and reinvested

		reinvested := 0.0
		if surplus := cashIn - spend - tr.Total; surplus > 0.5 {
			reinvested = surplus
			bal[bTaxable] += surplus
			basis += surplus
		}
		if a.shortfall > 1 {
			success = false
			if depletedAge == 0 {
				depletedAge = age
			}
		}

		// --- growth ---
		zt := 0.0
		if z != nil && t < len(z) {
			zt = z[t]
		}
		var endB Buckets
		for b := 0; b < nBuckets; b++ {
			lp := pl.logParam[phase][b]
			g := math.Exp(lp[0]+lp[1]*zt) - 1
			if g < -0.7 {
				g = -0.7
			}
			bal[b] = bal[b]*(1+g) + contrib[b]*math.Sqrt(1+g)
		}
		basis += contrib[bTaxable]
		if detail {
			endB = Buckets{bal[bCash], bal[bTaxable], bal[bTaxDeferred], bal[bTaxFree], bal[0] + bal[1] + bal[2] + bal[3]}
			cashFlow := saving + ordInc + ssInc + nonTaxInc - spend - tr.Total
			rows = append(rows, YearRow{
				Age: age, Year: year, Retired: retired,
				Start: startB, End: endB,
				Saving: saving, RetirementSpending: baseSpend, OtherSpending: otherSpend,
				OtherIncome:   ordInc + ssInc + nonTaxInc,
				CashFlow:      cashFlow,
				TotalSpending: spend,
				Reinvested:    reinvested,
				Shortfall:     a.shortfall,
				IncomeItems:   incItems,
				SpendingItems: spItems,
				Withdrawals: Withdrawals{
					Cash: a.cash, Taxable: a.taxable, TaxDeferred: a.td + rmd, TaxFree: a.roth,
					RMD: rmd, RothConverted: conv,
				},
				Taxes: TaxBreakdown{Federal: tr.Federal, State: tr.State, Gains: tr.Gains, Total: tr.Total, Marginal: tr.Marginal},
			})
		}
	}
	return
}

// allocate decides how much to withdraw from each bucket to cover `need`
// according to the configured strategy.
func (pl *Plan) allocate(bal [nBuckets]float64, gainFrac, need, rmd, ordBase, ss float64, tp taxParams) alloc {
	var a alloc
	remaining := maxf(need, 0)
	if remaining == 0 {
		return a
	}
	a.cash = minf(bal[bCash], remaining)
	remaining -= a.cash

	tdAvail := maxf(0, bal[bTaxDeferred]-rmd)
	takeTD := func(limit float64) {
		x := minf(minf(tdAvail-a.td, remaining), limit)
		if x > 0 {
			a.td += x
			remaining -= x
		}
	}
	takeTaxable := func() {
		x := minf(bal[bTaxable], remaining)
		if x > 0 {
			a.taxable = x
			a.taxableGains = x * gainFrac
			remaining -= x
		}
	}
	switch pl.P.WithdrawalStrategy {
	case StrategyTaxableFirst:
		takeTaxable()
		takeTD(math.Inf(1))
	case StrategyTaxDeferredFirst:
		takeTD(math.Inf(1))
		takeTaxable()
	default: // smart: use cheap low-bracket ordinary income first, then taxable, then the rest
		takeTD(tp.roomInBracket(12, ordBase, ss))
		takeTaxable()
		takeTD(math.Inf(1))
	}
	if remaining > 0 {
		x := minf(bal[bTaxFree], remaining)
		a.roth = x
		remaining -= x
	}
	if remaining > 0.005 {
		a.shortfall = remaining
	}
	return a
}
