package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonVersion = argon2.Version
	pinMemory    = 19 * 1024
	pinTime      = 2
	pinThreads   = 1
	pinSaltBytes = 16
	pinKeyBytes  = 32
)

func HashPIN(pin string) (string, error) {
	if !validPIN(pin) {
		return "", fmt.Errorf("PIN must contain exactly four digits")
	}
	salt := make([]byte, pinSaltBytes)
	rand.Read(salt)
	key := argon2.IDKey([]byte(pin), salt, pinTime, pinMemory, pinThreads, pinKeyBytes)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argonVersion,
		pinMemory,
		pinTime,
		pinThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func VerifyPIN(encoded, pin string) (bool, error) {
	if !validPIN(pin) {
		return false, nil
	}
	params, salt, expected, err := parsePINHash(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey(
		[]byte(pin),
		salt,
		params.time,
		params.memory,
		params.threads,
		uint32(len(expected)),
	)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

type pinParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func parsePINHash(encoded string) (pinParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return pinParams{}, nil, nil, fmt.Errorf("invalid PIN hash format")
	}
	version, err := parseNamedUint(parts[2], "v")
	if err != nil || version != argonVersion {
		return pinParams{}, nil, nil, fmt.Errorf("unsupported Argon2 version")
	}

	values := strings.Split(parts[3], ",")
	if len(values) != 3 {
		return pinParams{}, nil, nil, fmt.Errorf("invalid Argon2 parameters")
	}
	memory, err := parseNamedUint(values[0], "m")
	if err != nil {
		return pinParams{}, nil, nil, err
	}
	iterations, err := parseNamedUint(values[1], "t")
	if err != nil {
		return pinParams{}, nil, nil, err
	}
	parallelism, err := parseNamedUint(values[2], "p")
	if err != nil ||
		memory < 7*1024 || memory > 256*1024 ||
		iterations == 0 || iterations > 10 ||
		parallelism == 0 || parallelism > 8 {
		return pinParams{}, nil, nil, fmt.Errorf("invalid Argon2 parameters")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 16 {
		return pinParams{}, nil, nil, fmt.Errorf("invalid PIN hash salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) < 16 || len(expected) > 64 {
		return pinParams{}, nil, nil, fmt.Errorf("invalid PIN hash value")
	}
	return pinParams{
		memory:  uint32(memory),
		time:    uint32(iterations),
		threads: uint8(parallelism),
	}, salt, expected, nil
}

func parseNamedUint(value, name string) (uint64, error) {
	prefix := name + "="
	if !strings.HasPrefix(value, prefix) {
		return 0, fmt.Errorf("missing Argon2 parameter %s", name)
	}
	number, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse Argon2 parameter %s: %w", name, err)
	}
	return number, nil
}

func validPIN(pin string) bool {
	if len(pin) != 4 {
		return false
	}
	for _, value := range pin {
		if value < '0' || value > '9' {
			return false
		}
	}
	return true
}
