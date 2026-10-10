package main

import "testing"

func TestCountAutoOrganizeEligibleUsesRowPredicate(t *testing.T) {
	rows := []resultRecord{
		{Status: "confirmed", CatalogID: "ABC-001", AutoOrganizeEligible: true},
		{Status: "confirmed", CatalogID: "ABC-002", AutoOrganizeEligible: false},
		{Status: "review", CatalogID: "ABC-003", AutoOrganizeEligible: false},
	}
	if got := countAutoOrganizeEligible(rows); got != 1 {
		t.Fatalf("countAutoOrganizeEligible=%d, want 1", got)
	}
}
