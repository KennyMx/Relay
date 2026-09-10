package pricing

import (
	"github.com/KennyMx/Relay/internal/provider"
	"math"
	"testing"
)

func TestEstimate(t *testing.T) {
	table := Table{"p/m": {Input: 150, Output: 600}}
	cost, err := table.Estimate("p", "m", provider.Usage{InputTokens: 1000, OutputTokens: 200})
	if err != nil || cost != 270000 {
		t.Fatal(cost, err)
	}
	for _, u := range []provider.Usage{{InputTokens: -1}, {InputTokens: math.MaxInt64}} {
		if _, err := table.Estimate("p", "m", u); err == nil {
			t.Fatal("accepted invalid usage")
		}
	}
	if _, err := table.Estimate("p", "missing", provider.Usage{}); err == nil {
		t.Fatal("missing price treated as free")
	}
}
