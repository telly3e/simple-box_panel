package ids

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
)

func NewID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(buf)
}

func NewSecret(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(buf)
}
