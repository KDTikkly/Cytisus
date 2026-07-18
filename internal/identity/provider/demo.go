package provider

const SimulatedClassification = "SIMULATED"

type DemoIdentity struct {
	Subject        string `json:"subject"`
	Email          string `json:"email"`
	DisplayName    string `json:"display_name"`
	Classification string `json:"classification"`
}

func SyntheticDemoIdentity() DemoIdentity {
	return DemoIdentity{
		Subject:        "demo_identity_0001",
		Email:          "demo.user@example.invalid",
		DisplayName:    "Demo User",
		Classification: SimulatedClassification,
	}
}
