package card

import (
	"context"
	"encoding/json"
	"fmt"

	compliancestore "github.com/KDTikkly/Cytisus/internal/compliance/store"
	"github.com/jackc/pgx/v5"
)

func createComplianceCase(
	ctx context.Context,
	tx pgx.Tx,
	customerReference, caseType, resourceType, resourceID, reasonCode, nextAction string,
) (compliancestore.ComplianceCase, error) {
	identifier, err := newUUID()
	if err != nil {
		return compliancestore.ComplianceCase{}, err
	}
	created, err := compliancestore.New(tx).CreateCase(ctx, compliancestore.CreateCaseParams{
		ID: identifier, CustomerReference: customerReference, CaseType: caseType,
		ResourceType: resourceType, ResourceID: resourceID, Status: "IN_REVIEW",
		ReasonCode: reasonCode, NextAction: nextAction, PolicyVersion: PolicyVersion,
	})
	if err != nil {
		return compliancestore.ComplianceCase{}, fmt.Errorf("create card compliance case: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"reason_code": reasonCode, "status": created.Status})
	if _, err := compliancestore.New(tx).InsertCaseEvent(ctx, compliancestore.InsertCaseEventParams{
		CaseID: identifier, ToStatus: created.Status, ActorType: "SYSTEM", ActorID: "card-risk-policy",
		ReasonCode: reasonCode, Metadata: metadata,
	}); err != nil {
		return compliancestore.ComplianceCase{}, fmt.Errorf("record card compliance case: %w", err)
	}
	if err := recordMutation(ctx, tx, mutation{
		Action: "compliance.case.created", ResourceType: "compliance.case", ResourceID: identifier.String(),
		ActorType: "SYSTEM", ActorID: "card-risk-policy", CorrelationID: identifier, Metadata: metadata,
		AggregateType: "compliance.case", AggregateID: identifier.String(), EventType: "compliance.case.created", Payload: metadata,
	}); err != nil {
		return compliancestore.ComplianceCase{}, err
	}
	return created, nil
}

func transitionComplianceCase(
	ctx context.Context,
	tx pgx.Tx,
	caseID string,
	actor AdminActor,
	status, reasonCode, nextAction string,
) error {
	identifier, err := parseUUID(caseID)
	if err != nil {
		return fmt.Errorf("parse compliance case: %w", err)
	}
	queries := compliancestore.New(tx)
	current, err := queries.GetCaseForUpdate(ctx, identifier)
	if err != nil {
		return fmt.Errorf("lock compliance case: %w", err)
	}
	updated, err := queries.TransitionCase(ctx, compliancestore.TransitionCaseParams{
		Status: status, ReasonCode: reasonCode, NextAction: nextAction, ID: identifier,
	})
	if err != nil {
		return fmt.Errorf("transition compliance case: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"reason_code": reasonCode, "status": status})
	if _, err := queries.InsertCaseEvent(ctx, compliancestore.InsertCaseEventParams{
		CaseID: identifier, FromStatus: textValue(current.Status), ToStatus: updated.Status,
		ActorType: "ADMIN", ActorID: actor.ID, ReasonCode: reasonCode, Metadata: metadata,
	}); err != nil {
		return fmt.Errorf("record compliance decision: %w", err)
	}
	return nil
}
