package inbounds

import "crypto/sha256"

func sha256Sum(b []byte) [32]byte { return sha256.Sum256(append([]byte("veilon-ss2022:"), b...)) }
