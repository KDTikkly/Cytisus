package main

import "testing"

func TestRepositoryMigrationsHaveReversiblePairs(t *testing.T) {
	t.Parallel()
	if err := check("../../db/migrations"); err != nil {
		t.Fatal(err)
	}
}
