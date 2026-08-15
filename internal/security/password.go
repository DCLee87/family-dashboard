package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory    = 64 * 1024
	passwordTime      = 3
	passwordThreads   = 1
	passwordSaltBytes = 16
	passwordKeyBytes  = 32
	MinPasswordRunes  = 12
	MaxPasswordRunes  = 128
)

func HashPassword(password string) (string, error) {
	if !ValidPassword(password) {
		return "", fmt.Errorf("password must contain between %d and %d characters", MinPasswordRunes, MaxPasswordRunes)
	}
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, passwordTime, passwordMemory, passwordThreads, passwordKeyBytes)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argonVersion, passwordMemory, passwordTime, passwordThreads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) (bool, error) {
	if !ValidPassword(password) {
		return false, nil
	}
	params, salt, expected, err := parsePINHash(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func ValidPassword(password string) bool {
	if !utf8.ValidString(password) {
		return false
	}
	count := utf8.RuneCountInString(password)
	return count >= MinPasswordRunes && count <= MaxPasswordRunes
}
