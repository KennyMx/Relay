// Package ratelimit provides an atomic Redis token bucket measured in requests.
package ratelimit

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"time"
)

// Redis TIME avoids disagreements between gateway clocks. Idle buckets expire
// only after enough time has passed for a full refill.
var script = redis.NewScript(`
local clock = redis.call('TIME')
local now = tonumber(clock[1]) + tonumber(clock[2]) / 1000000
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2]) / 60
local old = redis.call('HMGET', KEYS[1], 'tokens', 'updated')
local tokens = tonumber(old[1]) or capacity
local updated = tonumber(old[2]) or now
tokens = math.min(capacity, tokens + math.max(0, now-updated)*rate)
local allowed = 0
local retry = 0
if tokens >= 1 then
 tokens = tokens-1
 allowed = 1
else
 retry = math.ceil((1-tokens)/rate*1000)
end
redis.call('HSET', KEYS[1], 'tokens', tokens, 'updated', math.max(now,updated))
redis.call('PEXPIRE', KEYS[1], math.ceil(capacity/rate*1000)+1000)
return {allowed, math.floor(tokens), retry}
`)

type Bucket struct{ Client *redis.Client }
type Decision struct {
	Allowed    bool
	Remaining  int64
	RetryAfter time.Duration
}

func (b *Bucket) Allow(ctx context.Context, keyID string, rpm, burst int) (Decision, error) {
	if rpm < 1 || rpm > 100000 || burst < 1 || burst > 100000 {
		return Decision{}, fmt.Errorf("invalid quota")
	}
	values, err := script.Run(ctx, b.Client, []string{"relay:bucket:" + keyID}, burst, rpm).Int64Slice()
	if err != nil {
		return Decision{}, err
	}
	if len(values) != 3 {
		return Decision{}, fmt.Errorf("invalid Redis reply")
	}
	return Decision{Allowed: values[0] == 1, Remaining: values[1], RetryAfter: time.Duration(values[2]) * time.Millisecond}, nil
}
