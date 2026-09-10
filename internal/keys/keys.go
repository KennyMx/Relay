package keys

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const Prefix = "rl_live_"

func New() (raw, hash string, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	raw = Prefix + hex.EncodeToString(b[:])
	hash, _ = Hash(raw)
	return
}
func Hash(raw string) (string, error) {
	if !strings.HasPrefix(raw, Prefix) || len(raw) != len(Prefix)+64 {
		return "", errors.New("invalid API key")
	}
	if _, err := hex.DecodeString(raw[len(Prefix):]); err != nil {
		return "", errors.New("invalid API key")
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]), nil
}
func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
