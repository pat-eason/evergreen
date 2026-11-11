// Package sim contains the financial planning engine: account/liability
// modelling, a US tax model, tax-aware withdrawal strategies, Monte Carlo
// projections and the Financial Freedom / Coast FIRE / Contribution
// Crossover analyses.
package sim

import "strings"

// Account categories.
const (
	CatCash       = "cash"
	CatInvestment = "investment"
	CatMortgage   = "mortgage"
	CatCreditCard = "credit_card"
	CatDebt       = "debt"
)

// Tax treatments for investment accounts.
const (
	TaxTaxable     = "taxable"
	TaxTaxDeferred = "tax_deferred"
	TaxTaxFree     = "tax_free"
)

// Withdrawal strategies.
const (
	StrategySmart            = "smart"
	StrategyTaxableFirst     = "taxable_first"
	StrategyTaxDeferredFirst = "tax_deferred_first"
)

// Profile holds the global planning assumptions for a user. All rates are
// decimals (0.07 == 7%).
type Profile struct {
	FirstName             string  `json:"first_name"`
	LastName              string  `json:"last_name"`
	BirthYear             int     `json:"birth_year"` // source of truth for age; current_age is derived from it
	CurrentAge            int     `json:"current_age"`
	RetirementAge         int     `json:"retirement_age"`
	EndAge                int     `json:"end_age"`
	FilingStatus          string  `json:"filing_status"` // single | married
	Inflation             float64 `json:"inflation"`
	ReturnPre             float64 `json:"return_pre"`
	ReturnPost            float64 `json:"return_post"`
	ReturnStdev           float64 `json:"return_stdev"`
	ContributionGrowth    float64 `json:"contribution_growth"`
	RetirementSpending    float64 `json:"retirement_spending"` // annual, today's dollars
	AnnualIncome          float64 `json:"annual_income"`       // current take-home pay, annual
	AnnualExpenses        float64 `json:"annual_expenses"`     // current living costs, annual
	SafeWithdrawalRate    float64 `json:"safe_withdrawal_rate"`
	StateTaxRate          float64 `json:"state_tax_rate"`
	DividendYield         float64 `json:"dividend_yield"`
	WithdrawalStrategy    string  `json:"withdrawal_strategy"`
	RothConversionCeiling int     `json:"roth_conversion_ceiling"` // 0 = off, else 12/22/24 (% bracket)
}

// DefaultProfile returns sensible planning defaults.
func DefaultProfile() Profile {
	return Profile{
		CurrentAge:            35,
		RetirementAge:         60,
		EndAge:                90,
		FilingStatus:          "married",
		Inflation:             0.025,
		ReturnPre:             0.07,
		ReturnPost:            0.055,
		ReturnStdev:           0.12,
		ContributionGrowth:    0.03,
		RetirementSpending:    60000,
		AnnualIncome:          120000,
		AnnualExpenses:        60000,
		SafeWithdrawalRate:    0.04,
		StateTaxRate:          0.03,
		DividendYield:         0.015,
		WithdrawalStrategy:    StrategySmart,
		RothConversionCeiling: 0,
	}
}

