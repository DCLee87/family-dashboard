package security

import (
	"bytes"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

var ErrInvalidVAPIDKeys = errors.New("invalid VAPID key pair")

func GenerateVAPIDKeys() (privateKey, publicKey string, err error) {
	private, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	public := elliptic.Marshal(elliptic.P256(), x, y)
	return base64.RawURLEncoding.EncodeToString(private),
		base64.RawURLEncoding.EncodeToString(public),
		nil
}

func ValidateVAPIDKeys(privateKey, publicKey string) error {
	private, err := base64.RawURLEncoding.DecodeString(privateKey)
	if err != nil || len(private) != 32 {
		return ErrInvalidVAPIDKeys
	}
	public, err := base64.RawURLEncoding.DecodeString(publicKey)
	if err != nil || len(public) != 65 || public[0] != 4 {
		return ErrInvalidVAPIDKeys
	}
	x, y := elliptic.P256().ScalarBaseMult(private)
	if !bytes.Equal(public, elliptic.Marshal(elliptic.P256(), x, y)) {
		return ErrInvalidVAPIDKeys
	}
	return nil
}
