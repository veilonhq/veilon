// Package util contains small helpers shared across packages.
package util

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math/big"
)

const alnum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// RandString returns a cryptographically random alphanumeric string.
func RandString(n int) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(alnum)))
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		b[i] = alnum[v.Int64()]
	}
	return string(b)
}

// RandHex returns n random bytes hex encoded.
func RandHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// UUIDv4 returns a random RFC 4122 version 4 UUID.
func UUIDv4() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// Opt is a JSON field that distinguishes "absent", "null" and a value.
// Use it in PATCH request structs.
type Opt[T any] struct {
	Set  bool
	Null bool
	Val  T
}

// UnmarshalJSON implements json.Unmarshaler.
func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		var zero T
		o.Val = zero
		return nil
	}
	o.Null = false
	return json.Unmarshal(b, &o.Val)
}

// Ptr returns nil when null/absent, else a pointer to the value.
func (o Opt[T]) Ptr() *T {
	if !o.Set || o.Null {
		return nil
	}
	v := o.Val
	return &v
}
