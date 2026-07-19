package money

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	Precision = 38
	Scale     = 18
)

var (
	ErrInvalidDecimal   = errors.New("invalid decimal")
	ErrDecimalPrecision = errors.New("decimal exceeds NUMERIC(38,18) precision")
	ErrDecimalScale     = errors.New("decimal exceeds 18 fractional digits")
	ErrNullDecimal      = errors.New("cannot scan NULL into Decimal")
)

// Decimal stores an exact NUMERIC(38,18) value as an integer scaled by 10^18.
// Its zero value is the numeric value zero. Decimal never performs implicit
// rounding; callers must use Round with an explicit, versioned policy.
type Decimal struct {
	coefficient *big.Int
}

func Parse(value string) (Decimal, error) {
	if value == "" || value != strings.TrimSpace(value) {
		return Decimal{}, ErrInvalidDecimal
	}

	negative := false
	if value[0] == '-' {
		negative = true
		value = value[1:]
	}
	if value == "" || value[0] == '+' {
		return Decimal{}, ErrInvalidDecimal
	}

	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return Decimal{}, ErrInvalidDecimal
	}
	integerPart := parts[0]
	fractionalPart := ""
	if len(parts) == 2 {
		fractionalPart = parts[1]
		if fractionalPart == "" {
			return Decimal{}, ErrInvalidDecimal
		}
	}
	if !isDigits(integerPart) || !isDigits(fractionalPart) {
		return Decimal{}, ErrInvalidDecimal
	}
	if len(fractionalPart) > Scale {
		return Decimal{}, ErrDecimalScale
	}

	digits := integerPart + fractionalPart + strings.Repeat("0", Scale-len(fractionalPart))
	coefficient, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return Decimal{}, ErrInvalidDecimal
	}
	if negative {
		coefficient.Neg(coefficient)
	}
	return decimalFromCoefficient(coefficient)
}

func MustParse(value string) Decimal {
	decimal, err := Parse(value)
	if err != nil {
		panic(err)
	}
	return decimal
}

func Zero() Decimal {
	return Decimal{}
}

func (d Decimal) String() string {
	coefficient := d.value()
	negative := coefficient.Sign() < 0
	coefficient.Abs(coefficient)

	digits := coefficient.String()
	if len(digits) <= Scale {
		digits = strings.Repeat("0", Scale+1-len(digits)) + digits
	}
	integerPart := digits[:len(digits)-Scale]
	fractionalPart := strings.TrimRight(digits[len(digits)-Scale:], "0")

	result := integerPart
	if fractionalPart != "" {
		result += "." + fractionalPart
	}
	if negative && coefficient.Sign() != 0 {
		result = "-" + result
	}
	return result
}

func (d Decimal) Add(other Decimal) (Decimal, error) {
	return decimalFromCoefficient(new(big.Int).Add(d.value(), other.value()))
}

func (d Decimal) Sub(other Decimal) (Decimal, error) {
	return decimalFromCoefficient(new(big.Int).Sub(d.value(), other.value()))
}

func (d Decimal) Negate() Decimal {
	coefficient := d.value()
	coefficient.Neg(coefficient)
	return Decimal{coefficient: coefficient}
}

func (d Decimal) Abs() Decimal {
	coefficient := d.value()
	coefficient.Abs(coefficient)
	return Decimal{coefficient: coefficient}
}

func (d Decimal) Compare(other Decimal) int {
	return d.value().Cmp(other.value())
}

func (d Decimal) Equal(other Decimal) bool {
	return d.Compare(other) == 0
}

func (d Decimal) IsZero() bool {
	return d.value().Sign() == 0
}

func (d Decimal) IsPositive() bool {
	return d.value().Sign() > 0
}

func (d Decimal) Sign() int {
	return d.value().Sign()
}

func (d Decimal) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Decimal) UnmarshalJSON(data []byte) error {
	if d == nil {
		return ErrInvalidDecimal
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return ErrNullDecimal
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decimal must be a JSON string: %w", err)
	}
	parsed, err := Parse(value)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// NumericValue implements pgtype.NumericValuer without converting through a
// floating-point representation.
func (d Decimal) NumericValue() (pgtype.Numeric, error) {
	return pgtype.Numeric{
		Int:   d.value(),
		Exp:   -Scale,
		Valid: true,
	}, nil
}

// ScanNumeric implements pgtype.NumericScanner and rejects any value that
// cannot be represented exactly at scale 18.
func (d *Decimal) ScanNumeric(value pgtype.Numeric) error {
	if d == nil {
		return ErrInvalidDecimal
	}
	if !value.Valid {
		return ErrNullDecimal
	}
	if value.NaN || value.InfinityModifier != pgtype.Finite {
		return ErrInvalidDecimal
	}

	coefficient := new(big.Int)
	if value.Int != nil {
		coefficient.Set(value.Int)
	}
	power := int(value.Exp) + Scale
	if power >= 0 {
		coefficient.Mul(coefficient, powerOfTen(power))
	} else {
		divisor := powerOfTen(-power)
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(coefficient, divisor, remainder)
		if remainder.Sign() != 0 {
			return ErrDecimalScale
		}
		coefficient = quotient
	}

	parsed, err := decimalFromCoefficient(coefficient)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func decimalFromCoefficient(coefficient *big.Int) (Decimal, error) {
	if coefficient == nil || coefficient.Sign() == 0 {
		return Decimal{}, nil
	}
	abs := new(big.Int).Abs(new(big.Int).Set(coefficient))
	if len(abs.String()) > Precision {
		return Decimal{}, ErrDecimalPrecision
	}
	return Decimal{coefficient: new(big.Int).Set(coefficient)}, nil
}

func (d Decimal) value() *big.Int {
	if d.coefficient == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(d.coefficient)
}

func isDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func powerOfTen(exponent int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil)
}
