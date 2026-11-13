package inbounds

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cloudflare/circl/kem/mlkem/mlkem768"
)

// VLESS Encryption (Xray-core >= 25.9.5): the inbound's "decryption" and the
// client's "encryption". Both are dot-joined strings:
//
//	server: mlkem768x25519plus.<mode>.<ticket>s.<server key>
//	client: mlkem768x25519plus.<mode>.<0rtt|1rtt>.<client key>
//
// The server key is an X25519 private key (32 bytes) or an ML-KEM-768 seed
// (64 bytes); the client key is the matching X25519 public key (32 bytes)
// or ML-KEM-768 encapsulation key (1184 bytes), all unpadded base64url.
// "<ticket>s" is how long the server's 0-RTT tickets live ("600s" makes
// Xray pick 50-100% of it per ticket); "0s" allows 1-RTT only.

// VLESSEncPrefix is the handshake block, the only one Xray has.
const VLESSEncPrefix = "mlkem768x25519plus"

// Authentication (server key) types.
const (
	EncAuthX25519 = "x25519"
	EncAuthMLKEM  = "mlkem768"
)

// MinVLESSEncXray is the first Xray-core release with this format.
const MinVLESSEncXray = "25.9.5"

var (
	EncAuths = []string{EncAuthX25519, EncAuthMLKEM}
	EncModes = []string{"native", "xorpub", "random"}
	EncRTTs  = []string{"0rtt", "1rtt"}
)

// DefaultTicketSeconds is the ticket lifetime `xray vlessenc` writes.
const DefaultTicketSeconds = 600

// MaxTicketSeconds caps the ticket lifetime (one day).
const MaxTicketSeconds = 86400

// VLESSEnc is the VLESS Encryption setting of a VLESS inbound.
type VLESSEnc struct {
	Enabled bool `json:"enabled"`
	// Auth is the server authentication key type: x25519 or mlkem768
	// (post-quantum). The session key exchange is post-quantum either way.
	Auth string `json:"auth"`
	// Mode is the traffic appearance: native, xorpub or random.
	Mode string `json:"mode"`
	// RTT: "0rtt" lets clients resume with a ticket (server issues
	// tickets for TicketSeconds); "1rtt" makes every connection do a
	// full handshake (server "0s").
	RTT           string `json:"rtt"`
	TicketSeconds int    `json:"ticket_seconds"`
	// PrivateKey is the server key (X25519 private key or ML-KEM-768
	// seed). Empty = generate one.
	PrivateKey string `json:"private_key"`
	// PublicKey (read-only) is the client key derived from PrivateKey.
	PublicKey string `json:"public_key"`
	// Encryption (read-only) is the client "encryption" string that share
	// links and client configs carry ("" while off).
	Encryption string `json:"encryption"`
}

// On reports whether VLESS Encryption is active on the inbound.
func (e *VLESSEnc) On() bool { return e != nil && e.Enabled && e.PrivateKey != "" && e.PublicKey != "" }

func (e *VLESSEnc) ticket() string {
	if e.RTT == "1rtt" {
		return "0s"
	}
	return strconv.Itoa(e.TicketSeconds) + "s"
}

// Decryption is the server "decryption" value ("none" while off).
func (e *VLESSEnc) Decryption() string {
	if !e.On() {
		return "none"
	}
	return strings.Join([]string{VLESSEncPrefix, e.Mode, e.ticket(), e.PrivateKey}, ".")
}

// ClientEncryption is the client "encryption" value ("none" while off).
func (e *VLESSEnc) ClientEncryption() string {
	if !e.On() {
		return "none"
	}
	return strings.Join([]string{VLESSEncPrefix, e.Mode, e.RTT, e.PublicKey}, ".")
}

// DefaultVLESSEnc is the setting of a VLESS inbound that never had one.
func DefaultVLESSEnc() *VLESSEnc {
	return &VLESSEnc{Auth: EncAuthX25519, Mode: "native", RTT: "0rtt", TicketSeconds: DefaultTicketSeconds}
}

