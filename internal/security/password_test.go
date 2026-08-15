package security

import (
	"strings"
	"testing"
)

func TestPasswordHashAndVerify(t *testing.T) {
	password := "correct horse battery staple"
	encoded, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, password) {
		t.Fatal("encoded hash contains password")
	}
	ok, err := VerifyPassword(encoded, password)
	if err != nil || !ok {
		t.Fatalf("correct password rejected: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword(encoded, "incorrect password value")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("incorrect password accepted")
	}
}

func TestPasswordUsesUniqueSaltAndBounds(t *testing.T) {
	first, err := HashPassword("a secure family password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashPassword("a secure family password")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("password hashes used the same salt")
	}
	for _, value := range []string{"short", strings.Repeat("가", MaxPasswordRunes+1)} {
		if _, err := HashPassword(value); err == nil {
			t.Fatalf("invalid password length accepted: %d", len([]rune(value)))
		}
	}
}
