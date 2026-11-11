package sim

import "math"

// LiabYear is one year of a liability amortisation schedule.
type LiabYear struct {
	Payment    float64 `json:"payment"`
	EndBalance float64 `json:"end_balance"`
}

// MonthlyAmortPayment computes the level monthly payment for a loan.
func MonthlyAmortPayment(balance, annualRate, years float64) float64 {
	n := years * 12
	if balance <= 0 || n <= 0 {
		return 0
	}
	i := annualRate / 12
	if i == 0 {
		return balance / n
	}
	return balance * i / (1 - math.Pow(1+i, -n))
}

// liabilityParams returns the (rate, monthly payment) used to amortise a
// liability account.
func (a Account) liabilityParams() (rate, payment float64) {
	c := a.Config
	switch a.Category {
	case CatCreditCard:
		rate = c.APR
		payment = c.MonthlyPayment
		if payment <= 0 {
			// minimum payment: 1% of balance + monthly interest, at least $25
			payment = maxf(25, c.Balance*0.01+c.Balance*rate/12)
		}
	case CatMortgage, CatDebt:
		rate = c.InterestRate
		payment = c.MonthlyPayment
		if payment <= 0 {
			years := c.RemainingYears
			if years <= 0 {
				years = 10
			}
			payment = MonthlyAmortPayment(c.Balance, rate, years)
		}
	}
	return
}

// Amortize returns a yearly schedule (len == years) for a liability account.
// Credit cards that are paid in full each month carry no long-term payments.
func (a Account) Amortize(years int) []LiabYear {
	out := make([]LiabYear, years)
	if a.IsAsset() || years <= 0 {
		return out
	}
	bal := a.Config.Balance
	if a.Category == CatCreditCard && a.Config.PayInFull {
		return out
	}
	rate, pay := a.liabilityParams()
	mr := rate / 12
	for y := 0; y < years; y++ {
		var paid float64
		for m := 0; m < 12 && bal > 0.005; m++ {
			interest := bal * mr
			p := pay
			if p > bal+interest {
				p = bal + interest
			}
			bal = bal + interest - p
			paid += p
			// Guard against payments that never cover interest.
			if bal > a.Config.Balance*50 {
				bal = a.Config.Balance * 50
			}
		}
		if bal < 0.005 {
			bal = 0
		}
		out[y] = LiabYear{Payment: paid, EndBalance: bal}
	}
	return out
}

// PayoffYears returns the whole years until the liability is paid off (0 if
// already zero). Returns -1 if it would not be paid off within maxYears.
func (a Account) PayoffYears(maxYears int) int {
	if a.Config.Balance <= 0 {
		return 0
	}
	for i, y := range a.Amortize(maxYears) {
		if y.EndBalance <= 0 {
			return i + 1
		}
	}
	return -1
}
