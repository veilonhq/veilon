package veiloncore

import (
	"net"
	"time"
)

const defaultDialTimeout = 15 * time.Second

// directResolver resolves names for the cores' own connections. The app is
// excluded from its own VPN, so the system resolver reaches the real network.
func directResolver() *net.Resolver { return net.DefaultResolver }
