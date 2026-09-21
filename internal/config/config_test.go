package config

import "testing"

func TestConfig(t *testing.T) {
	t.Setenv("RELAY_CLASSIFIER", "local")
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

func TestAutoConfig(t *testing.T) {
	t.Setenv("RELAY_CLASSIFIER", "local")
	c, err := Load("../../config/relay.json")
	if err != nil {
		t.Fatal(err)
	}
	c.AutoRouting.Routes["simple"] = "missing"
	if c.Validate() == nil {
		t.Fatal("invalid auto route accepted")
	}
	c, _ = Load("../../config/relay.json")
	t.Setenv("RELAY_CLASSIFIER", "jev")
	t.Setenv("JEV_API_KEY", "")
	if _, err = c.BuildRouter(); err == nil {
		t.Fatal("missing Jev credential accepted")
	}
}