// normalizeVLESSEnc validates e and fills its defaults, keys and the
// derived client values. A disabled setting keeps its keys so it can be
// switched back on with the same client links.
func normalizeVLESSEnc(e *VLESSEnc) error {
	if e.Auth == "" {
		e.Auth = EncAuthX25519
	}
	if e.Mode == "" {
		e.Mode = "native"
	}
	if e.RTT == "" {
		e.RTT = "0rtt"
	}
	e.PrivateKey = strings.TrimSpace(e.PrivateKey)
	// A whole server string (e.g. from `xray vlessenc`) pasted as the key.
	if strings.HasPrefix(e.PrivateKey, VLESSEncPrefix+".") {
		if err := e.importDecryption(e.PrivateKey); err != nil {
			return err
		}
	}
	if !in(e.Auth, EncAuths) {
		return verr("vless_encryption.auth must be one of %s", strings.Join(EncAuths, ", "))
	}
	if !in(e.Mode, EncModes) {
		return verr("vless_encryption.mode must be one of %s", strings.Join(EncModes, ", "))
	}
	if !in(e.RTT, EncRTTs) {
		return verr("vless_encryption.rtt must be one of %s", strings.Join(EncRTTs, ", "))
	}
	if e.RTT == "0rtt" && e.TicketSeconds == 0 {
		e.TicketSeconds = DefaultTicketSeconds
	}
	if e.RTT == "1rtt" {
		e.TicketSeconds = 0
	}
	if e.TicketSeconds < 0 || e.TicketSeconds > MaxTicketSeconds {
		return verr("vless_encryption.ticket_seconds must be 1-%d", MaxTicketSeconds)
	}
	if e.PrivateKey == "" {
		if !e.Enabled {
			e.PublicKey, e.Encryption = "", ""
			return nil
		}
		k, err := GenerateVLESSEncKeys(XrayBinary, e.Auth)
		if err != nil {
			return err
		}
		e.PrivateKey = k.PrivateKey
	}
	pub, err := VLESSEncClientKey(e.Auth, e.PrivateKey)
	if err != nil {
		return verr("vless_encryption.private_key: %v", err)
	}
	e.PublicKey = pub
	e.Encryption = ""
	if e.On() {
		e.Encryption = e.ClientEncryption()
	}
	return nil
}

// importDecryption reads a server "decryption" string into e (padding
// blocks are dropped; Xray then uses its default padding).
func (e *VLESSEnc) importDecryption(s string) error {
	b := strings.Split(s, ".")
	if len(b) < 4 || b[0] != VLESSEncPrefix {
		return verr("vless_encryption.private_key: not a VLESS decryption string")
	}
	e.Mode = b[1]
	t := strings.TrimSuffix(b[2], "s")
	if _, to, ok := strings.Cut(t, "-"); ok {
		t = to
	}
	n, err := strconv.Atoi(t)
	if err != nil || !strings.HasSuffix(b[2], "s") {
		return verr("vless_encryption.private_key: the ticket block %q must look like 600s", b[2])
	}
	if n == 0 {
		e.RTT, e.TicketSeconds = "1rtt", 0
	} else {
		e.RTT, e.TicketSeconds = "0rtt", n
	}
	var keys []string
	for _, x := range b[3:] {
		if len(x) >= 20 {
			keys = append(keys, x)
		}
	}
	if len(keys) != 1 {
		return verr("vless_encryption.private_key: exactly one server key is supported")
	}
	e.PrivateKey = keys[0]
	if raw, err := base64.RawURLEncoding.DecodeString(keys[0]); err == nil && len(raw) == 64 {
		e.Auth = EncAuthMLKEM
	} else {
		e.Auth = EncAuthX25519
	}
	return nil
}

