package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if err := check("db/migrations"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check(directory string) error {
	up, err := filepath.Glob(filepath.Join(directory, "*.up.sql"))
	if err != nil {
		return err
	}
	down, err := filepath.Glob(filepath.Join(directory, "*.down.sql"))
	if err != nil {
		return err
	}
	if len(up) == 0 {
		return fmt.Errorf("no up migrations found")
	}

	versions := make(map[string]int, len(up)+len(down))
	for _, path := range append(up, down...) {
		name := filepath.Base(path)
		version := strings.SplitN(name, ".", 2)[0]
		versions[version]++
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.TrimSpace(string(contents)) == "" {
			return fmt.Errorf("migration %s is empty", name)
		}
	}

	var invalid []string
	for version, count := range versions {
		if count != 2 {
			invalid = append(invalid, version)
		}
	}
	sort.Strings(invalid)
	if len(invalid) > 0 {
		return fmt.Errorf("migrations require one up/down pair: %s", strings.Join(invalid, ", "))
	}
	return nil
}
