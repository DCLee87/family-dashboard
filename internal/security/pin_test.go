package security

import (
	"strings"
	"testing"
)

func TestPINHashAndVerify(t *testing.T) {
	encoded, err := HashPIN("4826")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, "4826") {
		t.Fatal("encoded hash contains PIN")
	}
	ok, err := VerifyPIN(encoded, "4826")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("correct PIN was rejected")
	}
	ok, err = VerifyPIN(encoded, "4827")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("incorrect PIN was accepted")
	}
}

func TestPINUsesUniqueSalt(t *testing.T) {
	first, err := HashPIN("4826")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPIN("4826")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("hashes used the same salt")
	}
}

func TestPINRejectsInvalidInput(t *testing.T) {
	for _, pin := range []string{"", "123", "12345", "12a4", "１２３４"} {
		if _, err := HashPIN(pin); err == nil {
			t.Fatalf("invalid PIN %q was accepted", pin)
		}
	}
	ok, err := VerifyPIN("$invalid", "1234")
	if err == nil || ok {
		t.Fatal("malformed hash was accepted")
	}
}
