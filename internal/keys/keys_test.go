package keys

import "testing"

func TestKeys(t *testing.T) {
	raw, hash, err := New()
	if err != nil || raw == hash || len(hash) != 64 {
		t.Fatal(err)
	}
	got, err := Hash(raw)
	if err != nil || got != hash {
		t.Fatal("hash mismatch")
	}
	other, _, _ := New()
	if other == raw {
		t.Fatal("key reuse")
	}
	for _, bad := range []string{"", "rl_live_secret", raw + "x", "Bearer " + raw} {
		if _, err := Hash(bad); err == nil {
			t.Fatal("accepted malformed key")
		}
	}
}
