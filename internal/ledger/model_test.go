package ledger

import (
	"errors"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/money"
)

const (
	accountOne = "00000000-0000-4000-8000-000000000001"
	accountTwo = "00000000-0000-4000-8000-000000000002"
)

func validPosting() PostingCommand {
	return PostingCommand{
		Scope:           "ledger.post",
		IdempotencyKey:  "request-00000001",
		TransactionType: "CASH_TRANSFER",
		PolicyVersion:   "ledger-v1",
		EffectiveAt:     time.Date(2026, time.July, 19, 0, 0, 0, 0, time.UTC),
		Actor:           Actor{Type: "SYSTEM", ID: "integration-test"},
		Entries: []Entry{
			{AccountID: accountOne, Currency: money.MustParseCurrency("USD"), Dimension: DimensionSettled, Direction: Debit, Amount: money.MustParse("10")},
			{AccountID: accountTwo, Currency: money.MustParseCurrency("USD"), Dimension: DimensionSettled, Direction: Credit, Amount: money.MustParse("10")},
		},
	}
}

func TestPostingValidationAcceptsBalancedEntries(t *testing.T) {
	t.Parallel()
	if err := validPosting().validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPostingValidationRejectsImbalanceAndInvalidAmounts(t *testing.T) {
	t.Parallel()
	unbalanced := validPosting()
	unbalanced.Entries[1].Amount = money.MustParse("9.99")
	if !errors.Is(unbalanced.validate(), ErrUnbalancedPosting) {
		t.Fatalf("expected imbalance error, got %v", unbalanced.validate())
	}
	zero := validPosting()
	zero.Entries[0].Amount = money.Zero()
	if !errors.Is(zero.validate(), ErrInvalidCommand) {
		t.Fatalf("expected invalid amount error, got %v", zero.validate())
	}
}

func TestPostingHashIsStableAndCoversFinancialContent(t *testing.T) {
	t.Parallel()
	command := validPosting()
	first, err := hashPostingCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashPostingCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("expected stable hash, got %s and %s", first, second)
	}
	command.Entries[0].Amount = money.MustParse("11")
	changed, err := hashPostingCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("amount change did not change request hash")
	}
}

func TestProviderPayloadParticipatesInRequestHash(t *testing.T) {
	t.Parallel()
	command := validPosting()
	command.ProviderEvent = &ProviderEvent{Provider: "bank-sim", ExternalEventID: "event-1", Payload: []byte(`{"amount":"10"}`)}
	first, err := hashPostingCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	command.ProviderEvent.Payload = []byte(`{"amount":"11"}`)
	second, err := hashPostingCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("provider payload change did not change request hash")
	}
}

func TestUUIDGenerationAndParsing(t *testing.T) {
	t.Parallel()
	identifier, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseUUID(identifier.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed != identifier {
		t.Fatalf("expected %s, got %s", identifier.String(), parsed.String())
	}
}
