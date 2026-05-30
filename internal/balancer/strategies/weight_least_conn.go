package strategies

import (
	"sync/atomic"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

type lcView struct {
	servers []*model.Server
}

type WeightedLeastConnections struct {
	ptr atomic.Pointer[lcView]
}

func NewWeightedLeastConnections() Strategy {
	return &WeightedLeastConnections{}
}

// Update: chỉ chạy ở single-writer (applyStateChange) → cực nhanh
func (w *WeightedLeastConnections) Update(backends []*model.Server) {
	if len(backends) == 0 {
		w.ptr.Store(nil)
		return
	}

	// Copy slice để atomic swap an toàn
	servers := make([]*model.Server, len(backends))
	copy(servers, backends)

	w.ptr.Store(&lcView{servers: servers})
}

// Pick: hoàn toàn lock-free, hot-path siêu nhanh, không drift
func (w *WeightedLeastConnections) Pick(_ string) *model.Server {
	view := w.ptr.Load()
	if view == nil || len(view.servers) == 0 {
		return nil
	}

	// Fast path: chỉ 1 server
	if len(view.servers) == 1 {
		return view.servers[0]
	}

	// Weighted Least Connections với integer comparison (chuẩn Nginx/Envoy)
	var best *model.Server
	for _, srv := range view.servers {
		if best == nil {
			best = srv
			continue
		}

		c1 := srv.GetActiveConns()
		w1 := srv.GetWeight()
		c2 := best.GetActiveConns()
		w2 := best.GetWeight()

		if w1 <= 0 {
			w1 = 1
		}
		if w2 <= 0 {
			w2 = 1
		}

		// c1/w1 < c2/w2  <=>  c1*w2 < c2*w1
		mul1 := uint64(c1) * uint64(w2)
		mul2 := uint64(c2) * uint64(w1)

		if mul1 < mul2 || (mul1 == mul2 && w1 > w2) {
			best = srv
		}
	}

	return best
}
