package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/stratum/gateway/internal/schema"
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter provides in-memory request throttling by API key or client IP.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    rate.Limit
	burst    int
	ttl      time.Duration
}

// NewRateLimiter creates a rate limiter.
func NewRateLimiter(rpm int, burst int) *RateLimiter {
	if rpm <= 0 {
		return nil
	}
	if burst <= 0 {
		burst = 1
	}
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		limit:    rate.Limit(float64(rpm) / 60.0),
		burst:    burst,
		ttl:      10 * time.Minute,
	}
	return rl
}

// Middleware enforces request rate limits.
func (r *RateLimiter) Middleware() gin.HandlerFunc {
	if r == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		key := limiterKey(c)
		if !r.allow(key) {
			schema.AbortWithError(c, http.StatusTooManyRequests, "rate_limit_error", "Rate limit exceeded")
			return
		}
		c.Next()
	}
}

func (r *RateLimiter) allow(key string) bool {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()

	if v, ok := r.visitors[key]; ok {
		v.lastSeen = now
		return v.limiter.Allow()
	}
	lim := rate.NewLimiter(r.limit, r.burst)
	r.visitors[key] = &visitor{limiter: lim, lastSeen: now}

	// Opportunistic cleanup.
	if len(r.visitors) > 4096 {
		cutoff := now.Add(-r.ttl)
		for k, v := range r.visitors {
			if v.lastSeen.Before(cutoff) {
				delete(r.visitors, k)
			}
		}
	}
	return lim.Allow()
}

func limiterKey(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if token != "" {
			return "k:" + token
		}
	}
	return "ip:" + c.ClientIP()
}
