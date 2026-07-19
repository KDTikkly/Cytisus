package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	financialGoRoots   = []string{"internal/money", "internal/ledger", "internal/outbox", "internal/securities", "internal/banking", "internal/crypto"}
	financialSQLRoot   = "db"
	forbiddenSQLType   = regexp.MustCompile(`(?i)\b(?:real|float(?:4|8)?|double\s+precision)\b`)
	directBalanceWrite = regexp.MustCompile(`(?i)\b(?:update|insert\s+into|delete\s+from)\s+(?:ledger\.)?account_balances\b`)
)

func main() {
	if err := checkFinancialTypes(financialGoRoots, financialSQLRoot); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func checkFinancialTypes(goRoots []string, sqlRoot string) error {
	var violations []string
	for _, root := range goRoots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			found, err := goFileUsesFloat(path)
			if err != nil {
				return err
			}
			if found {
				violations = append(violations, path+": financial Go code uses a floating-point type")
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	err := filepath.WalkDir(sqlRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(contents)
		if forbiddenSQLType.MatchString(text) {
			violations = append(violations, path+": financial SQL uses a floating-point type")
		}
		if directBalanceWrite.MatchString(text) {
			violations = append(violations, path+": SQL attempts to write the derived account balance projection")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(violations) == 0 {
		return nil
	}
	sort.Strings(violations)
	return fmt.Errorf("financial safety check failed:\n%s", strings.Join(violations, "\n"))
}

func goFileUsesFloat(path string) (bool, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && (identifier.Name == "float32" || identifier.Name == "float64") {
			found = true
			return false
		}
		return !found
	})
	return found, nil
}
