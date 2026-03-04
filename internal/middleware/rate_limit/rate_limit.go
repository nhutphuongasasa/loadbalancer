package rate_limit

import (
	"context"
	"log/slog"
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
	tokenPerSecond rate.Limit
	limitBucket    int
	cache          *cache.CacheClient

	trustedProxies TrustedProxies

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

func NewIPRateLimiter(r rate.Limit, b int, logger *slog.Logger, opts ...Option) *ipRateLimiter {
	lim := &ipRateLimiter{
		ips:            make(map[string]*client),
		tokenPerSecond: r,
		limitBucket:    b,
		logger:         logger,
	}

	for _, opt := range opts {
		opt(lim)
	}

	if lim.logger == nil {
		lim.logger = slog.Default()
	}

	return lim
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
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
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

/*
*Lay thong tin cua 1 IP neu chua co thi khoi tao doi tuong
 */
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
