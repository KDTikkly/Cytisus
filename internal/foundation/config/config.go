package config

import (
	"errors"
	"os"
	"strings"
)

type Environment string

const (
	EnvironmentLocal      Environment = "local"
	EnvironmentTest       Environment = "test"
	EnvironmentStaging    Environment = "staging"
	EnvironmentProduction Environment = "production"
)

type Config struct {
	Environment      Environment
	APIAddress       string
	DatabaseURL      string
	WebOrigin        string
	AdminWebOrigin   string
	SimulatorAddress string
	SimulatorEnabled bool
}

func (c Config) Validate() error {
	switch c.Environment {
	case EnvironmentLocal, EnvironmentTest, EnvironmentStaging, EnvironmentProduction:
		return nil
	default:
		return errors.New("CYTISUS_ENV must be local, test, staging, or production")
	}
}

func Load() Config {
	return Config{
		Environment:      Environment(strings.ToLower(valueOrDefault("CYTISUS_ENV", string(EnvironmentLocal)))),
		APIAddress:       valueOrDefault("CYTISUS_API_ADDR", ":8080"),
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		WebOrigin:        valueOrDefault("CYTISUS_WEB_ORIGIN", "http://localhost:3000"),
		AdminWebOrigin:   valueOrDefault("CYTISUS_ADMIN_WEB_ORIGIN", "http://localhost:3001"),
		SimulatorAddress: valueOrDefault("CYTISUS_SIMULATOR_ADDR", ":8090"),
		SimulatorEnabled: boolOrDefault("CYTISUS_SIMULATOR_ENABLED", true),
	}
}

func (c Config) ValidateSimulator() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Environment == EnvironmentProduction && c.SimulatorEnabled {
		return errors.New("simulators are disabled in production")
	}
	if !c.SimulatorEnabled {
		return errors.New("simulator is disabled by configuration")
	}
	return nil
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func boolOrDefault(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return fallback
	}
}
