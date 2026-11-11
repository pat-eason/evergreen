package sim

import (
	"math"
	"testing"
)

func sampleInputs() Inputs {
	p := DefaultProfile()
	p.CurrentAge, p.RetirementAge, p.EndAge = 36, 45, 90
	p.RetirementSpending = 24000
	return Inputs{
		Profile:  p,
		BaseYear: 2026,
		Accounts: []Account{
			{Name: "401k", Category: CatInvestment, Config: AccountConfig{Balance: 300000, TaxTreatment: TaxTaxDeferred, MonthlyContribution: 2000}},
			{Name: "Brokerage", Category: CatInvestment, Config: AccountConfig{Balance: 100000, CostBasis: 70000, TaxTreatment: TaxTaxable, MonthlyContribution: 2000}},
			{Name: "Roth", Category: CatInvestment, Config: AccountConfig{Balance: 60000, TaxTreatment: TaxTaxFree, MonthlyContribution: 1000}},
			{Name: "HYSA", Category: CatCash, Config: AccountConfig{Balance: 40000, APY: 0.035, MonthlyContribution: 3000}},
			{Name: "Mortgage", Category: CatMortgage, Config: AccountConfig{Balance: 300000, InterestRate: 0.045, RemainingYears: 26, MonthlyPayment: 2000, HomeValue: 450000}},
		},
		Events: []Event{
			{Kind: "income", Type: "social_security", Name: "Social Security", StartAge: 67, AnnualAmount: 36000, InflationAdjusted: true},
			{Kind: "spending", Type: "health", Name: "Health Care", StartAge: 45, Years: 20, AnnualAmount: 20000, InflationAdjusted: true},
		},
	}
}

func TestAmortization(t *testing.T) {
	pay := MonthlyAmortPayment(300000, 0.06, 30)
	if math.Abs(pay-1798.65) > 0.1 {
		t.Fatalf("payment = %.2f", pay)
	}
	a := Account{Category: CatMortgage, Config: AccountConfig{Balance: 300000, InterestRate: 0.06, MonthlyPayment: pay}}
	s := a.Amortize(31)
	if s[29].EndBalance > 1 || s[0].EndBalance >= 300000 {
		t.Fatalf("bad schedule: y1=%.0f y30=%.0f", s[0].EndBalance, s[29].EndBalance)
	}
}

func TestTax(t *testing.T) {
	tp := paramsFor("single", 1)
	r := tp.compute(65750, 0, 0, 0) // 65750 - 15750 = 50000 taxable
	// 10%*11925 + 12%*(48475-11925) + 22%*(50000-48475)
	want := 1192.5 + 4386 + 335.5
	if math.Abs(r.Federal-want) > 0.01 {
		t.Fatalf("federal = %.2f want %.2f", r.Federal, want)
	}
	if tp.taxableSS(0, 30000) != 0 {
		t.Fatal("expected no taxable SS")
	}
	if got := tp.taxableSS(40000, 30000); got <= 0 || got > 0.85*30000 {
		t.Fatalf("taxable SS = %f", got)
	}
	// 0% gains when under threshold
	if g := tp.compute(0, 0, 30000, 0).Gains; g != 0 {
		t.Fatalf("gains tax = %f", g)
	}
}

func TestPlanRuns(t *testing.T) {
	res := RunPlan(sampleInputs(), Options{Trials: 300})
	if len(res.Years) != 55 {
		t.Fatalf("years = %d", len(res.Years))
	}
	if res.Summary.SuccessRate < 0 || res.Summary.SuccessRate > 1 {
		t.Fatalf("success = %v", res.Summary.SuccessRate)
	}
	pre := res.Years[0]
	if pre.Retired || pre.Saving < 90000 {
		t.Fatalf("expected savings in year 0, got %+v", pre.Saving)
	}
	ret := res.Years[9]
	if !ret.Retired || ret.RetirementSpending <= 0 {
		t.Fatalf("expected retirement at 45: %+v", ret)
	}
	// withdrawals must roughly equal spending + taxes - income
	for _, r := range res.Years {
		if !r.Retired || r.Shortfall > 1 {
			continue
		}
		got := r.Withdrawals.Cash + r.Withdrawals.Taxable + r.Withdrawals.TaxDeferred + r.Withdrawals.TaxFree + r.OtherIncome + 0.001
		need := r.TotalSpending + r.Taxes.Total + r.Reinvested
		if math.Abs(got-need) > 2 {
			t.Fatalf("age %d: sources %.0f != uses %.0f", r.Age, got, need)
		}
	}
	// percentile bands ordered
	for _, r := range res.Years {
		if r.PortfolioP10 > r.PortfolioP50+1 || r.PortfolioP50 > r.PortfolioP90+1 {
			t.Fatalf("band order broken at %d", r.Age)
		}
	}
	t.Logf("success=%.2f planned=%.0f projected=%.0f atRetire=%.0f end=%.0f depleted=%d",
		res.Summary.SuccessRate, res.Summary.PlannedMonthly, res.Summary.ProjectedMonthly,
		res.Summary.PortfolioAtRetire, res.Summary.PortfolioAtEnd, res.Summary.DepletedAge)
}

