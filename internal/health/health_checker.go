package health

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
	"golang.org/x/sync/semaphore"
)

const (
	maxConcurrent = 5
)

type HeathChecker struct {
	client *http.Client
	logger *slog.Logger
}

func NewHeathChecker(logger *slog.Logger) *HeathChecker {
	return &HeathChecker{
		logger: logger,
		client: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

/*
*Nhan dnah sach cna check tu registry  va ham update dnah sach
 */
func (h *HeathChecker) CheckServers(servers []*model.Server, opts func(srv *model.Server, alive bool)) {
	if len(servers) == 0 {
		return
	}

	/*
	*Khoi tao sem quan li so luong conuren
	 */
	sem := semaphore.NewWeighted(int64(maxConcurrent))
	var wg sync.WaitGroup

	for _, srv := range servers {
		wg.Add(1)
		go func(s *model.Server) {
			defer wg.Done()

			if err := sem.Acquire(context.Background(), 1); err != nil {
				h.logger.Warn("Semaphore acquire failed", "err", err)
				return
			}
			defer sem.Release(1)

			alive := h.ping(s.GetAddr())

			changed := alive != s.IsHealthy()

			s.SetAlive(alive)

			if opts != nil && changed {
				h.logger.Debug("Status server changed, start running opts func!")
				opts(s, alive)
			}
		}(srv)
	}

	wg.Wait()
}

// ping cơ bản (private) - không retry, chỉ 1 lần ping
func (h *HeathChecker) ping(addr string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), h.client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		h.logger.Error("Invalid health check request", "addr", addr, "err", err)
		return false
	}

	resp, err := h.client.Do(req)

	if err != nil {
		h.logger.Warn("Server is down", "addr", addr, "err", err)
		return false
	}

	defer resp.Body.Close()

	if resp.StatusCode < 500 {
		h.logger.Debug("Health check OK", "addr", addr, "status", resp.StatusCode)
		return true
	}

	h.logger.Warn("Server responded with error", "addr", addr, "status", resp.StatusCode)
	return false
}
