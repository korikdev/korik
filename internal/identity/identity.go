package identity

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Identity is the cryptographic identity of a single korik node.
// JID (similar to Jabber ID) = permanent identity, derived from public key.
// LID (Local ID) = short version of JID for display in UI to avoid being too long.
type Identity struct {
	Private *ecdh.PrivateKey `json:"-"`
	Public  *ecdh.PublicKey  `json:"-"`
	JID     string           `json:"jid"`
	LID     string           `json:"lid"`

	rawPriv []byte `json:"-"`
}

type storedIdentity struct {
	PrivateKeyHex string `json:"private_key_hex"`
}

// LoadOrCreate reads identity from file, or creates new one if doesn't exist yet.
func LoadOrCreate(path string) (*Identity, error) {
	if data, err := os.ReadFile(path); err == nil {
		var s storedIdentity
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}
		rawPriv, err := hex.DecodeString(s.PrivateKeyHex)
		if err != nil {
			return nil, err
		}
		return fromRawPrivate(rawPriv)
	}

	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	rawPriv := priv.Bytes()

	id, err := fromRawPrivate(rawPriv)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	out, err := json.Marshal(storedIdentity{PrivateKeyHex: hex.EncodeToString(rawPriv)})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return nil, err
	}
	return id, nil
}

func fromRawPrivate(rawPriv []byte) (*Identity, error) {
	curve := ecdh.X25519()
	priv, err := curve.NewPrivateKey(rawPriv)
	if err != nil {
		return nil, err
	}
	pub := priv.PublicKey()
	jid := "korik:" + hex.EncodeToString(pub.Bytes())
	lid := hex.EncodeToString(pub.Bytes())[:8]

	return &Identity{
		Private: priv,
		Public:  pub,
		JID:     jid,
		LID:     lid,
		rawPriv: rawPriv,
	}, nil
}

// PublicKeyHex is used for handshake, sent as part of hello message.
func (i *Identity) PublicKeyHex() string {
	return hex.EncodeToString(i.Public.Bytes())
}

// RawPrivateHex exposes the private key for encrypted backup only.
// Callers must never log or transmit this value in plaintext.
func (i *Identity) RawPrivateHex() string {
	return hex.EncodeToString(i.rawPriv)
}

// RestoreFromHex rebuilds an identity from a backed-up private key hex
// and persists it to path.
func RestoreFromHex(privateHex, path string) (*Identity, error) {
	rawPriv, err := hex.DecodeString(privateHex)
	if err != nil {
		return nil, err
	}
	id, err := fromRawPrivate(rawPriv)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	out, err := json.Marshal(storedIdentity{PrivateKeyHex: hex.EncodeToString(rawPriv)})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return nil, err
	}
	return id, nil
}

// ParsePeerPublicKey converts hex pubkey from peer to *ecdh.PublicKey.
func ParsePeerPublicKey(hexStr string) (*ecdh.PublicKey, error) {
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, err
	}
	return ecdh.X25519().NewPublicKey(raw)
}

// JIDFromPublicKeyHex calculates JID from hex pubkey (used for peer identification).
func JIDFromPublicKeyHex(hexStr string) string {
	return "korik:" + hexStr
}

// LIDFromPublicKeyHex calculates LID (short id) from hex pubkey.
func LIDFromPublicKeyHex(hexStr string) string {
	if len(hexStr) < 8 {
		return hexStr
	}
	return hexStr[:8]
}

func DefaultIdentityPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".korik", "identity.json")
}

func (i *Identity) String() string {
	return fmt.Sprintf("%s (lid: %s)", i.JID, i.LID)
}
