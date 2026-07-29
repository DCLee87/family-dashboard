package security

import (
	"strings"
	"testing"
)

func TestTokenHashAndMatch(t *testing.T) {
	first := NewToken()
	second := NewToken()
	if first == second {
		t.Fatal("independent tokens matched")
	}
	if len(first) < 40 {
		t.Fatalf("token is unexpectedly short: %d", len(first))
	}
	hash := TokenHash(first)
	if !TokenMatches(first, hash) {
		t.Fatal("token did not match its hash")
	}
	if TokenMatches(second, hash) {
		t.Fatal("different token matched hash")
	}
}

func TestDisplayCode(t *testing.T) {
	code, err := NewDisplayCode(6)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Fatalf("code length: got %d, want 6", len(code))
	}
	if strings.Trim(code, "0123456789") != "" {
		t.Fatalf("code contains non-digits: %q", code)
	}
	if _, err := NewDisplayCode(0); err == nil {
		t.Fatal("zero-length code was accepted")
	}
}
