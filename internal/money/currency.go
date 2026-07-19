package money

import (
	"bytes"
	"encoding/json"
	"errors"
)

var ErrInvalidCurrency = errors.New("invalid currency")

type Currency string

func ParseCurrency(value string) (Currency, error) {
	if len(value) < 3 || len(value) > 12 {
		return "", ErrInvalidCurrency
	}
	for index, character := range value {
		isLetter := character >= 'A' && character <= 'Z'
		isDigit := character >= '0' && character <= '9'
		if !isLetter && (index == 0 || !isDigit) {
			return "", ErrInvalidCurrency
		}
	}
	return Currency(value), nil
}

func MustParseCurrency(value string) Currency {
	currency, err := ParseCurrency(value)
	if err != nil {
		panic(err)
	}
	return currency
}

func (currency Currency) Validate() error {
	_, err := ParseCurrency(string(currency))
	return err
}

func (currency Currency) MarshalJSON() ([]byte, error) {
	if err := currency.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(string(currency))
}

func (currency *Currency) UnmarshalJSON(data []byte) error {
	if currency == nil || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return ErrInvalidCurrency
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return ErrInvalidCurrency
	}
	parsed, err := ParseCurrency(value)
	if err != nil {
		return err
	}
	*currency = parsed
	return nil
}

type Amount struct {
	Value    Decimal  `json:"value"`
	Currency Currency `json:"currency"`
}

func NewAmount(value Decimal, currency Currency) (Amount, error) {
	if err := currency.Validate(); err != nil {
		return Amount{}, err
	}
	return Amount{Value: value, Currency: currency}, nil
}