// Normalize clamps a profile into a sane, simulatable range.
func (p *Profile) Normalize() {
	d := DefaultProfile()
	p.FirstName, p.LastName = cleanName(p.FirstName), cleanName(p.LastName)
	if p.CurrentAge < 18 || p.CurrentAge > 100 {
		p.CurrentAge = d.CurrentAge
	}
	if p.EndAge <= p.CurrentAge {
		p.EndAge = p.CurrentAge + 1
	}
	if p.EndAge > 110 {
		p.EndAge = 110
	}
	if p.RetirementAge < p.CurrentAge {
		p.RetirementAge = p.CurrentAge
	}
	if p.RetirementAge > p.EndAge {
		p.RetirementAge = p.EndAge
	}
	if p.FilingStatus != "single" && p.FilingStatus != "married" {
		p.FilingStatus = "married"
	}
	p.Inflation = clamp(p.Inflation, -0.05, 0.2)
	p.ReturnPre = clamp(p.ReturnPre, -0.5, 0.5)
	p.ReturnPost = clamp(p.ReturnPost, -0.5, 0.5)
	p.ReturnStdev = clamp(p.ReturnStdev, 0, 0.6)
	p.ContributionGrowth = clamp(p.ContributionGrowth, -0.5, 0.5)
	p.StateTaxRate = clamp(p.StateTaxRate, 0, 0.2)
	p.DividendYield = clamp(p.DividendYield, 0, 0.1)
	if p.SafeWithdrawalRate <= 0 {
		p.SafeWithdrawalRate = d.SafeWithdrawalRate
	}
	p.SafeWithdrawalRate = clamp(p.SafeWithdrawalRate, 0.005, 0.2)
	if p.RetirementSpending < 0 {
		p.RetirementSpending = 0
	}
	if p.AnnualIncome < 0 {
		p.AnnualIncome = 0
	}
	if p.AnnualExpenses < 0 {
		p.AnnualExpenses = 0
	}
	switch p.WithdrawalStrategy {
	case StrategySmart, StrategyTaxableFirst, StrategyTaxDeferredFirst:
	default:
		p.WithdrawalStrategy = StrategySmart
	}
	switch p.RothConversionCeiling {
	case 0, 10, 12, 22, 24, 32:
	default:
		p.RothConversionCeiling = 0
	}
}

// AccountConfig is the union of per-category configuration. Only the fields
// relevant to an account's category are used.
type AccountConfig struct {
	Balance float64 `json:"balance"`

	// cash
	APY float64 `json:"apy,omitempty"`

	// cash + investment
	MonthlyContribution float64 `json:"monthly_contribution,omitempty"`

	// investment
	TaxTreatment   string  `json:"tax_treatment,omitempty"`
	ExpectedReturn float64 `json:"expected_return,omitempty"` // 0 = use profile default
	EmployerMatch  float64 `json:"employer_match,omitempty"`  // monthly
	CostBasis      float64 `json:"cost_basis,omitempty"`      // taxable only; 0 = assume == balance
	ExpenseRatio   float64 `json:"expense_ratio,omitempty"`

	// mortgage + debt
	InterestRate   float64 `json:"interest_rate,omitempty"`
	MonthlyPayment float64 `json:"monthly_payment,omitempty"`
	RemainingYears float64 `json:"remaining_years,omitempty"`

	// mortgage
	HomeValue        float64 `json:"home_value,omitempty"`
	HomeAppreciation float64 `json:"home_appreciation,omitempty"`
	PropertyTax      float64 `json:"property_tax,omitempty"` // annual
	Insurance        float64 `json:"insurance,omitempty"`    // annual

	// credit card
	APR         float64 `json:"apr,omitempty"`
	CreditLimit float64 `json:"credit_limit,omitempty"`
	PayInFull   bool    `json:"pay_in_full,omitempty"`

	// debt
	DebtKind string `json:"debt_kind,omitempty"`
}

// Account is a user-recorded financial account.
type Account struct {
	ID       int64         `json:"id"`
	Owner    string        `json:"owner"` // "self", "joint", or a household member id
	Name     string        `json:"name"`
	Category string        `json:"category"`
	Config   AccountConfig `json:"config"`
}

// IsAsset reports whether the account adds to net worth.
func (a Account) IsAsset() bool { return a.Category == CatCash || a.Category == CatInvestment }

// Event is an income event (Social Security, pension, side income...) or a
// spending goal (health care, travel, a car...) that starts at a given age.
type Event struct {
	ID                int64   `json:"id"`
	Owner             string  `json:"owner"` // "self" or a household member id; StartAge is that person's age
	Kind              string  `json:"kind"`  // income | spending
	Type              string  `json:"type"`  // social_security, pension, part_time, rental, other / health, travel, home, other
	Name              string  `json:"name"`
	StartAge          int     `json:"start_age"`
	Years             int     `json:"years"` // 0 = for life
	AnnualAmount      float64 `json:"annual_amount"`
	InflationAdjusted bool    `json:"inflation_adjusted"`
	Taxable           bool    `json:"taxable"`
}

func cleanName(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 40 {
		s = string(r[:40])
	}
	return s
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
