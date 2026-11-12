package veiloncore

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	quic "github.com/sagernet/quic-go"
	"github.com/sagernet/sing-quic/hysteria2"
)

// Certificate pinning.
//
// Veilon publishes TLS inbounds with self-signed certificates as
// "insecure + pin": the share link carries the hex SHA-256 of the leaf
// certificate (pcs / pinSHA256). Xray can pin that hash directly; sing-box
// pins the public key (SPKI). resolvePins performs a handshake, verifies the
// leaf against the published certificate hash and derives the SPKI pin, so
// the connection is never "insecure".

var errPinMismatch = errors.New("server certificate does not match the published pin")

type pinCacheEntry struct {
	spki string
	at   time.Time
}

var (
	pinCacheMu sync.Mutex
	pinCache   = map[string]pinCacheEntry{}
)

func spkiOf(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return b64std(sum[:])
}

func certHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// probeLeaf handshakes with the server and returns its leaf certificate.
func probeLeaf(ctx context.Context, p *Profile) (*x509.Certificate, error) {
	sni := p.SNI
	if sni == "" && net.ParseIP(p.Server) == nil {
		sni = p.Server
	}
	addr := net.JoinHostPort(p.Server, strconv.Itoa(p.Port))
	var leaf *x509.Certificate
	tcfg := &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true, // verified below against the pin
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("no certificate")
			}
			c, err := x509.ParseCertificate(raw[0])
			if err != nil {
				return err
			}
			leaf = c
			return nil
		},
	}
	switch {
	case p.Type == TypeHysteria2 || p.Type == TypeTUIC:
		alpn := p.ALPN
		if len(alpn) == 0 {
			alpn = []string{"h3"}
		}
		tcfg.NextProtos = alpn
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return nil, err
		}
		lpc, err := (&net.ListenConfig{Control: protectControl}).ListenPacket(ctx, "udp", ":0")
		if err != nil {
			return nil, err
		}
		pc := lpc.(*net.UDPConn)
		defer pc.Close()
		var conn net.PacketConn = pc
		if p.Type == TypeHysteria2 && p.ObfsPassword != "" {
			conn = hysteria2.NewSalamanderConn(pc, []byte(p.ObfsPassword))
		}
		qc, err := quic.Dial(ctx, conn, udpAddr, tcfg, &quic.Config{HandshakeIdleTimeout: 8 * time.Second})
		if err != nil {
			if leaf != nil {
				return leaf, nil
			}
			return nil, err
		}
		_ = qc.CloseWithError(0, "")
	default:
		alpn := p.ALPN
		if len(alpn) == 0 {
			alpn = []string{"h2", "http/1.1"}
		}
		tcfg.NextProtos = alpn
		d := protectedDialer(&net.Dialer{Timeout: 8 * time.Second, Resolver: directResolver()})
		c, err := tls.DialWithDialer(d, "tcp", addr, tcfg)
		if err != nil {
			if leaf != nil {
				return leaf, nil
			}
			return nil, err
		}
		c.Close()
	}
	if leaf == nil {
		return nil, errors.New("no certificate received")
	}
	return leaf, nil
}

// resolvePins fills PinSPKI (sing-box) / PinSHA256 (Xray) for insecure TLS
// profiles that publish a certificate pin. Profiles marked insecure without
// a pin are pinned on first use for Xray (which has no insecure mode).
func resolvePins(ctx context.Context, p *Profile, core Core) error {
	if !p.Insecure || p.Security == "reality" {
		return nil
	}
	needsTLS := p.Security == "tls" || p.Type == TypeHysteria2 || p.Type == TypeTUIC || p.Type == TypeMasque
	if !needsTLS {
		return nil
	}
	switch core {
	case CoreSingbox:
		if p.PinSPKI != "" || p.PinSHA256 == "" {
			return nil
		}
	case CoreXray:
		if p.PinSHA256 != "" {
			return nil
		}
	default:
		return nil
	}
	key := p.Server + ":" + strconv.Itoa(p.Port) + "|" + p.PinSHA256
	pinCacheMu.Lock()
	if e, ok := pinCache[key]; ok && time.Since(e.at) < 6*time.Hour && core == CoreSingbox {
		pinCacheMu.Unlock()
		p.PinSPKI = e.spki
		return nil
	}
	pinCacheMu.Unlock()
	leaf, err := probeLeaf(ctx, p)
	if err != nil {
		return fmt.Errorf("certificate check: %w", err)
	}
	h := certHash(leaf.Raw)
	switch core {
	case CoreSingbox:
		if !strings.EqualFold(h, p.PinSHA256) {
			return errPinMismatch
		}
		p.PinSPKI = spkiOf(leaf)
		pinCacheMu.Lock()
		pinCache[key] = pinCacheEntry{spki: p.PinSPKI, at: time.Now()}
		pinCacheMu.Unlock()
	case CoreXray:
		p.PinSHA256 = h
	}
	return nil
}
