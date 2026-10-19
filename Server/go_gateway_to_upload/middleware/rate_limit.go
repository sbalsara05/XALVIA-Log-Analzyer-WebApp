package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// ipLimiter holds a per-IP token bucket limiter and the last time it was seen.
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// limiterStore is a thread-safe map of IP → limiter, with periodic cleanup of
// stale entries so memory doesn't grow unbounded.
type limiterStore struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiter
	r        rate.Limit
	burst    int
}

func newLimiterStore(r rate.Limit, burst int) *limiterStore {
	ls := &limiterStore{
		limiters: make(map[string]*ipLimiter),
		r:        r,
		burst:    burst,
	}
	go ls.cleanup()
	return ls
}

func (ls *limiterStore) get(ip string) *rate.Limiter {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	entry, ok := ls.limiters[ip]
	if !ok {
		entry = &ipLimiter{limiter: rate.NewLimiter(ls.r, ls.burst)}
		ls.limiters[ip] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

// cleanup removes entries that have been idle for more than 10 minutes.
func (ls *limiterStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		ls.mu.Lock()
		for ip, entry := range ls.limiters {
			if time.Since(entry.lastSeen) > 10*time.Minute {
				delete(ls.limiters, ip)
			}
		}
		ls.mu.Unlock()
	}
}

// RateLimit returns a Gin middleware that enforces a per-IP rate limit.
// rps is requests per second; burst is the maximum token bucket depth.
// Use rps=10.0/60 + burst=10 to get "10 requests per minute".
func RateLimit(rps float64, burst int) gin.HandlerFunc {
	store := newLimiterStore(rate.Limit(rps), burst)
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !store.get(ip).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded — try again later",
			})
			return
		}
		c.Next()
	}
}
