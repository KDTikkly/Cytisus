package money

import (
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestParseAndStringRoundTrip(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"0":                    "0",
		"0.000000000000000001": "0.000000000000000001",
		"1.230000000000000000": "1.23",
		"-42.125":              "-42.125",
		"99999999999999999999.999999999999999999":   "99999999999999999999.999999999999999999",
		"000000000000000000000000000000000000000.1": "0.1",
	}
	for input, expected := range tests {
		input, expected := input, expected
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			value, err := Parse(input)
			if err != nil {
				t.Fatal(err)
			}
			if value.String() != expected {
				t.Fatalf("expected %s, got %s", expected, value.String())
			}
		})
	}
}

func TestParseRejectsInvalidAndOutOfRangeValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		err   error
	}{
		{"", ErrInvalidDecimal},
		{" 1", ErrInvalidDecimal},
		{"+1", ErrInvalidDecimal},
		{"1.", ErrInvalidDecimal},
		{".1", ErrInvalidDecimal},
		{"1e2", ErrInvalidDecimal},
		{"1.0000000000000000001", ErrDecimalScale},
		{"100000000000000000000", ErrDecimalPrecision},
	}
	for _, test := range tests {
		_, err := Parse(test.value)
		if !errors.Is(err, test.err) {
			t.Fatalf("Parse(%q): expected %v, got %v", test.value, test.err, err)
		}
	}
}

func TestArithmeticIsExact(t *testing.T) {
	t.Parallel()
	sum, err := MustParse("0.1").Add(MustParse("0.2"))
	if err != nil {
		t.Fatal(err)
	}
	if sum.String() != "0.3" {
		t.Fatalf("expected 0.3, got %s", sum)
	}
	difference, err := sum.Sub(MustParse("1.05"))
	if err != nil {
		t.Fatal(err)
	}
	if difference.String() != "-0.75" || difference.Abs().String() != "0.75" {
		t.Fatalf("unexpected difference %s", difference)
	}
}

func TestMultiplyAndDivideRequireExplicitRounding(t *testing.T) {
	t.Parallel()
	calculation := RoundingPolicy{Version: "paper-calculation-v1", DecimalPlaces: 18, Mode: RoundHalfEven}
	product, err := MustParse("1.5").Multiply(MustParse("10.25"), calculation)
	if err != nil {
		t.Fatal(err)
	}
	if product.String() != "15.375" {
		t.Fatalf("expected 15.375, got %s", product)
	}
	quotient, err := MustParse("1").Divide(MustParse("3"), calculation)
	if err != nil {
		t.Fatal(err)
	}
	if quotient.String() != "0.333333333333333333" {
		t.Fatalf("unexpected quotient %s", quotient)
	}

	cents := RoundingPolicy{Version: "paper-usd-v1", DecimalPlaces: 2, Mode: RoundHalfEven}
	halfEven, err := MustParse("1.005").Multiply(MustParse("1"), cents)
	if err != nil {
		t.Fatal(err)
	}
	if halfEven.String() != "1" {
		t.Fatalf("expected half-even 1.00, got %s", halfEven)
	}
	if _, err := MustParse("1").Divide(Zero(), calculation); !errors.Is(err, ErrDivideByZero) {
		t.Fatalf("expected divide-by-zero error, got %v", err)
	}
}

func TestRoundingRequiresExplicitVersionedPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value    string
		mode     RoundingMode
		expected string
	}{
		{"1.245", RoundHalfEven, "1.24"},
		{"1.255", RoundHalfEven, "1.26"},
		{"-1.255", RoundHalfEven, "-1.26"},
		{"1.241", RoundTowardZero, "1.24"},
		{"-1.241", RoundAwayFromZero, "-1.25"},
		{"1.245", RoundHalfAway, "1.25"},
	}
	for _, test := range tests {
		rounded, err := MustParse(test.value).Round(RoundingPolicy{
			Version:       "usd-display-v1",
			DecimalPlaces: 2,
			Mode:          test.mode,
		})
		if err != nil {
			t.Fatal(err)
		}
		if rounded.String() != test.expected {
			t.Fatalf("%s with %s: expected %s, got %s", test.value, test.mode, test.expected, rounded)
		}
	}
	if _, err := MustParse("1.23").Round(RoundingPolicy{DecimalPlaces: 2, Mode: RoundHalfEven}); !errors.Is(err, ErrInvalidRoundingPolicy) {
		t.Fatalf("expected invalid unversioned policy, got %v", err)
	}
}

func TestJSONRequiresString(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(MustParse("12.340"))
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `"12.34"` {
		t.Fatalf("unexpected JSON: %s", encoded)
	}
	var decoded Decimal
	if err := json.Unmarshal([]byte(`"12.34"`), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(MustParse("12.34")) {
		t.Fatalf("unexpected decoded value: %s", decoded)
	}
	if err := json.Unmarshal([]byte(`12.34`), &decoded); err == nil {
		t.Fatal("expected JSON number to be rejected")
	}
}

func TestPGXNumericMappingIsExact(t *testing.T) {
	t.Parallel()
	original := MustParse("123.000000000000000001")
	numeric, err := original.NumericValue()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Decimal
	if err := decoded.ScanNumeric(numeric); err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(original) {
		t.Fatalf("expected %s, got %s", original, decoded)
	}
	if err := decoded.ScanNumeric(pgtype.Numeric{Int: big.NewInt(1), Exp: -19, Valid: true}); !errors.Is(err, ErrDecimalScale) {
		t.Fatalf("expected scale error, got %v", err)
	}
	if err := decoded.ScanNumeric(pgtype.Numeric{}); !errors.Is(err, ErrNullDecimal) {
		t.Fatalf("expected null error, got %v", err)
	}
}

func TestProductionMoneyPackageContainsNoFloatingPointTypes(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(contents)
		forbidden := "float" + "32"
		if strings.Contains(text, forbidden) {
			t.Fatalf("%s contains a forbidden floating-point type", path)
		}
		forbidden = "float" + "64"
		if strings.Contains(text, forbidden) {
			t.Fatalf("%s contains a forbidden floating-point type", path)
		}
	}
}
