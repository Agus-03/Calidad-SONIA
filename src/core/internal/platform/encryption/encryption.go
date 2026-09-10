// Package encryption is the single place the four sensitive columns are turned into bytes and back (ADR-0031, regla 11).
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const (
	// KeyBytes is the 256 of AES-256: anything shorter is a different algorithm wearing the same name (ADR-0031, regla 6).
	KeyBytes = 32

	// The first byte of every ciphertext, so a key rotation can leave old rows readable instead of migrating the table at once (regla 5).
	version byte = 0x01

	nonceBytes = 12
)

// ErrCorrupted is what authenticated encryption buys: a tampered or truncated ciphertext refuses instead of returning a different number (regla 2).
var ErrCorrupted = errors.New("encryption: the stored value cannot be decrypted")

// Cipher holds the key. It is built once at boot and injected, never reached through a package global (regla 10 de CLAUDE.md).
type Cipher struct{ aead cipher.AEAD }

// New takes the raw key, not the base64: decoding is the caller's problem because that is where a bad key must stop the process.
func New(key []byte) (*Cipher, error) {
	if len(key) != KeyBytes {
		return nil, fmt.Errorf("encryption: the key is %d bytes and needs %d", len(key), KeyBytes)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("encryption: cannot build the cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encryption: cannot build gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// DecodeKey reads the shape the environment carries it in, so both the boot and its test agree on what a valid key looks like.
func DecodeKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("encryption: the key is not valid base64: %w", err)
	}
	if len(key) != KeyBytes {
		return nil, fmt.Errorf("encryption: the key decodes to %d bytes and needs %d", len(key), KeyBytes)
	}
	return key, nil
}

// Encrypt answers nil for an empty value, so a contact without a CBU stores NULL and not the ciphertext of nothing.
func (c *Cipher) Encrypt(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}

	nonce := make([]byte, nonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("encryption: cannot draw a nonce: %w", err)
	}

	// The version travels as additional data too, so changing it on a stored row breaks the tag instead of choosing another format.
	out := make([]byte, 0, 1+nonceBytes+len(plaintext)+c.aead.Overhead())
	out = append(out, version)
	out = append(out, nonce...)
	return c.aead.Seal(out, nonce, []byte(plaintext), out[:1]), nil
}

// Decrypt is the mirror: nil in, empty out, because a NULL column means the contact never had the datum.
func (c *Cipher) Decrypt(stored []byte) (string, error) {
	if len(stored) == 0 {
		return "", nil
	}
	if len(stored) < 1+nonceBytes+c.aead.Overhead() {
		return "", ErrCorrupted
	}
	if stored[0] != version {
		return "", fmt.Errorf("%w: unknown format version %#x", ErrCorrupted, stored[0])
	}

	nonce := stored[1 : 1+nonceBytes]
	plaintext, err := c.aead.Open(nil, nonce, stored[1+nonceBytes:], stored[:1])
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrCorrupted, err)
	}
	return string(plaintext), nil
}
