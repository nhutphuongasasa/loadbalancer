package rate_limit

import (
	"encoding/json"
	"net/http"
)

/*
*Thuc hien kiem tra IP va so lan truy cap con lai
 */
func (i *ipRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := RealIP(r, i.cfgManager.GetRateLimitConfig().TrustedProxies)

		cl := i.GetLimiter(key)

		if !cl.limiter.Allow() {
			i.logger.Warn("Rate limit exceeded",
				"key", key,
				"method", r.Method,
				"path", r.URL.Path,
				"user_agent", r.UserAgent(),
			)

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "30")
			w.Header().Set("X-RateLimit-Remaining", "0")

			w.WriteHeader(http.StatusTooManyRequests)

			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":       "Too Many Requests",
				"message":     "You have exceeded the rate limit. Please try again later.",
				"retry_after": 30,
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}
