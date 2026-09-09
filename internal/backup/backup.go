package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Bundle is an encrypted portable backup of identity + data.
type Bundle struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	SaltHex   string    `json:"salt_hex"`
	NonceHex  string    `json:"nonce_hex"`
	DataHex   string    `json:"data_hex"`
}

// Payload holds the plaintext content inside a bundle.
type Payload struct {
	PrivateKeyHex string          `json:"private_key_hex"`
	Config        json.RawMessage `json:"config,omitempty"`
	Contacts      json.RawMessage `json:"contacts,omitempty"`
	History       json.RawMessage `json:"history,omitempty"`
}

const bundleVersion = 1

// deriveKey stretches a password with PBKDF2-HMAC-SHA256 (stdlib only).
func deriveKey(password string, salt []byte, iterations int) []byte {
	hLen := sha256.Size
	key := make([]byte, 32)
	block := 1
	for len(key) > 0 {
		mac := hmac.New(sha256.New, []byte(password))
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, []byte(password))
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		n := copy(key, t)
		key = key[n:]
		block++
		_ = hLen
	}
	// Recompute cleanly for fixed 32-byte output.
	out := pbkdf2(password, salt, iterations)
	return out
}

func pbkdf2(password string, salt []byte, iterations int) []byte {
	var out []byte
	for block := 1; len(out) < 32; block++ {
		mac := hmac.New(sha256.New, []byte(password))
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, []byte(password))
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:32]
}

// Export encrypts payload with a password and writes a bundle file.
func Export(path, password string, payload Payload) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	key := deriveKey(password, salt, 100000)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	sealed := gcm.Seal(nonce, nonce, plain, nil)
	bundle := Bundle{
		Version:   bundleVersion,
		CreatedAt: time.Now(),
		SaltHex:   hex.EncodeToString(salt),
		NonceHex:  hex.EncodeToString(nonce),
		DataHex:   hex.EncodeToString(sealed),
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Import decrypts a bundle file with the password.
func Import(path, password string) (Payload, error) {
	var bundle Bundle
	data, err := os.ReadFile(path)
	if err != nil {
		return Payload{}, err
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		return Payload{}, fmt.Errorf("invalid backup file")
	}
	salt, err := hex.DecodeString(bundle.SaltHex)
	if err != nil {
		return Payload{}, fmt.Errorf("invalid backup file")
	}
	sealed, err := hex.DecodeString(bundle.DataHex)
	if err != nil {
		return Payload{}, fmt.Errorf("invalid backup file")
	}
	key := deriveKey(password, salt, 100000)
	block, err := aes.NewCipher(key)
	if err != nil {
		return Payload{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Payload{}, err
	}
	if len(sealed) < gcm.NonceSize() {
		return Payload{}, fmt.Errorf("invalid backup file")
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return Payload{}, fmt.Errorf("wrong password or corrupted backup")
	}
	var payload Payload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return Payload{}, fmt.Errorf("corrupted backup payload")
	}
	return payload, nil
}
