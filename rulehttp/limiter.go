package rulehttp

import (
	"sync"
	"time"
)

// NoLimit is a [Limiter] that always allows. Use it only behind another
// limiter or on an internal network; it exists so disabling the default is
// an explicit, greppable choice.
var NoLimit Limiter = allowAll{}

type allowAll struct{}

func (allowAll) Allow(string) bool { return true }

// maxBucketKeys bounds the token bucket's memory: when more distinct
// clients than this are tracked, the stalest entries are evicted. A client
// evicted mid-burst simply starts a fresh bucket, which is a full burst —
// acceptable, since eviction only happens under very wide traffic.
const maxBucketKeys = 10_000

// TokenBucket is an in-memory per-key token bucket: `rate` tokens are added
// per second up to `burst`, and each Allow spends one. Dependency-free on
// purpose so the library stays stdlib-only; a multi-instance deployment
// should put a shared limiter in front and pass [NoLimit] here.
type TokenBucket struct {
	rate  float64
	burst float64
	now   func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewTokenBucket builds a limiter with the given refill rate (per second)
// and burst capacity.
func NewTokenBucket(ratePerSecond, burst float64) *TokenBucket {
	return &TokenBucket{rate: ratePerSecond, burst: burst, now: time.Now, buckets: map[string]*bucket{}}
}

// Allow implements [Limiter].
func (t *TokenBucket) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	b, ok := t.buckets[key]
	if !ok {
		if len(t.buckets) >= maxBucketKeys {
			t.evict(now)
		}
		b = &bucket{tokens: t.burst, last: now}
		t.buckets[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = min(t.burst, b.tokens+elapsed*t.rate)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// evict drops the half of the entries least recently touched. Linear in
// the map size, which is fine at the rate eviction occurs.
func (t *TokenBucket) evict(now time.Time) {
	// Any bucket that has been idle long enough to refill fully carries no
	// information; drop those first.
	fullAfter := time.Duration(t.burst / t.rate * float64(time.Second))
	for k, b := range t.buckets {
		if now.Sub(b.last) >= fullAfter {
			delete(t.buckets, k)
		}
	}
	if len(t.buckets) < maxBucketKeys {
		return
	}
	// Still full of active clients: drop an arbitrary half.
	n := len(t.buckets) / 2
	for k := range t.buckets {
		if n == 0 {
			break
		}
		delete(t.buckets, k)
		n--
	}
}
