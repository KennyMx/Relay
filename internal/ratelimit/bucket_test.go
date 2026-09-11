package ratelimit

import (
	"context"
	"github.com/KennyMx/Relay/internal/keys"
	"github.com/redis/go-redis/v9"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRedisTokenBucket(t *testing.T) {
	if os.Getenv("RELAY_INTEGRATION") != "1" {
		t.Skip("set RELAY_INTEGRATION=1 with REDIS_URL")
	}
	options, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	ctx := context.Background()
	b := Bucket{Client: client}
	id := keys.ID()
	defer client.Del(ctx, "relay:bucket:"+id)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := b.Allow(ctx, id, 1, 10)
			if err != nil {
				t.Error(err)
			}
			if d.Allowed {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 10 {
		t.Fatalf("atomic burst: got %d", allowed.Load())
	}
	d, err := b.Allow(ctx, id, 1, 10)
	if err != nil || d.Allowed || d.RetryAfter <= 0 {
		t.Fatal(d, err)
	}
	other := keys.ID()
	defer client.Del(ctx, "relay:bucket:"+other)
	d, err = b.Allow(ctx, other, 600, 1)
	if err != nil || !d.Allowed {
		t.Fatal("key isolation", d, err)
	}
	d, err = b.Allow(ctx, other, 600, 1)
	if err != nil || d.Allowed {
		t.Fatal("empty bucket", d, err)
	}
	time.Sleep(120 * time.Millisecond)
	d, err = b.Allow(ctx, other, 600, 1)
	if err != nil || !d.Allowed {
		t.Fatal("refill", d, err)
	}
	if ttl := client.PTTL(ctx, "relay:bucket:"+other).Val(); ttl <= 0 {
		t.Fatal("bucket does not expire")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = b.Allow(canceled, other, 600, 1); err == nil {
		t.Fatal("ignored cancellation")
	}
}
