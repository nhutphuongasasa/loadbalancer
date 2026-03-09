package rate_limit

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
	"golang.org/x/time/rate"
)

type IRateLimiter interface {
	Start()
	Stop()
	Middleware(next http.Handler) http.Handler
}

type ipRateLimiter struct {
	ips        map[string]*client
	mux        sync.RWMutex
	cfgManager *config.ConfigManager

	cleanupInterval time.Duration
	cleanupTTL      time.Duration

	ctx      context.Context
	cancel   context.CancelFunc
	startOne sync.Once
	stopOne  sync.Once
	wg       sync.WaitGroup
	logger   *slog.Logger
}

type client struct {
	limiter  *rate.Limiter
	lastSeen atomic.Int64
}

func (c *client) updateLastSeen() {
	c.lastSeen.Store(time.Now().UnixNano())
}

func (c *client) getLastSeen() time.Time {
	return time.Unix(0, c.lastSeen.Load())
}

func NewIPRateLimiter(cfgManager *config.ConfigManager, logger *slog.Logger) *ipRateLimiter {
	if logger == nil {
		logger = slog.Default()
	}

	cfg := cfgManager.GetRateLimitConfig()

	cleanupInterval := cfg.CleanupInterval
	cleanupTTL := cfg.CleanupTTL
	if cleanupInterval <= 0 {
		cleanupInterval = 60 * time.Second
	}
	if cleanupTTL <= 0 {
		cleanupTTL = 3 * time.Minute
	}

	return &ipRateLimiter{
		ips:             make(map[string]*client),
		cfgManager:      cfgManager,
		cleanupInterval: cleanupInterval,
		cleanupTTL:      cleanupTTL,
		logger:          logger,
	}
}

/*
*Khoi dong IP rate limiter, tao context va goroutine de xoa cac IP khong su dung trong 3 phut
 */
func (i *ipRateLimiter) Start() {
	i.startOne.Do(func() {
		i.ctx, i.cancel = context.WithCancel(context.Background())
		i.wg.Add(1)
		i.logger.Info("Starting cleanup")
		go i.cleanUp()
		i.logger.Info("Complete starting cleanup")
	})
}

/*
*Top cleanup, huy context va doi cho goroutine ket thuc
 */
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

/*
*Xoa cac IP khong su dung trong 3 phut, chay moi 60 giay de kiem tra mot lan
 */
func (i *ipRateLimiter) cleanUp() {
	ticker := time.NewTicker(i.cleanupInterval)
	defer ticker.Stop()
	defer i.wg.Done()

	for {
		select {
		case <-ticker.C:
			i.mux.Lock()
			for ip, value := range i.ips {
				if time.Since(value.getLastSeen()) > i.cleanupTTL {
					delete(i.ips, ip)
				}
			}
			i.mux.Unlock()

		case <-i.ctx.Done():
			return
		}
	}
}

/*
*Lay thong tin cua 1 IP neu chua co thi khoi tao doi tuong
 */
func (i *ipRateLimiter) GetLimiter(ip string) *client {
	i.mux.RLock()
	value, ok := i.ips[ip]
	i.mux.RUnlock()

	if ok {
		value.updateLastSeen()
		return value
	}

	i.mux.Lock()
	defer i.mux.Unlock()

	// double-check sau khi có write lock
	if value, ok = i.ips[ip]; ok {
		value.updateLastSeen()
		return value
	}

	cfg := i.cfgManager.GetRateLimitConfig()
	rps := rate.Limit(cfg.RequestsPerSecond)
	burst := cfg.Burst
	if rps <= 0 {
		rps = 10
	}
	if burst <= 0 {
		burst = 20
	}

	value = &client{
		limiter: rate.NewLimiter(rps, burst),
	}
	value.updateLastSeen()
	i.ips[ip] = value
	return value
}
