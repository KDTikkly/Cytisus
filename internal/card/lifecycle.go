package card

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	cardprovider "github.com/KDTikkly/Cytisus/internal/card/provider"
	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/notification"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) Profile(ctx context.Context, accessToken string) (Profile, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return Profile{}, err
	}
	rows, err := store.New(service.database).ListCards(ctx, profile.CustomerReference)
	if err != nil {
		return Profile{}, fmt.Errorf("list customer cards: %w", err)
	}
	cards := make([]Card, 0, len(rows))
	for _, row := range rows {
		cards = append(cards, cardFromStore(row, false))
	}
	return Profile{
		CustomerReference: profile.CustomerReference, RepaymentMode: RepaymentMode(profile.RepaymentMode),
		SpendingStatus: profile.SpendingStatus, SpendingStatusReason: profile.SpendingStatusReason.String,
		PolicyVersion: profile.PolicyVersion, Cards: cards,
	}, nil
}

func (service *Service) CreateCard(ctx context.Context, command CreateCardCommand) (Card, error) {
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) ||
		(command.Type != TypeVirtual && command.Type != TypePlastic && command.Type != TypeMetal) {
		return Card{}, ErrInvalidCommand
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Card{}, err
	}
	requestHash, err := hashValue(struct{ Type CardType }{command.Type})
	if err != nil {
		return Card{}, err
	}
	cardID, err := newUUID()
	if err != nil {
		return Card{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Card{}, fmt.Errorf("begin card creation: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	if _, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference); err != nil {
		return Card{}, fmt.Errorf("lock card profile: %w", err)
	}
	inserted, err := queries.InsertCommandRequest(ctx, store.InsertCommandRequestParams{
		CustomerReference: profile.CustomerReference, Scope: "card.create", IdempotencyKey: command.IdempotencyKey,
		RequestHash: requestHash, ResourceID: cardID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingRequest, getErr := queries.GetCommandRequest(ctx, store.GetCommandRequestParams{
			CustomerReference: profile.CustomerReference, Scope: "card.create", IdempotencyKey: command.IdempotencyKey,
		})
		if getErr != nil {
			return Card{}, fmt.Errorf("get card creation replay: %w", getErr)
		}
		if existingRequest.RequestHash != requestHash {
			return Card{}, ErrIdempotencyConflict
		}
		existing, getErr := queries.GetCustomerCard(ctx, store.GetCustomerCardParams{ID: existingRequest.ResourceID, CustomerReference: profile.CustomerReference})
		if getErr != nil {
			return Card{}, fmt.Errorf("get replayed card: %w", getErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return Card{}, err
		}
		return cardFromStore(existing, true), nil
	}
	if err != nil || inserted != cardID {
		return Card{}, fmt.Errorf("reserve card creation: %w", err)
	}
	providerCtx, cancel := service.withProviderTimeout(ctx)
	defer cancel()
	provisioned, err := service.provider.Provision(providerCtx, cardprovider.ProvisionRequest{
		CustomerReference: profile.CustomerReference, CardType: string(command.Type), IdempotencyKey: command.IdempotencyKey,
	})
	if err != nil {
		return Card{}, fmt.Errorf("provision simulated card: %w", err)
	}
	displayName := "Virtual card"
	if command.Type == TypePlastic {
		displayName = "Physical plastic card"
	} else if command.Type == TypeMetal {
		displayName = "Metal card"
	}
	created, err := queries.CreateCard(ctx, store.CreateCardParams{
		ID: cardID, CustomerReference: profile.CustomerReference, CardType: string(command.Type),
		Status: provisioned.InitialStatus, DisplayName: displayName, Last4: provisioned.Last4,
		Provider: service.provider.Name(), ProviderCardReference: provisioned.ProviderCardReference,
		AppleWalletStatus: provisioned.AppleWalletStatus, GoogleWalletStatus: provisioned.GoogleWalletStatus,
		PolicyVersion: profile.PolicyVersion,
	})
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return Card{}, ErrInvalidState
		}
		return Card{}, fmt.Errorf("create card record: %w", err)
	}
	if _, err := queries.InsertLifecycleEvent(ctx, store.InsertLifecycleEventParams{
		CardID: cardID, ToStatus: created.Status, Action: "CREATE", ActorType: "USER", ActorID: profile.CustomerReference,
	}); err != nil {
		return Card{}, fmt.Errorf("record card creation lifecycle: %w", err)
	}
	if err := service.recordCardEvent(ctx, tx, profile.CustomerReference, created, "card.created", "CREATE"); err != nil {
		return Card{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Card{}, fmt.Errorf("commit card creation: %w", err)
	}
	return cardFromStore(created, false), nil
}

func (service *Service) ActOnCard(ctx context.Context, command CardActionCommand) (Card, error) {
	command.Action = strings.ToUpper(strings.TrimSpace(command.Action))
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || command.CardID == "" {
		return Card{}, ErrInvalidCommand
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Card{}, err
	}
	cardID, err := parseUUID(command.CardID)
	if err != nil {
		return Card{}, ErrCardNotFound
	}
	requestHash, err := hashValue(struct{ CardID, Action, Reason string }{command.CardID, command.Action, command.ReasonCode})
	if err != nil {
		return Card{}, err
	}
	resourceID, err := newUUID()
	if err != nil {
		return Card{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Card{}, fmt.Errorf("begin card action: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	if _, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference); err != nil {
		return Card{}, err
	}
	_, err = queries.InsertCommandRequest(ctx, store.InsertCommandRequestParams{
		CustomerReference: profile.CustomerReference, Scope: "card.action", IdempotencyKey: command.IdempotencyKey,
		RequestHash: requestHash, ResourceID: resourceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		replayed, replayErr := queries.GetCommandRequest(ctx, store.GetCommandRequestParams{
			CustomerReference: profile.CustomerReference, Scope: "card.action", IdempotencyKey: command.IdempotencyKey,
		})
		if replayErr != nil || replayed.RequestHash != requestHash {
			return Card{}, ErrIdempotencyConflict
		}
		existing, getErr := queries.GetCustomerCard(ctx, store.GetCustomerCardParams{ID: replayed.ResourceID, CustomerReference: profile.CustomerReference})
		if getErr != nil {
			return Card{}, fmt.Errorf("get replayed card action: %w", getErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return Card{}, err
		}
		return cardFromStore(existing, true), nil
	}
	if err != nil {
		return Card{}, fmt.Errorf("reserve card action: %w", err)
	}
	current, err := queries.GetCustomerCardForUpdate(ctx, store.GetCustomerCardForUpdateParams{ID: cardID, CustomerReference: profile.CustomerReference})
	if errors.Is(err, pgx.ErrNoRows) {
		return Card{}, ErrCardNotFound
	}
	if err != nil {
		return Card{}, err
	}
	if command.Action == "REPLACE" {
		updated, replacement, replaceErr := service.replaceCard(ctx, tx, current, resourceID, command)
		_ = updated
		if replaceErr != nil {
			return Card{}, replaceErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Card{}, err
		}
		return cardFromStore(replacement, false), nil
	}
	nextStatus, pinSet, err := nextUserCardState(current, command.Action)
	if err != nil {
		return Card{}, err
	}
	updated, err := queries.UpdateCardState(ctx, store.UpdateCardStateParams{ID: current.ID, Status: nextStatus, PinSet: pinSet})
	if err != nil {
		return Card{}, fmt.Errorf("update card state: %w", err)
	}
	if _, err := queries.InsertLifecycleEvent(ctx, store.InsertLifecycleEventParams{
		CardID: current.ID, FromStatus: textValue(current.Status), ToStatus: nextStatus, Action: command.Action,
		ActorType: "USER", ActorID: profile.CustomerReference, ReasonCode: textValue(command.ReasonCode),
	}); err != nil {
		return Card{}, err
	}
	if err := service.recordCardEvent(ctx, tx, profile.CustomerReference, updated, "card.state.changed", command.Action); err != nil {
		return Card{}, err
	}
	// The action request points to the returned card, not the generated reservation UUID.
	if err := queries.BindCommandRequestResource(ctx, store.BindCommandRequestResourceParams{
		ResourceID: current.ID, CustomerReference: profile.CustomerReference, Scope: "card.action", IdempotencyKey: command.IdempotencyKey,
	}); err != nil {
		return Card{}, fmt.Errorf("bind card action result: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Card{}, fmt.Errorf("commit card action: %w", err)
	}
	return cardFromStore(updated, false), nil
}

func nextUserCardState(current store.CardCard, action string) (string, bool, error) {
	pinSet := current.PinSet
	switch action {
	case "ACTIVATE":
		if current.Status != "CREATED" && current.Status != "DELIVERED" {
			return "", false, ErrInvalidState
		}
		return "ACTIVE", pinSet, nil
	case "SET_PIN":
		if current.Status != "ACTIVE" || current.PinSet {
			return "", false, ErrInvalidState
		}
		return current.Status, true, nil
	case "CHANGE_PIN":
		if current.Status != "ACTIVE" || !current.PinSet {
			return "", false, ErrInvalidState
		}
		return current.Status, true, nil
	case "FREEZE":
		if current.Status != "ACTIVE" {
			return "", false, ErrInvalidState
		}
		return "FROZEN", pinSet, nil
	case "UNFREEZE":
		if current.Status != "FROZEN" {
			return "", false, ErrInvalidState
		}
		return "ACTIVE", pinSet, nil
	case "REPORT_LOST":
		if current.CardType == "VIRTUAL" || (current.Status != "ACTIVE" && current.Status != "FROZEN") {
			return "", false, ErrInvalidState
		}
		return "LOST", pinSet, nil
	case "REPORT_STOLEN":
		if current.CardType == "VIRTUAL" || (current.Status != "ACTIVE" && current.Status != "FROZEN") {
			return "", false, ErrInvalidState
		}
		return "STOLEN", pinSet, nil
	case "CLOSE":
		if current.Status == "CLOSED" || current.Status == "REPLACED" || current.Status == "CANCELED" {
			return "", false, ErrInvalidState
		}
		return "CLOSED", pinSet, nil
	default:
		return "", false, ErrInvalidCommand
	}
}

func (service *Service) replaceCard(ctx context.Context, tx pgx.Tx, current store.CardCard, replacementID pgtype.UUID, command CardActionCommand) (store.CardCard, store.CardCard, error) {
	if current.Status != "ACTIVE" && current.Status != "FROZEN" && current.Status != "LOST" && current.Status != "STOLEN" {
		return store.CardCard{}, store.CardCard{}, ErrInvalidState
	}
	queries := store.New(tx)
	updated, err := queries.UpdateCardState(ctx, store.UpdateCardStateParams{ID: current.ID, Status: "REPLACED", PinSet: current.PinSet})
	if err != nil {
		return store.CardCard{}, store.CardCard{}, err
	}
	providerCtx, cancel := service.withProviderTimeout(ctx)
	defer cancel()
	provisioned, err := service.provider.Provision(providerCtx, cardprovider.ProvisionRequest{
		CustomerReference: current.CustomerReference, CardType: current.CardType, IdempotencyKey: command.IdempotencyKey + ".replacement",
	})
	if err != nil {
		return store.CardCard{}, store.CardCard{}, err
	}
	replacement, err := queries.CreateCard(ctx, store.CreateCardParams{
		ID: replacementID, CustomerReference: current.CustomerReference, CardType: current.CardType,
		Status: provisioned.InitialStatus, DisplayName: current.DisplayName, Last4: provisioned.Last4,
		Provider: service.provider.Name(), ProviderCardReference: provisioned.ProviderCardReference,
		AppleWalletStatus: provisioned.AppleWalletStatus, GoogleWalletStatus: provisioned.GoogleWalletStatus,
		ReplacementForCardID: current.ID, PolicyVersion: current.PolicyVersion,
	})
	if err != nil {
		return store.CardCard{}, store.CardCard{}, err
	}
	if _, err := queries.InsertLifecycleEvent(ctx, store.InsertLifecycleEventParams{
		CardID: current.ID, FromStatus: textValue(current.Status), ToStatus: "REPLACED", Action: "REPLACE",
		ActorType: "USER", ActorID: current.CustomerReference, ReasonCode: textValue(command.ReasonCode),
	}); err != nil {
		return store.CardCard{}, store.CardCard{}, err
	}
	if _, err := queries.InsertLifecycleEvent(ctx, store.InsertLifecycleEventParams{
		CardID: replacement.ID, ToStatus: replacement.Status, Action: "CREATE_REPLACEMENT",
		ActorType: "SYSTEM", ActorID: "card-replacement",
	}); err != nil {
		return store.CardCard{}, store.CardCard{}, err
	}
	if err := service.recordCardEvent(ctx, tx, current.CustomerReference, replacement, "card.replaced", "REPLACE"); err != nil {
		return store.CardCard{}, store.CardCard{}, err
	}
	return updated, replacement, nil
}

func (service *Service) AdvancePhysicalCard(ctx context.Context, command AdvancePhysicalCardCommand) (Card, error) {
	if !authorizedCardAdmin(command.Actor) || command.CardID == "" {
		return Card{}, ErrAdminUnauthorized
	}
	id, err := parseUUID(command.CardID)
	if err != nil {
		return Card{}, ErrCardNotFound
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Card{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	current, err := queries.GetCardForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Card{}, ErrCardNotFound
	}
	if err != nil || current.CardType == "VIRTUAL" || !physicalTransitionAllowed(current.Status, command.NextStatus) {
		return Card{}, ErrInvalidState
	}
	updated, err := queries.UpdateCardState(ctx, store.UpdateCardStateParams{ID: current.ID, Status: command.NextStatus, PinSet: current.PinSet})
	if err != nil {
		return Card{}, err
	}
	if _, err := queries.InsertLifecycleEvent(ctx, store.InsertLifecycleEventParams{
		CardID: current.ID, FromStatus: textValue(current.Status), ToStatus: command.NextStatus,
		Action: "ADVANCE_PHYSICAL", ActorType: "ADMIN", ActorID: command.Actor.ID, ReasonCode: textValue(command.ReasonCode),
	}); err != nil {
		return Card{}, err
	}
	if err := service.recordCardEvent(ctx, tx, current.CustomerReference, updated, "card.physical.state.changed", "ADVANCE_PHYSICAL"); err != nil {
		return Card{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Card{}, err
	}
	return cardFromStore(updated, false), nil
}

func physicalTransitionAllowed(from, to string) bool {
	transitions := map[string]map[string]bool{
		"APPLICATION_SUBMITTED": {"UNDER_REVIEW": true, "ADDRESS_REVIEW": true, "CANCELED": true},
		"ADDRESS_REVIEW":        {"UNDER_REVIEW": true, "REJECTED": true, "CANCELED": true},
		"UNDER_REVIEW":          {"APPROVED": true, "REJECTED": true, "ADDRESS_REVIEW": true},
		"APPROVED":              {"MANUFACTURING": true, "CANCELED": true},
		"MANUFACTURING":         {"SHIPPED": true, "SHIPMENT_DELAYED": true},
		"SHIPMENT_DELAYED":      {"SHIPPED": true, "CANCELED": true},
		"SHIPPED":               {"DELIVERED": true, "SHIPMENT_DELAYED": true},
		"LOST":                  {"REPLACEMENT_REQUESTED": true, "CLOSED": true},
		"STOLEN":                {"REPLACEMENT_REQUESTED": true, "CLOSED": true},
		"REPLACEMENT_REQUESTED": {"REPLACED": true},
	}
	return transitions[from][to]
}

func authorizedCardAdmin(actor AdminActor) bool {
	if actor.ID == "" {
		return false
	}
	switch actor.Role {
	case "OPERATIONS", "ADMIN", "RISK_ANALYST":
		return true
	default:
		return false
	}
}

func (service *Service) recordCardEvent(ctx context.Context, tx pgx.Tx, customerReference string, value store.CardCard, eventType, action string) error {
	payload, _ := json.Marshal(map[string]string{"card_id": value.ID.String(), "status": value.Status, "card_type": value.CardType})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card." + strings.ToLower(action), ResourceType: "card", ResourceID: value.ID.String(),
		ActorType: "SYSTEM", ActorID: "card-service", CorrelationID: value.ID, Metadata: payload,
		AggregateType: "card", AggregateID: value.ID.String(), EventType: eventType, Payload: payload,
	}); err != nil {
		return err
	}
	_, err := service.notifications.Record(ctx, tx, notification.Event{
		NotificationEventID: eventType + "." + value.ID.String() + "." + fmt.Sprint(value.Version),
		CustomerReference:   customerReference, Category: "CARD", EventType: eventType,
		TitleKey: "notification.card.state.title", BodyKey: "notification.card.state.body",
		ResourceType: "card", ResourceID: value.ID.String(), ActionPath: "/card",
	})
	return err
}

func cardFromStore(value store.CardCard, replayed bool) Card {
	return Card{
		ID: value.ID.String(), Type: CardType(value.CardType), Status: value.Status, DisplayName: value.DisplayName,
		Last4: value.Last4, PINSet: value.PinSet, Provider: value.Provider,
		ProviderCardReference: value.ProviderCardReference, AppleWalletStatus: value.AppleWalletStatus,
		GoogleWalletStatus: value.GoogleWalletStatus, ReplacementForCardID: value.ReplacementForCardID.String(),
		PolicyVersion: value.PolicyVersion, Replayed: replayed, CreatedAt: value.CreatedAt.Time.UTC(), UpdatedAt: value.UpdatedAt.Time.UTC(),
	}
}
