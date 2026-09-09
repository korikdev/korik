package cryptoutil

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

// DeriveSessionKey calculates shared secret via ECDH then derives an AES-256 key
// using SHA-256 (sufficient for this use case, not full HKDF).
func DeriveSessionKey(myPriv *ecdh.PrivateKey, theirPub *ecdh.PublicKey) ([]byte, error) {
	shared, err := myPriv.ECDH(theirPub)
	if err != nil {
		return nil, err
	}
	key := sha256.Sum256(append(shared, []byte("korik-e2e-v1")...))
	return key[:], nil
}

// Encrypt using AES-256-GCM, result = nonce || ciphertext
func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt reverses Encrypt
func Decrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// MixKeys combines an old session key with a fresh ECDH-derived key
// for periodic rekeying (forward secrecy). SHA-256 binds both halves
// with a domain separator so old and new keys cannot be confused.
func MixKeys(oldKey, freshKey []byte) []byte {
	h := sha256.New()
	h.Write([]byte("korik-rekey-v1:"))
	h.Write(oldKey)
	h.Write([]byte("|"))
	h.Write(freshKey)
	return h.Sum(nil)
}
