package sim

import "math"

// FreedomInputs drive the Contribution Crossover, Coast FIRE and Financial
// Freedom analyses. Everything is evaluated in today's (real) dollars.
type FreedomInputs struct {
	CurrentAge         int     `json:"current_age"`
	RetirementAge      int     `json:"retirement_age"`
	Investable         float64 `json:"investable"`          // cash + investments today
	AnnualContribution float64 `json:"annual_contribution"` // today's dollars
	ContributionGrowth float64 `json:"contribution_growth"` // nominal growth of contributions
	ReturnRate         float64 `json:"return_rate"`         // nominal
	Inflation          float64 `json:"inflation"`
	AnnualSpending     float64 `json:"annual_spending"`  // target in retirement, today's $
	CurrentExpenses    float64 `json:"current_expenses"` // today's living costs
	SafeWithdrawalRate float64 `json:"safe_withdrawal_rate"`
}

// FreedomPoint is one year of the accumulation projection (today's dollars).
type FreedomPoint struct {
	Age           int     `json:"age"`
	Portfolio     float64 `json:"portfolio"`
	Contributions float64 `json:"contributions"`
	Growth        float64 `json:"growth"` // investment growth earned this year
	PassiveIncome float64 `json:"passive_income"`
	CoastNumber   float64 `json:"coast_number"`    // needed *at this age* to coast to FI at retirement
	CoastGrowth   float64 `json:"coast_projected"` // portfolio if contributions stop now, at retirement age
	FINumber      float64 `json:"fi_number"`
}

// Crossover describes the Contribution Crossover point: when investment
// growth in a year first exceeds what you contribute.
type Crossover struct {
	Reached          bool    `json:"reached"`
	Age              int     `json:"age"`
	YearsAway        int     `json:"years_away"`
	PortfolioAtCross float64 `json:"portfolio_at_cross"`
	GrowthNow        float64 `json:"growth_now"`
	ContribNow       float64 `json:"contrib_now"`
	RatioNow         float64 `json:"ratio_now"` // growth / contributions
	PortfolioNeeded  float64 `json:"portfolio_needed_now"`
}

// CoastFIRE describes the Coast FIRE milestone: having enough invested that,
// with no further contributions, growth alone reaches your FI number by
// retirement.
type CoastFIRE struct {
	CoastNumberToday  float64 `json:"coast_number_today"`
	Coasting          bool    `json:"coasting"`
	Progress          float64 `json:"progress"`
	CoastAge          int     `json:"coast_age"` // 0 if never within horizon
	YearsToCoast      int     `json:"years_to_coast"`
	ProjectedAtRetire float64 `json:"projected_if_stop_now"`
	FINumberAtRetire  float64 `json:"fi_number_at_retirement"`
	Shortfall         float64 `json:"shortfall"`
	MonthlyCovered    float64 `json:"monthly_expenses_to_cover"` // earned income needed while coasting
	YearsToRetirement int     `json:"years_to_retirement"`
}

// FinancialFreedom describes the Financial Independence milestone.
type FinancialFreedom struct {
	FINumber       float64 `json:"fi_number"`
	LeanFI         float64 `json:"lean_fi"`
	FatFI          float64 `json:"fat_fi"`
	Progress       float64 `json:"progress"`
	FIAge          int     `json:"fi_age"` // 0 if not reachable by age 100
	YearsToFI      int     `json:"years_to_fi"`
	PassiveMonthly float64 `json:"passive_monthly_now"`
	FreedomRatio   float64 `json:"freedom_ratio"` // passive income / current expenses
	SavingsRate    float64 `json:"savings_rate"`
	MonthlyGap     float64 `json:"monthly_gap"`
	FIAtRetirement bool    `json:"fi_by_retirement"`
}

// FreedomResult is the combined analysis.
type FreedomResult struct {
	Inputs     FreedomInputs    `json:"inputs"`
	RealReturn float64          `json:"real_return"`
	Series     []FreedomPoint   `json:"series"`
	Crossover  Crossover        `json:"crossover"`
	Coast      CoastFIRE        `json:"coast"`
	Freedom    FinancialFreedom `json:"freedom"`
}

