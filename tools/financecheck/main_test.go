package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryFinancialCodePasses(t *testing.T) {
	t.Parallel()
	if err := checkFinancialTypes([]string{"../../internal/money", "../../internal/ledger", "../../internal/outbox", "../../internal/securities", "../../internal/banking", "../../internal/crypto"}, "../../db"); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRejectsFloatingPointAndBalanceWrites(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	goRoot := filepath.Join(directory, "go")
	sqlRoot := filepath.Join(directory, "sql")
	if err := os.MkdirAll(goRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sqlRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goRoot, "money.go"), []byte("package fixture\nvar amount float64\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sqlRoot, "ledger.sql"), []byte("UPDATE ledger.account_balances SET debit_balance = 1::REAL;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkFinancialTypes([]string{goRoot}, sqlRoot); err == nil {
		t.Fatal("expected financial safety violations")
	}
}
