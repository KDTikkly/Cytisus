package money

import (
	"errors"
	"fmt"
	"math/big"
)

type RoundingMode string

const (
	RoundTowardZero   RoundingMode = "TOWARD_ZERO"
	RoundAwayFromZero RoundingMode = "AWAY_FROM_ZERO"
	RoundHalfEven     RoundingMode = "HALF_EVEN"
	RoundHalfAway     RoundingMode = "HALF_AWAY_FROM_ZERO"
)

var ErrInvalidRoundingPolicy = errors.New("invalid rounding policy")

type RoundingPolicy struct {
	Version       string
	DecimalPlaces uint8
	Mode          RoundingMode
}

func (policy RoundingPolicy) Validate() error {
	if policy.Version == "" || policy.DecimalPlaces > Scale {
		return ErrInvalidRoundingPolicy
	}
	switch policy.Mode {
	case RoundTowardZero, RoundAwayFromZero, RoundHalfEven, RoundHalfAway:
		return nil
	default:
		return ErrInvalidRoundingPolicy
	}
}

func (d Decimal) Round(policy RoundingPolicy) (Decimal, error) {
	if err := policy.Validate(); err != nil {
		return Decimal{}, err
	}

	droppedPlaces := Scale - int(policy.DecimalPlaces)
	if droppedPlaces == 0 {
		return decimalFromCoefficient(d.value())
	}
	divisor := powerOfTen(droppedPlaces)
	absolute := d.value()
	negative := absolute.Sign() < 0
	absolute.Abs(absolute)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(absolute, divisor, remainder)

	if shouldRoundUp(quotient, remainder, divisor, policy.Mode) {
		quotient.Add(quotient, big.NewInt(1))
	}
	coefficient := quotient.Mul(quotient, divisor)
	if negative {
		coefficient.Neg(coefficient)
	}
	rounded, err := decimalFromCoefficient(coefficient)
	if err != nil {
		return Decimal{}, fmt.Errorf("round with policy %s: %w", policy.Version, err)
	}
	return rounded, nil
}

func shouldRoundUp(quotient, remainder, divisor *big.Int, mode RoundingMode) bool {
	if remainder.Sign() == 0 {
		return false
	}
	switch mode {
	case RoundTowardZero:
		return false
	case RoundAwayFromZero:
		return true
	case RoundHalfAway:
		return new(big.Int).Mul(remainder, big.NewInt(2)).Cmp(divisor) >= 0
	case RoundHalfEven:
		comparison := new(big.Int).Mul(remainder, big.NewInt(2)).Cmp(divisor)
		return comparison > 0 || (comparison == 0 && quotient.Bit(0) == 1)
	default:
		return false
	}
}