// AnalyzeFreedom computes the three headline FIRE concepts.
func AnalyzeFreedom(in FreedomInputs) FreedomResult {
	if in.SafeWithdrawalRate <= 0 {
		in.SafeWithdrawalRate = 0.04
	}
	if in.CurrentAge < 0 {
		in.CurrentAge = 0
	}
	if in.RetirementAge < in.CurrentAge {
		in.RetirementAge = in.CurrentAge
	}
	if in.Investable < 0 {
		in.Investable = 0
	}
	r := (1+in.ReturnRate)/(1+in.Inflation) - 1
	cgReal := (1+in.ContributionGrowth)/(1+in.Inflation) - 1
	fiNumber := in.AnnualSpending / in.SafeWithdrawalRate
	yearsToRet := in.RetirementAge - in.CurrentAge

	const maxAge = 100
	res := FreedomResult{Inputs: in, RealReturn: r}

	// Accumulate assuming you keep saving at this rate (through age 80) so each
	// milestone reports the earliest age you'd reach it, independent of when you
	// plan to retire.
	port := in.Investable
	contrib := in.AnnualContribution
	fiAge, crossAge, coastAge := 0, 0, 0
	var crossPort float64
	const workUntil = 80
	for age := in.CurrentAge; age <= maxAge; age++ {
		growth := port * r
		c := contrib
		if age >= workUntil {
			c = 0
		}
		pt := FreedomPoint{
			Age: age, Portfolio: port, Contributions: c, Growth: growth,
			PassiveIncome: port * in.SafeWithdrawalRate, FINumber: fiNumber,
		}
		n := float64(maxi(0, in.RetirementAge-age))
		pt.CoastNumber = fiNumber / math.Pow(1+r, n)
		pt.CoastGrowth = port * math.Pow(1+r, n)
		res.Series = append(res.Series, pt)

		if crossAge == 0 && port > 0 && c > 0 && growth >= c {
			crossAge, crossPort = age, port
		}
		if coastAge == 0 && port >= pt.CoastNumber {
			coastAge = age
		}
		if fiAge == 0 && port >= fiNumber && fiNumber > 0 {
			fiAge = age
		}
		port = port*(1+r) + c*math.Sqrt(1+r)
		contrib *= 1 + cgReal
		if crossAge != 0 && coastAge != 0 && fiAge != 0 && age >= in.RetirementAge+5 {
			break
		}
	}

	// --- Contribution Crossover ---
	cur := res.Series[0]
	co := Crossover{GrowthNow: cur.Growth, ContribNow: cur.Contributions}
	if cur.Contributions > 0 {
		co.RatioNow = cur.Growth / cur.Contributions
	}
	if r > 0 {
		co.PortfolioNeeded = cur.Contributions / r
	}
	if crossAge != 0 {
		co.Reached = true
		co.Age = crossAge
		co.YearsAway = crossAge - in.CurrentAge
		co.PortfolioAtCross = crossPort
	}
	res.Crossover = co

	// --- Coast FIRE ---
	coastToday := res.Series[0].CoastNumber
	cf := CoastFIRE{
		CoastNumberToday:  coastToday,
		Coasting:          in.Investable >= coastToday && fiNumber > 0,
		ProjectedAtRetire: res.Series[0].CoastGrowth,
		FINumberAtRetire:  fiNumber,
		YearsToRetirement: yearsToRet,
		MonthlyCovered:    in.CurrentExpenses / 12,
	}
	if coastToday > 0 {
		cf.Progress = minf(1, in.Investable/coastToday)
	} else {
		cf.Progress = 1
	}
	cf.Shortfall = maxf(0, coastToday-in.Investable)
	if coastAge != 0 {
		cf.CoastAge = coastAge
		cf.YearsToCoast = coastAge - in.CurrentAge
	}
	res.Coast = cf

	// --- Financial Freedom ---
	ff := FinancialFreedom{
		FINumber:       fiNumber,
		LeanFI:         fiNumber * 0.7,
		FatFI:          fiNumber * 1.5,
		PassiveMonthly: in.Investable * in.SafeWithdrawalRate / 12,
	}
	if fiNumber > 0 {
		ff.Progress = minf(1, in.Investable/fiNumber)
	}
	if in.CurrentExpenses > 0 {
		ff.FreedomRatio = in.Investable * in.SafeWithdrawalRate / in.CurrentExpenses
	}
	totalIncome := in.CurrentExpenses + in.AnnualContribution
	if totalIncome > 0 {
		ff.SavingsRate = in.AnnualContribution / totalIncome
	}
	ff.MonthlyGap = maxf(0, (fiNumber-in.Investable)*in.SafeWithdrawalRate/12)
	if fiAge != 0 {
		ff.FIAge = fiAge
		ff.YearsToFI = fiAge - in.CurrentAge
		ff.FIAtRetirement = fiAge <= in.RetirementAge
	}
	res.Freedom = ff
	return res
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}
