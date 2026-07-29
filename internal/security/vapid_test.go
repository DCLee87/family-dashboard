package security

import (
	"errors"
	"testing"
)

func TestGenerateAndValidateVAPIDKeys(t *testing.T) {
	privateKey, publicKey, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateVAPIDKeys(privateKey, publicKey); err != nil {
		t.Fatalf("generated keys were invalid: %v", err)
	}
	otherPrivate, _, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateVAPIDKeys(otherPrivate, publicKey); !errors.Is(err, ErrInvalidVAPIDKeys) {
		t.Fatalf("mismatched key pair: got %v, want %v", err, ErrInvalidVAPIDKeys)
	}
}
