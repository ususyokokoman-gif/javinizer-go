//go:build windows

package main

import "testing"

func TestWindowsDPAPIRoundTrip(t *testing.T) {
	const value = "round-trip-value"
	encrypted, err := protectSecret(value)
	if err != nil {
		t.Fatalf("protectSecret: %v", err)
	}
	if encrypted == "" || encrypted == value {
		t.Fatalf("value was not protected")
	}
	plain, err := unprotectSecret(encrypted)
	if err != nil {
		t.Fatalf("unprotectSecret: %v", err)
	}
	if plain != value {
		t.Fatalf("round trip mismatch")
	}
}
