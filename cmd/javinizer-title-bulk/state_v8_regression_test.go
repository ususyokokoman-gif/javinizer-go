package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBulkStateV8IsDiscarded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := []byte("{\"version\":8,\"files\":{\"old.mp4\":{\"status\":\"confirmed\",\"catalog_id\":\"ABC-123\",\"decision_policy_version\":1}},\"titles\":{\"catalog\\u0000abc-123\":{\"status\":\"confirmed\",\"catalog_id\":\"ABC-123\",\"decision_policy_version\":1}}}")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := newStateStore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if store.state.Version != bulkStateVersion {
		t.Fatalf("version=%d, want %d", store.state.Version, bulkStateVersion)
	}
	if len(store.state.Files) != 0 || len(store.state.Titles) != 0 {
		t.Fatalf("v8 state survived policy upgrade: files=%d titles=%d", len(store.state.Files), len(store.state.Titles))
	}
}

func TestBulkStateV9IsDiscarded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := []byte("{\"version\":9,\"files\":{\"me.mp4\":{\"status\":\"confirmed\",\"catalog_id\":\"KKV-538\",\"decision_policy_version\":2}},\"titles\":{\"title\\u0000me\":{\"status\":\"confirmed\",\"catalog_id\":\"KKV-538\",\"decision_policy_version\":2}}}")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := newStateStore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if store.state.Version != bulkStateVersion {
		t.Fatalf("version=%d, want %d", store.state.Version, bulkStateVersion)
	}
	if len(store.state.Files) != 0 || len(store.state.Titles) != 0 {
		t.Fatalf("v9 state survived policy upgrade: files=%d titles=%d", len(store.state.Files), len(store.state.Titles))
	}
}

func TestBulkStateV10IsDiscarded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := []byte("{\"version\":10,\"files\":{\"random.mp4\":{\"status\":\"review\",\"catalog_id\":\"SPG0U9LUQ-77GOR\",\"decision_policy_version\":3}},\"titles\":{\"catalog\\u0000spg0u9luq-77gor\":{\"status\":\"review\",\"catalog_id\":\"SPG0U9LUQ-77GOR\",\"decision_policy_version\":3}}}")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := newStateStore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if store.state.Version != bulkStateVersion {
		t.Fatalf("version=%d, want %d", store.state.Version, bulkStateVersion)
	}
	if len(store.state.Files) != 0 || len(store.state.Titles) != 0 {
		t.Fatalf("v10 state survived classifier upgrade: files=%d titles=%d", len(store.state.Files), len(store.state.Titles))
	}
}
