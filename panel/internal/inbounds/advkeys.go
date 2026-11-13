package inbounds

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
)

// MLDSA65Verify derives the Reality client "mldsa65Verify" (ML-DSA-65
// public key, unpadded base64url) from the server "mldsa65Seed" (32 bytes,
// unpadded base64url), the way `xray mldsa65 -i` does.
func MLDSA65Verify(seed string) (string, error) {
	if !b64urlOK(seed, mldsa65.SeedSize) {
		return "", errors.New("must be 32 bytes of unpadded base64url (xray mldsa65)")
	}
	b, _ := base64.RawURLEncoding.DecodeString(seed)
	var s [mldsa65.SeedSize]byte
	copy(s[:], b)
	pk, _ := mldsa65.NewKeyFromSeed(&s)
	return base64.RawURLEncoding.EncodeToString(pk.Bytes()), nil
}

// KeyPair is a generated server secret and its client half.
type KeyPair struct {
	// Server is the secret the server config holds (mldsa65Seed,
	// echServerKeys); Client what client configs carry (mldsa65Verify,
	// echConfigList).
	Server string `json:"server"`
	Client string `json:"client"`
	// Source: "xray" when the installed Xray-core made it, "go" otherwise.
	Source string `json:"source"`
}

func runXray(bin string, args ...string) ([]byte, bool) {
	if bin == "" {
		return nil, false
	}
	if st, err := os.Stat(bin); err != nil || st.IsDir() {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	return out, err == nil
}

// GenerateMLDSA65 makes a Reality ML-DSA-65 pair with `xray mldsa65`
// (checked against the panel's own derivation), else in Go.
func GenerateMLDSA65(bin string) (KeyPair, error) {
	if out, ok := runXray(bin, "mldsa65"); ok {
		var seed, ver string
		sc := bufio.NewScanner(bytes.NewReader(out))
		sc.Buffer(make([]byte, 0, 8192), 1<<16)
		for sc.Scan() {
			k, v, ok := strings.Cut(sc.Text(), ":")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "seed":
				seed = strings.TrimSpace(v)
			case "verify":
				ver = strings.TrimSpace(v)
			}
		}
		if mine, err := MLDSA65Verify(seed); err == nil && mine == ver {
			return KeyPair{Server: seed, Client: ver, Source: "xray"}, nil
		}
		slog.Warn("xray mldsa65 did not match the panel's derivation; using the panel's own")
	}
	b := make([]byte, mldsa65.SeedSize)
	if _, err := rand.Read(b); err != nil {
		return KeyPair{}, err
	}
	seed := base64.RawURLEncoding.EncodeToString(b)
	ver, err := MLDSA65Verify(seed)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{Server: seed, Client: ver, Source: "go"}, nil
}

// ECHConfigListFromKeys derives the client ECH config list (base64) from
// the server's "echServerKeys" (base64: per key set a 2-byte length, the
// private key, a 2-byte length, the ECHConfig), as `xray tls ech -i`.
func ECHConfigListFromKeys(keys string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(keys)
	if err != nil || len(data) == 0 {
		return "", errors.New("must be base64 (xray tls ech)")
	}
	var list []byte
	for len(data) > 0 {
		if len(data) < 2 {
			return "", errors.New("truncated key set")
		}
		kl := int(binary.BigEndian.Uint16(data))
		if len(data) < 2+kl+2 {
			return "", errors.New("truncated key set")
		}
		cl := int(binary.BigEndian.Uint16(data[2+kl:]))
		if len(data) < 2+kl+2+cl || kl == 0 || cl < 4 {
			return "", errors.New("truncated key set")
		}
		cfg := data[2+kl+2 : 2+kl+2+cl]
		if binary.BigEndian.Uint16(cfg) != 0xfe0d {
			return "", errors.New("not an ECH config (version 0xfe0d)")
		}
		list = binary.BigEndian.AppendUint16(list, uint16(len(cfg)))
		list = append(list, cfg...)
		data = data[2+kl+2+cl:]
	}
	return base64.StdEncoding.EncodeToString(list), nil
}

// echConfig encodes one ECHConfig (draft-ietf-tls-esni, version 0xfe0d)
// for an X25519 key, with the cipher suites `xray tls ech` offers.
func echConfig(pub []byte, publicName string) []byte {
	var body []byte
	body = append(body, 0) // config id
	body = binary.BigEndian.AppendUint16(body, 0x0020)
	body = binary.BigEndian.AppendUint16(body, uint16(len(pub)))
	body = append(body, pub...)
	var suites []byte
	for _, kdf := range []uint16{1, 2, 3} {
		for _, aead := range []uint16{1, 2, 3} {
			suites = binary.BigEndian.AppendUint16(suites, kdf)
			suites = binary.BigEndian.AppendUint16(suites, aead)
		}
	}
	body = binary.BigEndian.AppendUint16(body, uint16(len(suites)))
	body = append(body, suites...)
	body = append(body, 0) // max name length
	body = append(body, byte(len(publicName)))
	body = append(body, publicName...)
	body = binary.BigEndian.AppendUint16(body, 0) // no extensions
	out := binary.BigEndian.AppendUint16(nil, 0xfe0d)
	out = binary.BigEndian.AppendUint16(out, uint16(len(body)))
	return append(out, body...)
}

// GenerateECH makes ECH server keys and the client config list for a
// public (outer) server name, with `xray tls ech` when possible.
func GenerateECH(bin, publicName string) (KeyPair, error) {
	publicName = strings.TrimSpace(publicName)
	if publicName == "" {
		publicName = "cloudflare-ech.com"
	}
	if !hostRe.MatchString(publicName) {
		return KeyPair{}, verr("ECH public name must be a host name")
	}
	if out, ok := runXray(bin, "tls", "ech", "--serverName", publicName); ok {
		var list, keys string
		lines := strings.Split(string(out), "\n")
		for i, l := range lines {
			if i+1 >= len(lines) {
				break
			}
			switch strings.ToLower(strings.TrimSpace(l)) {
			case "ech config list:":
				list = strings.TrimSpace(lines[i+1])
			case "ech server keys:":
				keys = strings.TrimSpace(lines[i+1])
			}
		}
		if mine, err := ECHConfigListFromKeys(keys); err == nil && mine == list {
			return KeyPair{Server: keys, Client: list, Source: "xray"}, nil
		}
		slog.Warn("xray tls ech did not match the panel's derivation; using the panel's own")
	}
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, err
	}
	cfg := echConfig(k.PublicKey().Bytes(), publicName)
	priv := k.Bytes()
	var ks []byte
	ks = binary.BigEndian.AppendUint16(ks, uint16(len(priv)))
	ks = append(ks, priv...)
	ks = binary.BigEndian.AppendUint16(ks, uint16(len(cfg)))
	ks = append(ks, cfg...)
	keys := base64.StdEncoding.EncodeToString(ks)
	list, err := ECHConfigListFromKeys(keys)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{Server: keys, Client: list, Source: "go"}, nil
}
