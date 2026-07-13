package main

import "testing"

func TestCountsEmpty(t *testing.T) {
	if !countsEmpty(map[string]int{"users": 0, "applications": 0}) {
		t.Fatal("expected zero counts to be empty")
	}
	if countsEmpty(map[string]int{"users": 1, "applications": 0}) {
		t.Fatal("expected non-zero counts to be non-empty")
	}
}

func TestCompareCounts(t *testing.T) {
	if err := compareCounts(map[string]int{"users": 1}, map[string]int{"users": 1}); err != nil {
		t.Fatalf("matching counts returned error: %v", err)
	}
	if err := compareCounts(map[string]int{"users": 1}, map[string]int{"users": 2}); err == nil {
		t.Fatal("expected mismatched counts to fail")
	}
}
