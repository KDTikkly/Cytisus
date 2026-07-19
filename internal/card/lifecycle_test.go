package card

import (
	"errors"
	"testing"

	"github.com/KDTikkly/Cytisus/internal/card/store"
)

func TestVirtualCardLifecycle(t *testing.T) {
	active, pinSet, err := nextUserCardState(store.CardCard{CardType: "VIRTUAL", Status: "CREATED"}, "ACTIVATE")
	if err != nil || active != "ACTIVE" || pinSet {
		t.Fatalf("unexpected activation transition: status=%s pin=%t err=%v", active, pinSet, err)
	}
	frozen, _, err := nextUserCardState(store.CardCard{CardType: "VIRTUAL", Status: active}, "FREEZE")
	if err != nil || frozen != "FROZEN" {
		t.Fatalf("unexpected freeze transition: status=%s err=%v", frozen, err)
	}
	if _, _, err := nextUserCardState(store.CardCard{CardType: "VIRTUAL", Status: frozen}, "REPORT_LOST"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("virtual card must not enter a physical lost-card state: %v", err)
	}
}

func TestPhysicalAndAdminLifecycleRules(t *testing.T) {
	valid := [][2]string{
		{"APPLICATION_SUBMITTED", "UNDER_REVIEW"},
		{"UNDER_REVIEW", "APPROVED"},
		{"APPROVED", "MANUFACTURING"},
		{"MANUFACTURING", "SHIPPED"},
		{"SHIPPED", "DELIVERED"},
	}
	for _, transition := range valid {
		if !physicalTransitionAllowed(transition[0], transition[1]) {
			t.Fatalf("expected physical transition %s -> %s", transition[0], transition[1])
		}
	}
	if physicalTransitionAllowed("APPLICATION_SUBMITTED", "DELIVERED") {
		t.Fatal("physical lifecycle must not skip review and fulfillment states")
	}
	if !authorizedCardAdmin(AdminActor{ID: "operations-1", Role: "OPERATIONS"}) ||
		!authorizedDisputeAdmin(AdminActor{ID: "operations-1", Role: "OPERATIONS"}) ||
		authorizedDisputeAdmin(AdminActor{ID: "support-1", Role: "SUPPORT"}) {
		t.Fatal("Card admin role boundaries are inconsistent")
	}
}
