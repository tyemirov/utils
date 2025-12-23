package preflight

import (
	"crypto/sha256"
	"encoding/hex"
)

// RedactionMode controls how sensitive fields are reported.
type RedactionMode int

const (
	RedactionModeRedacted RedactionMode = iota + 1
	RedactionModeFull
)

// HashSHA256Hex returns the hex-encoded SHA-256 hash of the payload.
func HashSHA256Hex(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
