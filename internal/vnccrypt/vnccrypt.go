// Package vnccrypt encrypts stored VNC passwords at rest using AES-256-GCM
// with a key derived from a server secret. Ciphertext is base64-encoded; the
// plaintext never leaves the gateway process except during the RFB handshake.
package vnccrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// ErrNoSecret is returned when encryption is attempted with no secret, so
// callers can fail closed rather than persist plaintext.
var ErrNoSecret = errors.New("vnccrypt: no secret configured")

func deriveKey(secret string) ([]byte, error) {
	if secret == "" {
		return nil, ErrNoSecret
	}
	sum := sha256.Sum256([]byte(secret))
	return sum[:], nil
}

// Encrypt returns base64(nonce || ciphertext) for plaintext under secret.
func Encrypt(secret, plaintext string) (string, error) {
	key, err := deriveKey(secret)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

// Decrypt reverses Encrypt. An empty input yields an empty password.
func Decrypt(secret, encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	key, err := deriveKey(secret)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("vnccrypt: ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