func TestStrategiesAndRoth(t *testing.T) {
	for _, s := range []string{StrategySmart, StrategyTaxableFirst, StrategyTaxDeferredFirst} {
		in := sampleInputs()
		in.Profile.WithdrawalStrategy = s
		in.Profile.RothConversionCeiling = 12
		r := RunPlan(in, Options{Trials: 50, SkipAbility: true})
		t.Logf("%s taxes=%.0f end=%.0f", s, r.Summary.LifetimeTaxes, r.Summary.PortfolioAtEnd)
	}
}

func TestDeterministic(t *testing.T) {
	a := RunPlan(sampleInputs(), Options{Trials: 100, SkipAbility: true})
	b := RunPlan(sampleInputs(), Options{Trials: 100, SkipAbility: true})
	if a.Summary.SuccessRate != b.Summary.SuccessRate || a.Years[20].End.Total != b.Years[20].End.Total {
		t.Fatal("non-deterministic")
	}
}

func TestFreedom(t *testing.T) {
	r := AnalyzeFreedom(FreedomInputs{
		CurrentAge: 36, RetirementAge: 60, Investable: 400000, AnnualContribution: 60000,
		ContributionGrowth: 0.03, ReturnRate: 0.07, Inflation: 0.025,
		AnnualSpending: 60000, CurrentExpenses: 50000, SafeWithdrawalRate: 0.04,
	})
	if r.Freedom.FINumber != 1500000 {
		t.Fatalf("fi number %v", r.Freedom.FINumber)
	}
	if r.Freedom.FIAge == 0 || r.Freedom.FIAge <= 36 {
		t.Fatalf("fi age %d", r.Freedom.FIAge)
	}
	if !r.Crossover.Reached || r.Crossover.Age < 36 {
		t.Fatalf("crossover %+v", r.Crossover)
	}
	if r.Coast.CoastNumberToday <= 0 || r.Coast.CoastAge == 0 {
		t.Fatalf("coast %+v", r.Coast)
	}
	t.Logf("FI age %d, crossover age %d, coast age %d, coast today %.0f", r.Freedom.FIAge, r.Crossover.Age, r.Coast.CoastAge, r.Coast.CoastNumberToday)
}

// Regression: crossover must still be found when FI is reached before retirement.
func TestCrossoverWhenFIReachedEarly(t *testing.T) {
	r := AnalyzeFreedom(FreedomInputs{
		CurrentAge: 36, RetirementAge: 45, Investable: 460000, AnnualContribution: 103000,
		ContributionGrowth: 0.03, ReturnRate: 0.07, Inflation: 0.025,
		AnnualSpending: 24000, CurrentExpenses: 44000, SafeWithdrawalRate: 0.04,
	})
	if r.Freedom.FIAge == 0 || r.Freedom.FIAge >= 45 {
		t.Fatalf("expected FI before retirement, got %d", r.Freedom.FIAge)
	}
	if !r.Crossover.Reached || r.Crossover.Age <= 36 {
		t.Fatalf("crossover should be found even after FI: %+v", r.Crossover)
	}
	for _, p := range r.Series {
		if p.Age < r.Crossover.Age && p.Contributions == 0 {
			t.Fatalf("contributions should continue until crossover (age %d)", p.Age)
		}
	}
}
