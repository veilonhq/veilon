package inbounds

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/veilonhq/veilon/panel/internal/util"
)

// RealityKeys is a freshly generated x25519 key pair plus a short id.
type RealityKeys struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	ShortID    string `json:"short_id"`
}

// GenerateRealityKeys creates an x25519 key pair encoded the way Xray
// expects (unpadded base64url, 32 bytes each).
func GenerateRealityKeys() (RealityKeys, error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return RealityKeys{}, err
	}
	// Clamp like the reference implementation so the stored key is canonical.
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	privB64 := base64.RawURLEncoding.EncodeToString(priv[:])
	pub, err := PublicKeyFromPrivate(privB64)
	if err != nil {
		return RealityKeys{}, err
	}
	return RealityKeys{PrivateKey: privB64, PublicKey: pub, ShortID: util.RandHex(8)}, nil
}

// PublicKeyFromPrivate derives the x25519 public key from a base64url private key.
func PublicKeyFromPrivate(priv string) (string, error) {
	raw, err := decodeKey(priv)
	if err != nil {
		return "", err
	}
	k, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

func decodeKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, errors.New("key must be 32 bytes base64url")
}
