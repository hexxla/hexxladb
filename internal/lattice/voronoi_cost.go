package lattice

import (
	"cmp"
	"math"
	"math/big"
)

// Every finite binary64 is a multiple of 2^-1074. We stop after the
// first sum above MaxFloat64, so one further hop cannot exceed 2^1025.
// 2,112 bits cover all significant bits in that range.
const voronoiCostPrecision = 2112

// voronoiCost keeps an exact two-term sum for the common case. The optional
// immutable value preserves sums that need more than two binary64 terms.
type voronoiCost struct {
	rounded float64
	tail    float64
	exact   *big.Float
}

func (c voronoiCost) advance(penalty float64) voronoiCost {
	if c.exact == nil {
		if step, ok := c.addFloat(1); ok {
			if sum, ok := step.addFloat(penalty); ok {
				return sum
			}
		}
	}
	exact := new(big.Float).SetPrec(voronoiCostPrecision)
	var term big.Float
	if c.exact == nil {
		exact.SetFloat64(c.rounded)
		exact.Add(exact, term.SetFloat64(c.tail))
	} else {
		exact.Set(c.exact)
	}
	exact.Add(exact, term.SetFloat64(1))
	exact.Add(exact, term.SetFloat64(penalty))
	rounded, _ := exact.Float64()
	return voronoiCost{rounded: rounded, exact: exact}
}

// addFloat uses error-free TwoSum steps. The final normalization puts the
// nearest binary64 value in rounded and its exact residual in tail.
func (c voronoiCost) addFloat(term float64) (voronoiCost, bool) {
	high, residual := twoSum(c.rounded, term)
	if math.IsInf(high, 1) {
		return voronoiCost{}, false
	}
	if c.tail == 0 {
		return voronoiCost{rounded: high, tail: residual}, true
	}
	low, last := twoSum(c.tail, residual)
	high, low = twoSum(high, low)
	low, leftover := twoSum(low, last)
	if leftover != 0 || math.IsInf(high, 1) {
		return voronoiCost{}, false
	}
	high, low = twoSum(high, low)
	return voronoiCost{rounded: high, tail: low}, !math.IsInf(high, 1)
}

// TwoSum uses the error-free transformation described in Algorithm 3.1 of
// Ogita, Rump and Oishi, Accurate Sum and Dot Product (2005).
func twoSum(a, b float64) (sum, residual float64) {
	sum = a + b
	z := sum - a
	return sum, (a - (sum - z)) + (b - z)
}

func (c voronoiCost) compare(other voronoiCost) int {
	order := cmp.Compare(c.rounded, other.rounded)
	if order != 0 {
		return order
	}
	if c.exact == nil && other.exact == nil {
		return cmp.Compare(c.tail, other.tail)
	}
	var value, term big.Float
	switch {
	case c.exact == nil:
		value.SetPrec(voronoiCostPrecision).SetFloat64(c.rounded)
		return value.Add(&value, term.SetFloat64(c.tail)).Cmp(other.exact)
	case other.exact == nil:
		value.SetPrec(voronoiCostPrecision).SetFloat64(other.rounded)
		return c.exact.Cmp(value.Add(&value, term.SetFloat64(other.tail)))
	default:
		return c.exact.Cmp(other.exact)
	}
}

func (c voronoiCost) overflows() bool {
	return c.rounded >= math.MaxFloat64 && c.compare(voronoiCost{rounded: math.MaxFloat64}) > 0
}
