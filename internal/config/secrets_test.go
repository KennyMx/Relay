package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := InitEnv(path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credentials need owner-only permissions")
	}
	lines := strings.Split(string(before), "\n")
	admin := strings.TrimPrefix(lines[0], "RELAY_ADMIN_TOKEN=")
	database := strings.TrimPrefix(lines[1], "POSTGRES_PASSWORD=")
	if len(admin) != 64 || len(database) != 64 || admin == database {
		t.Fatal("independent random secrets required")
	}
	if err := ValidateAdminToken(admin); err != nil {
		t.Fatal(err)
	}
	if err := InitEnv(path); err == nil {
		t.Fatal("existing credentials overwritten")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("existing file changed")
	}
	other := filepath.Join(t.TempDir(), ".env")
	if err := InitEnv(other); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(other)
	if string(before) == string(data) {
		t.Fatal("reused credentials")
	}
}

func TestRejectExampleAdminTokens(t *testing.T) {
	for _, value := range []string{"", "short", "local-relay-admin-token-change-me-now", "local-demo-admin-token-change-before-sharing", "replace-with-a-unique-admin-secret-here"} {
		if ValidateAdminToken(value) == nil {
			t.Fatal("accepted insecure admin credential")
		}
	}
}
