package config

import "testing"

func TestProductionRejectsSimulator(t *testing.T) {
	t.Parallel()

	cfg := Config{Environment: EnvironmentProduction, SimulatorEnabled: true}
	if err := cfg.ValidateSimulator(); err == nil {
		t.Fatal("expected production simulator validation to fail")
	}
}

func TestLocalAllowsSimulator(t *testing.T) {
	t.Parallel()

	cfg := Config{Environment: EnvironmentLocal, SimulatorEnabled: true}
	if err := cfg.ValidateSimulator(); err != nil {
		t.Fatalf("expected local simulator validation to pass: %v", err)
	}
}

func TestUnknownEnvironmentIsRejected(t *testing.T) {
	t.Parallel()

	cfg := Config{Environment: Environment("unknown")}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected unknown environment validation to fail")
	}
}
