package veiloncore

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// socksBackend carries connections accepted by the local SOCKS5 server.
type socksBackend interface {
	DialTCP(ctx context.Context, addr string) (net.Conn, error)
	// ListenPacket returns an unconnected packet conn for UDP ASSOCIATE,
	// or an error when the backend cannot carry UDP.
	ListenPacket(ctx context.Context) (net.PacketConn, error)
	// ResolveUDP resolves a host for UDP datagrams.
	ResolveUDP(ctx context.Context, host string, port int) (net.Addr, error)
}

// socksServer is a minimal SOCKS5 server (CONNECT + UDP ASSOCIATE) with
// mandatory username/password auth, listening on 127.0.0.1.
type socksServer struct {
	ln      net.Listener
	user    string
	pass    string
	backend socksBackend
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func listenLocal() (net.Listener, int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	return ln, ln.Addr().(*net.TCPAddr).Port, nil
}

func freePort() (int, error) {
	ln, port, err := listenLocal()
	if err != nil {
		return 0, err
	}
	ln.Close()
	return port, nil
}

func startSocksServer(backend socksBackend) (*socksServer, error) {
	ln, _, err := listenLocal()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &socksServer{ln: ln, user: randomToken(8), pass: randomToken(16), backend: backend, ctx: ctx, cancel: cancel}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

func (s *socksServer) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *socksServer) Close() error {
	s.cancel()
	err := s.ln.Close()
	s.wg.Wait()
	return err
}

func (s *socksServer) serve() {
	defer s.wg.Done()
	defer recoverGo("socks sidecar accept")
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func readAddr(r io.Reader, atyp byte) (string, error) {
	var host string
	switch atyp {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 3:
		l := make([]byte, 1)
		if _, err := io.ReadFull(r, l); err != nil {
			return "", err
		}
		b := make([]byte, int(l[0]))
		if _, err := io.ReadFull(r, b); err != nil {
			return "", err
		}
		host = string(b)
	default:
		return "", fmt.Errorf("socks: bad atyp %d", atyp)
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(r, pb); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb)))), nil
}

func encodeAddr(addr net.Addr) []byte {
	var ip net.IP
	var port int
	switch a := addr.(type) {
	case *net.UDPAddr:
		ip, port = a.IP, a.Port
	case *net.TCPAddr:
		ip, port = a.IP, a.Port
	default:
		h, ps, _ := net.SplitHostPort(addr.String())
		ip = net.ParseIP(h)
		port, _ = strconv.Atoi(ps)
	}
	var out []byte
	if v4 := ip.To4(); v4 != nil {
		out = append([]byte{1}, v4...)
	} else if ip != nil {
		out = append([]byte{4}, ip.To16()...)
	} else {
		out = []byte{1, 0, 0, 0, 0}
	}
	return binary.BigEndian.AppendUint16(out, uint16(port))
}

func (s *socksServer) handle(c net.Conn) {
	defer recoverGo("socks sidecar")
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(20 * time.Second))
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil || hdr[0] != 5 {
		return
	}
	methods := make([]byte, int(hdr[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	hasAuth := false
	for _, m := range methods {
		if m == 2 {
			hasAuth = true
		}
	}
	if !hasAuth {
		_, _ = c.Write([]byte{5, 0xff})
		return
	}
	_, _ = c.Write([]byte{5, 2})
	// RFC 1929
	vb := make([]byte, 2)
	if _, err := io.ReadFull(c, vb); err != nil {
		return
	}
	u := make([]byte, int(vb[1]))
	if _, err := io.ReadFull(c, u); err != nil {
		return
	}
	pl := make([]byte, 1)
	if _, err := io.ReadFull(c, pl); err != nil {
		return
	}
	pw := make([]byte, int(pl[0]))
	if _, err := io.ReadFull(c, pw); err != nil {
		return
	}
	if string(u) != s.user || string(pw) != s.pass {
		_, _ = c.Write([]byte{1, 1})
		return
	}
	_, _ = c.Write([]byte{1, 0})
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil || req[0] != 5 {
		return
	}
	addr, err := readAddr(c, req[3])
	if err != nil {
		return
	}
	switch req[1] {
	case 1: // CONNECT
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		rc, err := s.backend.DialTCP(ctx, addr)
		cancel()
		if err != nil {
			_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
			return
		}
		defer rc.Close()
		_, _ = c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		_ = c.SetDeadline(time.Time{})
		pipe(c, rc)
	case 3: // UDP ASSOCIATE
		s.associate(c)
	default:
		_, _ = c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
	}
}

func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		defer func() { done <- struct{}{} }()
		defer recoverGo("socks pipe")
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.SetReadDeadline(time.Now().Add(5 * time.Second))
		}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	<-done
}

func (s *socksServer) associate(c net.Conn) {
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	remote, err := s.backend.ListenPacket(ctx)
	if err != nil {
		_, _ = c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	local, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		_, _ = c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer local.Close()
	reply := append([]byte{5, 0, 0}, encodeAddr(local.LocalAddr())...)
	if _, err := c.Write(reply); err != nil {
		return
	}
	_ = c.SetDeadline(time.Time{})
	var clientMu sync.Mutex
	var client *net.UDPAddr
	go func() {
		defer recoverGo("socks udp control")
		// The association lives as long as the TCP control connection.
		_, _ = io.Copy(io.Discard, c)
		cancel()
		local.Close()
		remote.Close()
	}()
	go func() {
		defer recoverGo("socks udp reader")
		buf := make([]byte, 65535)
		for {
			n, from, err := remote.ReadFrom(buf)
			if err != nil {
				return
			}
			clientMu.Lock()
			ca := client
			clientMu.Unlock()
			if ca == nil {
				continue
			}
			pkt := append([]byte{0, 0, 0}, encodeAddr(from)...)
			pkt = append(pkt, buf[:n]...)
			_, _ = local.WriteToUDP(pkt, ca)
		}
	}()
	buf := make([]byte, 65535)
	for {
		n, from, err := local.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n < 4 || buf[2] != 0 { // fragments unsupported
			continue
		}
		clientMu.Lock()
		client = from
		clientMu.Unlock()
		r := &sliceReader{b: buf[4:n]}
		dst, err := readAddr(r, buf[3])
		if err != nil {
			continue
		}
		h, ps, _ := net.SplitHostPort(dst)
		port, _ := strconv.Atoi(ps)
		ra, err := s.backend.ResolveUDP(ctx, h, port)
		if err != nil {
			continue
		}
		_, _ = remote.WriteTo(r.b, ra)
	}
}

type sliceReader struct{ b []byte }

func (r *sliceReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

var errNoUDP = errors.New("udp not supported")
