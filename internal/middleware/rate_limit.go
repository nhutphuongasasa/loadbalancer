package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/cache"
	"golang.org/x/time/rate"
)

type IRateLimiter interface {
	Start()
	Stop()
	Middleware(next http.Handler) http.Handler
}

type ipRateLimiter struct {
	ips            map[string]*client
	mux            sync.RWMutex
	tokenPerSecond rate.Limit //toc do sinh ra token
	limitBucket    int        //so token
	cache          *cache.CacheClient

	ctx      context.Context
	cancel   context.CancelFunc
	startOne sync.Once
	stopOne  sync.Once
	wg       sync.WaitGroup

	logger *slog.Logger
}

type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type Option func(*ipRateLimiter)

func NewIPRateLimiter(r rate.Limit, b int, logger *slog.Logger) *ipRateLimiter {
	return &ipRateLimiter{
		ips:            make(map[string]*client),
		tokenPerSecond: r,
		limitBucket:    b,
		logger:         logger,
	}
}

func (i *ipRateLimiter) Start() {
	i.startOne.Do(func() {
		i.ctx, i.cancel = context.WithCancel(context.Background())
		i.wg.Add(1)
		i.logger.Info("Starting cleanup")
		go i.cleanUp()
		i.logger.Info("Complete starting cleanup")
	})
}

func (i *ipRateLimiter) Stop() {
	i.stopOne.Do(func() {
		if i.cancel != nil {
			i.logger.Info("Starting stop cleanup")
			i.cancel()
		}
		i.wg.Wait()
		i.logger.Info("Complete stop cleanup")
	})
}

func (i *ipRateLimiter) cleanUp() {
	ticker := time.NewTicker(60 * time.Second)
	// defer ticker.Stop()
	defer i.wg.Done()
	for {
		select {
		case <-ticker.C:
			i.mux.Lock()
			for ip, value := range i.ips {
				if time.Since(value.lastSeen) > 3*time.Minute {
					delete(i.ips, ip)
				}
			}
			i.mux.Unlock()
		case <-i.ctx.Done():
			return
		}
	}
}

func (i *ipRateLimiter) GetLimiter(ip string) *client {
	i.mux.Lock()
	defer i.mux.Unlock()

	value, ok := i.ips[ip]
	if !ok {
		value = &client{
			limiter:  rate.NewLimiter(i.tokenPerSecond, i.limitBucket),
			lastSeen: time.Now(),
		}
		i.ips[ip] = value
	}

	value.lastSeen = time.Now()
	return value
}

func (i *ipRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getIpUser(r)

		value := i.GetLimiter(ip)

		if !value.limiter.Allow() {
			i.logger.Warn("Rate limit exceeded",
				"ip", ip,
				"method", r.Method,
				"path", r.URL.Path,
				"user_agent", r.UserAgent(),
			)
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func getIpUser(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}

func (i *ipRateLimiter) GetStats() int {
	i.mux.RLock()
	defer i.mux.RUnlock()
	return len(i.ips)
}
