package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// InitEnv creates unique local credentials without printing them or replacing
// an existing file. A failed write leaves a file in place rather than risking
// overwriting credentials during a retry.
func InitEnv(path string) error {
	secret := func() (string, error) {
		var b [32]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		return hex.EncodeToString(b[:]), nil
	}
	admin, err := secret()
	if err != nil {
		return err
	}
	database, err := secret()
	if err != nil {
		return err
	}
	// #nosec G304 -- path is an explicit local CLI output, never HTTP input;
	// O_EXCL prevents overwriting files and following a target symlink.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("cannot create credentials file (existing files are never overwritten): %w", err)
	}
	_, writeErr := fmt.Fprintf(f, "RELAY_ADMIN_TOKEN=%s\nPOSTGRES_PASSWORD=%s\nRELAY_PORT=8080\nRELAY_ALLOWED_HOSTS=localhost,127.0.0.1,::1\nOPENAI_API_KEY=\nANTHROPIC_API_KEY=\nCOHERE_API_KEY=\nRELAY_CLASSIFIER=local\nJEV_API_KEY=\n", admin, database)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// Reject known example credentials, even when they satisfy the length rule.
func ValidateAdminToken(token string) error {
	if len(token) < 32 || token != strings.TrimSpace(token) ||
		strings.HasPrefix(token, "local-") || strings.HasPrefix(strings.ToLower(token), "replace-") {
		return fmt.Errorf("RELAY_ADMIN_TOKEN must be at least 32 characters and must not be an example value; generate credentials with relay init")
	}
	return nil
}
