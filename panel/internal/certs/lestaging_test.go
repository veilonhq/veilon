package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"os"
	"testing"
	"time"
)

// TestLEStagingOrder checks that Let's Encrypt staging accepts a newOrder
// for an IP identifier with the shortlived profile. It does not validate
// (that needs port 80 reachable). Run with VEILON_LE_STAGING_IP=<public ip>.
func TestLEStagingOrder(t *testing.T) {
	ip := os.Getenv("VEILON_LE_STAGING_IP")
	if ip == "" {
		t.Skip("VEILON_LE_STAGING_IP not set")
	}
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	c := &acmeClient{DirectoryURL: "https://acme-staging-v02.api.letsencrypt.org/directory", Key: k}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Register(ctx, ""); err != nil {
		t.Fatal(err)
	}
	o, err := c.NewOrder(ctx, Identifiers(ip), ProfileShortLived)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("order %s status=%s profile=%s ids=%v", o.URL, o.Status, o.Profile, o.Identifiers)
	if o.Profile != ProfileShortLived || len(o.Identifiers) != 1 || o.Identifiers[0].Type != "ip" {
		t.Fatalf("unexpected order: %+v", o)
	}
	z, err := c.Authorization(ctx, o.Authorizations[0])
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, ch := range z.Challenges {
		types = append(types, ch.Type)
	}
	t.Logf("authz %s %s challenges=%v", z.Identifier.Type, z.Identifier.Value, types)
}
