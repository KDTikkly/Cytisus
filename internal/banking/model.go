package banking

import (
	"errors"
	"time"

	"github.com/KDTikkly/Cytisus/internal/money"
)

const PolicyVersion = "banking-compliance-v1"

type CustomerSession struct {
	PaperAccountID      string
	CustomerReference   string
	CashLedgerAccountID string
}

type BankAccount struct {
	ID                       string
	Provider                 string
	ExternalAccountReference string
	RailSupport              string
	OwnerRelation            string
	OwnershipStatus          string
	Status                   string
	PreferredForWithdrawal   bool
	SuccessfullyFundedAt     *time.Time
	CoolingUntil             *time.Time
	CreatedAt                time.Time
}

type FundingTransfer struct {
	ID                 string
	BankAccountID      string
	Rail               string
	Amount             money.Decimal
	Status             string
	ProviderTransferID string
	ReasonCode         string
	Pending            bool
	Settled            bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Replayed           bool
}

type Withdrawal struct {
	ID                 string
	BankAccountID      string
	Amount             money.Decimal
	Status             string
	ReasonCode         string
	NextAction         string
	ComplianceCaseID   string
	CoolingUntil       *time.Time
	ProviderTransferID string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Replayed           bool
}

type ComplianceCase struct {
	ID            string
	CustomerRef   string
	CaseType      string
	ResourceType  string
	ResourceID    string
	Status        string
	ReasonCode    string
	NextAction    string
	PolicyVersion string
	Version       int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ReviewProposal struct {
	ID                string
	CaseID            string
	Action            string
	MakerID           string
	MakerRole         string
	ReasonCode        string
	TicketReference   string
	EvidenceReference string
	Status            string
	CheckerID         string
	CheckerRole       string
	DecisionReason    string
	CreatedAt         time.Time
	DecidedAt         *time.Time
}

type ReconciliationRun struct {
	ID             string
	Status         string
	LedgerAmount   money.Decimal
	ProviderAmount money.Decimal
	Difference     money.Decimal
	CaseID         string
	StartedAt      time.Time
	CompletedAt    time.Time
}

type ReconcileCustomerCommand struct {
	Actor             AdminActor
	CustomerReference string
	ProviderAmount    *money.Decimal
}

type LinkBankAccountCommand struct {
	AccessToken              string
	ExternalAccountReference string
	RailSupport              string
	OwnerRelation            string
	RiskClass                string
}

type InitiateFundingCommand struct {
	AccessToken    string
	IdempotencyKey string
	BankAccountID  string
	Rail           string
	Amount         money.Decimal
}

type RequestWithdrawalCommand struct {
	AccessToken    string
	IdempotencyKey string
	BankAccountID  string
	Amount         money.Decimal
}

type AdminActor struct {
	ID   string
	Role string
}

type ProposeReviewCommand struct {
	Actor             AdminActor
	CaseID            string
	Action            string
	ReasonCode        string
	TicketReference   string
	EvidenceReference string
}

type DecideReviewCommand struct {
	Actor          AdminActor
	ProposalID     string
	Approve        bool
	DecisionReason string
}

var (
	ErrUnauthorized             = errors.New("banking session is unauthorized")
	ErrInvalidCommand           = errors.New("invalid banking command")
	ErrIdempotencyConflict      = errors.New("idempotency key was reused with different banking request data")
	ErrProviderEventConflict    = errors.New("provider event was reused with different banking callback data")
	ErrBankAccountNotFound      = errors.New("bank account not found")
	ErrTransferNotFound         = errors.New("bank transfer not found")
	ErrWithdrawalNotFound       = errors.New("withdrawal not found")
	ErrCaseNotFound             = errors.New("compliance case not found")
	ErrReviewNotFound           = errors.New("review proposal not found")
	ErrThirdPartyDisabled       = errors.New("third-party bank accounts are permanently disabled for MVP withdrawals")
	ErrOwnershipVerification    = errors.New("bank ownership verification is required")
	ErrCoolingOff               = errors.New("bank withdrawal cooling period is active")
	ErrEnhancedReview           = errors.New("enhanced bank withdrawal review is required")
	ErrInsufficientWithdrawable = errors.New("insufficient settled and withdrawable USD")
	ErrInvalidState             = errors.New("banking state does not allow this action")
	ErrMakerCheckerConflict     = errors.New("maker and checker must be different administrators")
	ErrAdminUnauthorized        = errors.New("admin role is not authorized for this action")
	ErrReconciliationDifference = errors.New("bank reconciliation difference requires review")
)