// VLESSEncClientKey derives the client key from a server key.
func VLESSEncClientKey(auth, priv string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(priv, "="))
	if err != nil {
		return "", errors.New("must be unpadded base64url")
	}
	switch auth {
	case EncAuthX25519:
		if len(raw) != 32 {
			return "", errors.New("an X25519 private key is 32 bytes")
		}
		k, err := ecdh.X25519().NewPrivateKey(raw)
		if err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), nil
	case EncAuthMLKEM:
		if len(raw) != mlkem768.KeySeedSize {
			return "", errors.New("an ML-KEM-768 seed is 64 bytes")
		}
		pk, _ := mlkem768.NewKeyFromSeed(raw)
		b, err := pk.MarshalBinary()
		if err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(b), nil
	}
	return "", fmt.Errorf("unknown auth %q", auth)
}

// XrayBinary is the Xray-core binary key generation tries first ("" = Go
// only). Set once at startup.
var XrayBinary string

// VLESSEncKeys is a generated server/client key pair.
type VLESSEncKeys struct {
	Auth       string `json:"auth"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	// Source is "xray" when the installed Xray-core made the pair, "go"
	// when the panel did.
	Source string `json:"source"`
}

// GenerateVLESSEncKeys makes a key pair the way `xray x25519` /
// `xray mlkem768` do. It asks the Xray binary at bin first and checks its
// answer against the panel's own derivation; without a usable binary (or
// on any mismatch) it generates the pair itself.
func GenerateVLESSEncKeys(bin, auth string) (VLESSEncKeys, error) {
	if !in(auth, EncAuths) {
		return VLESSEncKeys{}, verr("auth must be one of %s", strings.Join(EncAuths, ", "))
	}
	if bin != "" {
		if k, ok := xrayKeys(bin, auth); ok {
			return k, nil
		}
	}
	var priv []byte
	switch auth {
	case EncAuthX25519:
		priv = make([]byte, 32)
		if _, err := rand.Read(priv); err != nil {
			return VLESSEncKeys{}, err
		}
		// Clamp like Xray so the stored key is the one it prints.
		priv[0] &= 248
		priv[31] &= 127
		priv[31] |= 64
	case EncAuthMLKEM:
		priv = make([]byte, mlkem768.KeySeedSize)
		if _, err := rand.Read(priv); err != nil {
			return VLESSEncKeys{}, err
		}
	}
	p := base64.RawURLEncoding.EncodeToString(priv)
	pub, err := VLESSEncClientKey(auth, p)
	if err != nil {
		return VLESSEncKeys{}, err
	}
	return VLESSEncKeys{Auth: auth, PrivateKey: p, PublicKey: pub, Source: "go"}, nil
}

// xrayKeys runs `xray x25519` or `xray mlkem768` and reads its pair.
func xrayKeys(bin, auth string) (VLESSEncKeys, bool) {
	if st, err := os.Stat(bin); err != nil || st.IsDir() {
		return VLESSEncKeys{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, auth).Output()
	if err != nil {
		return VLESSEncKeys{}, false
	}
	var priv, pub string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 8192), 1<<16)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch {
		case k == "privatekey" || k == "private key" || k == "seed":
			priv = v
		case strings.HasPrefix(k, "password") || k == "public key" || k == "client":
			pub = v
		}
	}
	if priv == "" || pub == "" {
		return VLESSEncKeys{}, false
	}
	mine, err := VLESSEncClientKey(auth, priv)
	if err != nil || mine != pub {
		// Never log the keys themselves.
		slog.Warn("xray key generation did not match the panel's derivation; using the panel's own", "auth", auth)
		return VLESSEncKeys{}, false
	}
	return VLESSEncKeys{Auth: auth, PrivateKey: priv, PublicKey: pub, Source: "xray"}, true
}

// VersionAtLeast compares dotted numeric versions ("26.10.31" >= "25.9.5").
// An empty or unparsable version counts as unknown (true).
func VersionAtLeast(v, min string) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return true
	}
	a, b := strings.Split(v, "."), strings.Split(min, ".")
	for i := 0; i < len(b); i++ {
		var x int
		if i < len(a) {
			n, err := strconv.Atoi(strings.SplitN(a[i], "-", 2)[0])
			if err != nil {
				return true
			}
			x = n
		}
		y, _ := strconv.Atoi(b[i])
		if x != y {
			return x > y
		}
	}
	return true
}
