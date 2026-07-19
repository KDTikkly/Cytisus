package banking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	bankstore "github.com/KDTikkly/Cytisus/internal/banking/store"
	compliancestore "github.com/KDTikkly/Cytisus/internal/compliance/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/jackc/pgx/v5"
)

func createCase(
	ctx context.Context,
	tx pgx.Tx,
	customerReference, caseType, resourceType, resourceID, reasonCode, nextAction, status string,
) (compliancestore.ComplianceCase, error) {
	identifier, err := newUUID()
	if err != nil {
		return compliancestore.ComplianceCase{}, err
	}
	created, err := compliancestore.New(tx).CreateCase(ctx, compliancestore.CreateCaseParams{
		ID:                identifier,
		CustomerReference: customerReference,
		CaseType:          caseType,
		ResourceType:      resourceType,
		ResourceID:        resourceID,
		Status:            status,
		ReasonCode:        reasonCode,
		NextAction:        nextAction,
		PolicyVersion:     PolicyVersion,
	})
	if err != nil {
		return compliancestore.ComplianceCase{}, fmt.Errorf("create compliance case: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"reason_code": reasonCode, "status": status})
	if _, err := compliancestore.New(tx).InsertCaseEvent(ctx, compliancestore.InsertCaseEventParams{
		CaseID:     identifier,
		ToStatus:   status,
		ActorType:  "SYSTEM",
		ActorID:    "banking-compliance-policy",
		ReasonCode: reasonCode,
		Metadata:   metadata,
	}); err != nil {
		return compliancestore.ComplianceCase{}, fmt.Errorf("insert compliance case event: %w", err)
	}
	if err := recordMutation(ctx, tx, mutation{
		Action:        "compliance.case.created",
		ResourceType:  "compliance.case",
		ResourceID:    identifier.String(),
		ActorType:     "SYSTEM",
		ActorID:       "banking-compliance-policy",
		Metadata:      metadata,
		AggregateType: "compliance.case",
		AggregateID:   identifier.String(),
		EventType:     "compliance.case.created",
		Payload:       metadata,
	}); err != nil {
		return compliancestore.ComplianceCase{}, err
	}
	return created, nil
}

func (service *Service) ListCases(ctx context.Context, actor AdminActor, status string, pageSize int32) ([]ComplianceCase, error) {
	if !adminReadRoleAllowed(actor.Role) || actor.ID == "" {
		return nil, ErrAdminUnauthorized
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := compliancestore.New(service.database).ListCases(ctx, compliancestore.ListCasesParams{
		StatusFilter: strings.ToUpper(strings.TrimSpace(status)),
		PageSize:     pageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list compliance cases: %w", err)
	}
	result := make([]ComplianceCase, 0, len(rows))
	for _, row := range rows {
		result = append(result, caseFromStore(row))
	}
	return result, nil
}

func (service *Service) ProposeReview(ctx context.Context, command ProposeReviewCommand) (ReviewProposal, error) {
	if !adminReviewRoleAllowed(command.Actor.Role) || command.Actor.ID == "" {
		return ReviewProposal{}, ErrAdminUnauthorized
	}
	command.Action = strings.ToUpper(strings.TrimSpace(command.Action))
	if command.Action != "APPROVE_WITHDRAWAL" && command.Action != "OVERRIDE_COOLING" && command.Action != "REJECT_WITHDRAWAL" ||
		command.ReasonCode == "" || command.TicketReference == "" || command.EvidenceReference == "" {
		return ReviewProposal{}, ErrInvalidCommand
	}
	caseID, err := parseUUID(command.CaseID)
	if err != nil {
		return ReviewProposal{}, ErrCaseNotFound
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ReviewProposal{}, fmt.Errorf("begin review proposal: %w", err)
	}
	defer tx.Rollback(ctx)
	caseQueries := compliancestore.New(tx)
	complianceCase, err := caseQueries.GetCaseForUpdate(ctx, caseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewProposal{}, ErrCaseNotFound
	}
	if err != nil {
		return ReviewProposal{}, fmt.Errorf("lock compliance case: %w", err)
	}
	if complianceCase.Status == "OPEN" {
		previous := complianceCase.Status
		complianceCase, err = caseQueries.TransitionCase(ctx, compliancestore.TransitionCaseParams{
			Status: "IN_REVIEW", ReasonCode: "ADMIN_REVIEW_STARTED", NextAction: "A second authorized administrator must independently decide the proposal.", ID: caseID,
		})
		if err != nil {
			return ReviewProposal{}, fmt.Errorf("start compliance review: %w", err)
		}
		if _, err := caseQueries.InsertCaseEvent(ctx, compliancestore.InsertCaseEventParams{
			CaseID: caseID, FromStatus: textValue(previous), ToStatus: complianceCase.Status,
			ActorType: "ADMIN", ActorID: command.Actor.ID, ReasonCode: "ADMIN_REVIEW_STARTED", Metadata: []byte(`{"maker_checker":true}`),
		}); err != nil {
			return ReviewProposal{}, fmt.Errorf("record review transition: %w", err)
		}
	}
	if complianceCase.Status != "IN_REVIEW" {
		return ReviewProposal{}, ErrInvalidState
	}
	proposalID, err := newUUID()
	if err != nil {
		return ReviewProposal{}, err
	}
	proposal, err := caseQueries.CreateReviewProposal(ctx, compliancestore.CreateReviewProposalParams{
		ID: proposalID, CaseID: caseID, Action: command.Action, MakerID: command.Actor.ID,
		MakerRole: command.Actor.Role, ReasonCode: command.ReasonCode, TicketReference: command.TicketReference,
		EvidenceReference: command.EvidenceReference,
	})
	if err != nil {
		return ReviewProposal{}, fmt.Errorf("create review proposal: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"action": command.Action, "reason_code": command.ReasonCode, "ticket_reference": command.TicketReference})
	if err := recordMutation(ctx, tx, mutation{
		Action: "compliance.review.proposed", ResourceType: "compliance.review", ResourceID: proposalID.String(),
		ActorType: "ADMIN", ActorID: command.Actor.ID, Metadata: metadata,
		AggregateType: "compliance.review", AggregateID: proposalID.String(), EventType: "compliance.review.proposed", Payload: metadata,
	}); err != nil {
		return ReviewProposal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReviewProposal{}, fmt.Errorf("commit review proposal: %w", err)
	}
	return proposalFromStore(proposal), nil
}

func (service *Service) DecideReview(ctx context.Context, command DecideReviewCommand) (ReviewProposal, error) {
	if !adminReviewRoleAllowed(command.Actor.Role) || command.Actor.ID == "" || command.DecisionReason == "" {
		return ReviewProposal{}, ErrAdminUnauthorized
	}
	proposalID, err := parseUUID(command.ProposalID)
	if err != nil {
		return ReviewProposal{}, ErrReviewNotFound
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ReviewProposal{}, fmt.Errorf("begin review decision: %w", err)
	}
	defer tx.Rollback(ctx)
	caseQueries := compliancestore.New(tx)
	proposal, err := caseQueries.GetReviewProposalForUpdate(ctx, proposalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReviewProposal{}, ErrReviewNotFound
	}
	if err != nil {
		return ReviewProposal{}, fmt.Errorf("lock review proposal: %w", err)
	}
	if proposal.Status != "PENDING" {
		return ReviewProposal{}, ErrInvalidState
	}
	if proposal.MakerID == command.Actor.ID {
		return ReviewProposal{}, ErrMakerCheckerConflict
	}
	complianceCase, err := caseQueries.GetCaseForUpdate(ctx, proposal.CaseID)
	if err != nil {
		return ReviewProposal{}, fmt.Errorf("lock proposal case: %w", err)
	}
	decisionStatus := "REJECTED"
	if command.Approve {
		decisionStatus = "APPROVED"
	}
	decided, err := caseQueries.DecideReviewProposal(ctx, compliancestore.DecideReviewProposalParams{
		Status: decisionStatus, CheckerID: textValue(command.Actor.ID), CheckerRole: textValue(command.Actor.Role),
		DecisionReason: textValue(command.DecisionReason), ID: proposalID,
	})
	if err != nil {
		return ReviewProposal{}, fmt.Errorf("decide review proposal: %w", err)
	}
	if command.Approve {
		if err := service.applyApprovedReview(ctx, tx, complianceCase, proposal, command.Actor); err != nil {
			return ReviewProposal{}, err
		}
	}
	metadata, _ := json.Marshal(map[string]string{"action": proposal.Action, "decision": decisionStatus, "reason": command.DecisionReason})
	if err := recordMutation(ctx, tx, mutation{
		Action: "compliance.review." + strings.ToLower(decisionStatus), ResourceType: "compliance.review", ResourceID: proposalID.String(),
		ActorType: "ADMIN", ActorID: command.Actor.ID, Metadata: metadata,
		AggregateType: "compliance.review", AggregateID: proposalID.String(), EventType: "compliance.review." + strings.ToLower(decisionStatus),
		EventVersion: 2, Payload: metadata,
	}); err != nil {
		return ReviewProposal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ReviewProposal{}, fmt.Errorf("commit review decision: %w", err)
	}
	return proposalFromStore(decided), nil
}

func (service *Service) applyApprovedReview(ctx context.Context, tx pgx.Tx, complianceCase compliancestore.ComplianceCase, proposal compliancestore.ComplianceReviewProposal, actor AdminActor) error {
	if complianceCase.ResourceType != "bank.withdrawal" {
		return ErrInvalidState
	}
	bankQueries := bankstore.New(tx)
	withdrawal, err := bankQueries.GetWithdrawalByCaseForUpdate(ctx, complianceCase.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWithdrawalNotFound
	}
	if err != nil {
		return fmt.Errorf("lock reviewed withdrawal: %w", err)
	}
	caseStatus := "APPROVED"
	caseReason := "WITHDRAWAL_APPROVED"
	nextAction := "The withdrawal is approved and will be submitted to the bank simulator."
	withdrawalStatus := "APPROVED"
	if proposal.Action == "REJECT_WITHDRAWAL" {
		caseStatus = "REJECTED"
		caseReason = "WITHDRAWAL_REJECTED"
		nextAction = "Review the case response before submitting a new withdrawal."
		withdrawalStatus = "REJECTED"
	} else {
		if proposal.Action == "APPROVE_WITHDRAWAL" && withdrawal.CoolingUntil.Valid && service.now().UTC().Before(withdrawal.CoolingUntil.Time.UTC()) {
			return ErrCoolingOff
		}
		profile, getErr := bankQueries.GetCustomerProfileForUpdate(ctx, withdrawal.CustomerReference)
		if getErr != nil {
			return fmt.Errorf("lock withdrawal profile: %w", getErr)
		}
		reservationID, reserveErr := service.reserveWithdrawal(ctx, tx, profile, withdrawal, ledger.Actor{Type: "ADMIN", ID: actor.ID})
		if reserveErr != nil {
			return reserveErr
		}
		withdrawal.ReservationLedgerTransactionID = reservationID
	}
	updatedWithdrawal, err := bankQueries.TransitionWithdrawal(ctx, bankstore.TransitionWithdrawalParams{
		Status: withdrawalStatus, ReasonCode: caseReason, NextAction: nextAction,
		ReservationLedgerTransactionID: withdrawal.ReservationLedgerTransactionID, ID: withdrawal.ID,
	})
	if err != nil {
		return fmt.Errorf("apply reviewed withdrawal: %w", err)
	}
	updatedCase, err := compliancestore.New(tx).TransitionCase(ctx, compliancestore.TransitionCaseParams{
		Status: caseStatus, ReasonCode: caseReason, NextAction: nextAction, ID: complianceCase.ID,
	})
	if err != nil {
		return fmt.Errorf("complete reviewed case: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"proposal_action": proposal.Action, "withdrawal_status": updatedWithdrawal.Status})
	if _, err := compliancestore.New(tx).InsertCaseEvent(ctx, compliancestore.InsertCaseEventParams{
		CaseID: complianceCase.ID, FromStatus: textValue(complianceCase.Status), ToStatus: updatedCase.Status,
		ActorType: "ADMIN", ActorID: actor.ID, ReasonCode: caseReason, Metadata: metadata,
	}); err != nil {
		return fmt.Errorf("record reviewed case decision: %w", err)
	}
	return nil
}

func adminReadRoleAllowed(role string) bool {
	switch role {
	case "COMPLIANCE_ANALYST", "RISK_ANALYST", "OPERATIONS", "ADMIN", "AUDITOR":
		return true
	default:
		return false
	}
}

func adminReviewRoleAllowed(role string) bool {
	switch role {
	case "COMPLIANCE_ANALYST", "RISK_ANALYST", "OPERATIONS", "ADMIN":
		return true
	default:
		return false
	}
}

func caseFromStore(value compliancestore.ComplianceCase) ComplianceCase {
	return ComplianceCase{
		ID: value.ID.String(), CustomerRef: value.CustomerReference, CaseType: value.CaseType,
		ResourceType: value.ResourceType, ResourceID: value.ResourceID, Status: value.Status,
		ReasonCode: value.ReasonCode, NextAction: value.NextAction, PolicyVersion: value.PolicyVersion,
		Version: value.Version, CreatedAt: value.CreatedAt.Time.UTC(), UpdatedAt: value.UpdatedAt.Time.UTC(),
	}
}

func proposalFromStore(value compliancestore.ComplianceReviewProposal) ReviewProposal {
	var decidedAt *time.Time
	if value.DecidedAt.Valid {
		resolved := value.DecidedAt.Time.UTC()
		decidedAt = &resolved
	}
	return ReviewProposal{
		ID: value.ID.String(), CaseID: value.CaseID.String(), Action: value.Action, MakerID: value.MakerID,
		MakerRole: value.MakerRole, ReasonCode: value.ReasonCode, TicketReference: value.TicketReference,
		EvidenceReference: value.EvidenceReference, Status: value.Status, CheckerID: value.CheckerID.String,
		CheckerRole: value.CheckerRole.String, DecisionReason: value.DecisionReason.String,
		CreatedAt: value.CreatedAt.Time.UTC(), DecidedAt: decidedAt,
	}
}
