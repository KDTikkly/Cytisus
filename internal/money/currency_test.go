package money

import (
	"encoding/json"
	"testing"
)

func TestCurrencyValidation(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"USD", "USDC", "BTC", "ASSET123"} {
		currency, err := ParseCurrency(value)
		if err != nil {
			t.Fatalf("ParseCurrency(%q): %v", value, err)
		}
		if string(currency) != value {
			t.Fatalf("expected %s, got %s", value, currency)
		}
	}
	for _, value := range []string{"usd", "1USD", "US", "USD-TEST", "TOO_LONG_ASSET"} {
		if _, err := ParseCurrency(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestAmountJSONUsesDecimalString(t *testing.T) {
	t.Parallel()
	amount, err := NewAmount(MustParse("25.50"), MustParseCurrency("USD"))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(amount)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"value":"25.5","currency":"USD"}` {
		t.Fatalf("unexpected JSON: %s", encoded)
	}
}
