package main

import "testing"

func TestElapsedPercentilesNearestRank(t *testing.T) {
	rows := []resultRecord{
		{ElapsedMS: 100},
		{ElapsedMS: 10},
		{ElapsedMS: 50},
		{ElapsedMS: 20},
		{ElapsedMS: 30},
	}
	p50, p95 := elapsedPercentiles(rows)
	if p50 != 30 {
		t.Fatalf("p50=%d want 30", p50)
	}
	if p95 != 100 {
		t.Fatalf("p95=%d want 100", p95)
	}
}

func TestElapsedPercentilesEmpty(t *testing.T) {
	p50, p95 := elapsedPercentiles(nil)
	if p50 != 0 || p95 != 0 {
		t.Fatalf("p50=%d p95=%d want 0,0", p50, p95)
	}
}
