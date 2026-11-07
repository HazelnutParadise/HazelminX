package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

const (
	keyLen   = 32 // AES-256
	nonceLen = 12 // GCM standard nonce
)

// aad binds a ciphertext to its row so one cannot be moved to another secret
// or version and still decrypt.
func aad(secretID string, version int) []byte {
	return []byte(fmt.Sprintf("%s@%d", secretID, version))
}

// seal encrypts plaintext with key under the given AAD: nonce || ciphertext || tag.
func seal(key, plaintext, associated []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, associated), nil
}

// open reverses seal. On failure it returns ErrDecrypt without echoing input.
func open(key, sealed, associated []byte) ([]byte, error) {
	if len(sealed) < nonceLen {
		return nil, ErrDecrypt
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	pt, err := gcm.Open(nil, sealed[:nonceLen], sealed[nonceLen:], associated)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

func randomKey() ([]byte, error) {
	k := make([]byte, keyLen)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	return k, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
