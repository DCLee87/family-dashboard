package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"math/big"
)

const tokenBytes = 32

func NewToken() string {
	value := make([]byte, tokenBytes)
	rand.Read(value)
	return base64.RawURLEncoding.EncodeToString(value)
}

func NewDisplayCode(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("display code length must be positive")
	}
	value := make([]byte, length)
	for i := range value {
		digit, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", fmt.Errorf("generate display code: %w", err)
		}
		value[i] = byte('0' + digit.Int64())
	}
	return string(value), nil
}

func TokenHash(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}

func TokenMatches(token string, expected [sha256.Size]byte) bool {
	actual := TokenHash(token)
	return subtle.ConstantTimeCompare(actual[:], expected[:]) == 1
}
