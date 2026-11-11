package sim

// A simplified US federal + flat state tax model (2025 law), with bracket
// thresholds indexed to inflation. It is an estimation tool, not tax advice.

type bracket struct {
	top  float64 // upper bound of taxable income for this bracket
	rate float64
}

var singleBrackets = []bracket{
	{11925, 0.10}, {48475, 0.12}, {103350, 0.22}, {197300, 0.24},
	{250525, 0.32}, {626350, 0.35}, {1e18, 0.37},
}

var marriedBrackets = []bracket{
	{23850, 0.10}, {96950, 0.12}, {206700, 0.22}, {394600, 0.24},
	{501050, 0.32}, {751600, 0.35}, {1e18, 0.37},
}

type taxParams struct {
	brackets   []bracket
	stdDed     float64
	ltcg0Top   float64 // top of 0% long-term gains bracket (taxable income)
	ltcg15Top  float64
	ssT1, ssT2 float64 // provisional income thresholds (not indexed)
}

func paramsFor(filing string, infl float64) taxParams {
	var p taxParams
	if filing == "single" {
		p = taxParams{brackets: singleBrackets, stdDed: 15750, ltcg0Top: 48350, ltcg15Top: 533400, ssT1: 25000, ssT2: 34000}
	} else {
		p = taxParams{brackets: marriedBrackets, stdDed: 31500, ltcg0Top: 96700, ltcg15Top: 600050, ssT1: 32000, ssT2: 44000}
	}
	// Index brackets/deduction to inflation (thresholds for SS are fixed by law).
	scaled := make([]bracket, len(p.brackets))
	for i, b := range p.brackets {
		top := b.top
		if top < 1e17 {
			top *= infl
		}
		scaled[i] = bracket{top: top, rate: b.rate}
	}
	p.brackets = scaled
	p.stdDed *= infl
	p.ltcg0Top *= infl
	p.ltcg15Top *= infl
	return p
}

// taxResult is the outcome of a single-year tax calculation.
type taxResult struct {
	Federal   float64 // ordinary income tax
	Gains     float64 // long-term gains & qualified dividends tax
	State     float64
	SSTaxable float64
	Marginal  float64 // marginal ordinary rate
	Total     float64
}

// taxableSS returns the taxable portion of Social Security benefits.
func (p taxParams) taxableSS(otherOrdinary, ss float64) float64 {
	if ss <= 0 {
		return 0
	}
	pi := otherOrdinary + 0.5*ss
	if pi <= p.ssT1 {
		return 0
	}
	var t float64
	if pi <= p.ssT2 {
		t = 0.5 * (pi - p.ssT1)
	} else {
		t = 0.85*(pi-p.ssT2) + minf(0.5*ss, 0.5*(p.ssT2-p.ssT1))
	}
	return minf(t, 0.85*ss)
}

func (p taxParams) ordinaryTax(taxable float64) (tax, marginal float64) {
	prev := 0.0
	marginal = p.brackets[0].rate
	for _, b := range p.brackets {
		if taxable <= prev {
			break
		}
		top := minf(taxable, b.top)
		tax += (top - prev) * b.rate
		marginal = b.rate
		prev = b.top
	}
	return
}

// compute estimates federal and state tax for one year.
//
//	ordinary: wages, pensions, interest, tax-deferred withdrawals, Roth conversions
//	ss:       gross Social Security benefits
//	gains:    long-term capital gains + qualified dividends
func (p taxParams) compute(ordinary, ss, gains, stateRate float64) taxResult {
	var r taxResult
	r.SSTaxable = p.taxableSS(ordinary, ss)
	agi := ordinary + r.SSTaxable
	taxableOrd := maxf(0, agi-p.stdDed)
	leftoverDed := maxf(0, p.stdDed-agi)
	taxableGains := maxf(0, gains-leftoverDed)

	r.Federal, r.Marginal = p.ordinaryTax(taxableOrd)

	// Stack long-term gains on top of ordinary taxable income.
	g := taxableGains
	room0 := maxf(0, p.ltcg0Top-taxableOrd)
	in0 := minf(g, room0)
	g -= in0
	room15 := maxf(0, p.ltcg15Top-maxf(taxableOrd, p.ltcg0Top))
	in15 := minf(g, room15)
	g -= in15
	r.Gains = in15*0.15 + g*0.20

	// State: flat rate on income excluding Social Security.
	r.State = maxf(0, ordinary+gains) * stateRate
	r.Total = r.Federal + r.Gains + r.State
	return r
}

// roomInBracket returns how much additional ordinary income can be realised
// before taxable ordinary income exceeds the top of the bracket with the given
// rate (as a percentage, e.g. 12), given the ordinary income already present.
func (p taxParams) roomInBracket(ratePct int, ordinary, ss float64) float64 {
	target := float64(ratePct) / 100
	top := 0.0
	for _, b := range p.brackets {
		if b.rate <= target+1e-9 {
			top = b.top
		}
	}
	if top == 0 {
		return 0
	}
	// Binary search the extra ordinary income x so taxable ordinary == top.
	lo, hi := 0.0, top+p.stdDed
	for i := 0; i < 40; i++ {
		mid := (lo + hi) / 2
		agi := ordinary + mid + p.taxableSS(ordinary+mid, ss)
		if maxf(0, agi-p.stdDed) <= top {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

// Uniform Lifetime Table (IRS) distribution periods by age.
var rmdDivisors = map[int]float64{
	72: 27.4, 73: 26.5, 74: 25.5, 75: 24.6, 76: 23.7, 77: 22.9, 78: 22.0, 79: 21.1,
	80: 20.2, 81: 19.4, 82: 18.5, 83: 17.7, 84: 16.8, 85: 16.0, 86: 15.2, 87: 14.4,
	88: 13.7, 89: 12.9, 90: 12.2, 91: 11.5, 92: 10.8, 93: 10.1, 94: 9.5, 95: 8.9,
	96: 8.4, 97: 7.8, 98: 7.3, 99: 6.8, 100: 6.4, 101: 6.0, 102: 5.6, 103: 5.2,
	104: 4.9, 105: 4.6, 106: 4.3, 107: 4.1, 108: 3.9, 109: 3.7, 110: 3.5,
}

// rmdStartAge: 73 for people born 1959 or earlier, 75 for 1960+.
func rmdStartAge(birthYear int) int {
	if birthYear >= 1960 {
		return 75
	}
	return 73
}

func rmdDivisor(age int) float64 {
	if d, ok := rmdDivisors[age]; ok {
		return d
	}
	if age > 110 {
		return 3.5
	}
	return 27.4
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
