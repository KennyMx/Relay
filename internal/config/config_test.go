package config

import "testing"

func TestConfig(t *testing.T) {
	c, err := Load("../../config/relay.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.BuildRouter(); err != nil {
		t.Fatal(err)
	}
	delete(c.Pricing, "mock/mock-v1")
	if err = c.Validate(); err == nil {
		t.Fatal("accepted missing price")
	}
	c, _ = Load("../../config/relay.json")
	c.MaxAttempts = 100
	if err = c.Validate(); err == nil {
		t.Fatal("unbounded attempts")
	}
}
