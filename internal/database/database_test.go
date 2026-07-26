package database

import (
	"context"
	"testing"
)

func TestDatabasePersistsStartCount(t *testing.T) {
	dataDir := t.TempDir()

	first, err := Open(dataDir, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	check, err := Open(dataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	count, err := check.StartCount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("check-only open changed start count: got %d, want 1", count)
	}
	if err := check.IntegrityCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := check.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := Open(dataDir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	count, err = second.StartCount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("start count did not persist: got %d, want 2", count)
	}
}
