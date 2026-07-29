package main

import "testing"

func TestParsePrefixes(t *testing.T) {
	prefixes, err := parsePrefixes("192.0.2.0/24, 10.0.0.12/8")
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixes) != 2 {
		t.Fatalf("prefix count: got %d, want 2", len(prefixes))
	}
	if prefixes[1].String() != "10.0.0.0/8" {
		t.Fatalf("prefix was not masked: %s", prefixes[1])
	}
	if _, err := parsePrefixes("not-a-prefix"); err == nil {
		t.Fatal("invalid prefix was accepted")
	}
}

func TestEnvBool(t *testing.T) {
	t.Setenv("FAMILY_DASHBOARD_TEST_BOOL", "false")
	value, err := envBool("FAMILY_DASHBOARD_TEST_BOOL", true)
	if err != nil {
		t.Fatal(err)
	}
	if value {
		t.Fatal("false environment value was parsed as true")
	}
	t.Setenv("FAMILY_DASHBOARD_TEST_BOOL", "invalid")
	if _, err := envBool("FAMILY_DASHBOARD_TEST_BOOL", true); err == nil {
		t.Fatal("invalid boolean was accepted")
	}
}
