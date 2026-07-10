package utils

import (
	"crypto/sha256"
	"encoding/hex"
)

func HashStrings(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte("\n---\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
