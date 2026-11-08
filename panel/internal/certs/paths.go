package certs

import (
	"fmt"
	"path/filepath"
)

// Paths returns the fullchain and private key file paths of a certificate.
func Paths(dataDir string, id int64) (certFile, keyFile string) {
	d := filepath.Join(dataDir, "certs", fmt.Sprintf("%d", id))
	return filepath.Join(d, "fullchain.pem"), filepath.Join(d, "privkey.pem")
}
