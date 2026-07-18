package provider

import (
	"strings"
	"testing"
)

func TestDemoIdentityIsSynthetic(t *testing.T) {
	t.Parallel()

	identity := SyntheticDemoIdentity()
	if identity.Classification != SimulatedClassification {
		t.Fatalf("expected %q classification, got %q", SimulatedClassification, identity.Classification)
	}
	if !strings.HasSuffix(identity.Email, ".invalid") {
		t.Fatalf("demo email must use the reserved .invalid domain: %q", identity.Email)
	}
}
