package strategies

import (
	"sync"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

type wrrEntry struct {
	server  *model.Server
	weight  int64
	current int64 // plain int64, không cần atomic
}

type view struct {
	entries     []*wrrEntry
	totalWeight int64
}

type WeightedRoundRobin struct {
	mu   sync.Mutex // bảo vệ cả Pick lẫn Update
	view *view      // plain pointer, không cần atomic.Pointer
}

func NewWeightedRoundRobin() Strategy {
	return &WeightedRoundRobin{}
}

// Update: chỉ chạy ở single-writer (applyStateChange)
func (w *WeightedRoundRobin) Update(backends []*model.Server) {
	entries := make([]*wrrEntry, 0, len(backends))
	var total int64

	// Build new view + preserve currentWeight (giữ smoothness)
	oldView := func() *view {
		w.mu.Lock()
		v := w.view
		w.mu.Unlock()
		return v
	}()

	preserve := make(map[string]int64, len(backends))
	if oldView != nil {
		for _, e := range oldView.entries {
			preserve[e.server.GetID()] = e.current
		}
	}

	for _, s := range backends {
		if !s.IsHealthy() {
			continue
		}
		wt := int64(s.GetWeight())
		if wt <= 0 {
			wt = 1
		}
		total += wt

		entry := &wrrEntry{
			server:  s,
			weight:  wt,
			current: 0,
		}
		if oldCW, ok := preserve[s.GetID()]; ok {
			entry.current = oldCW
		}

		entries = append(entries, entry)
	}

	newView := &view{
		entries:     entries,
		totalWeight: total,
	}

	// Swap view dưới lock để tránh race với Pick
	w.mu.Lock()
	w.view = newView
	w.mu.Unlock()
}

// Pick: hoàn toàn đúng toán học, không drift, lock time cực ngắn
func (w *WeightedRoundRobin) Pick(_ string) *model.Server {
	w.mu.Lock()
	defer w.mu.Unlock()

	v := w.view
	if v == nil || len(v.entries) == 0 {
		return nil
	}

	// Fast path: chỉ 1 server
	if len(v.entries) == 1 {
		return v.entries[0].server
	}

	// === TOÁN HỌC SMOOTH WRR ĐÚNG 100% ===
	var best *wrrEntry
	bestVal := int64(-1 << 63) // MinInt64

	for _, e := range v.entries {
		e.current += e.weight // plain += (nhanh, không atomic)
		if e.current > bestVal {
			bestVal = e.current
			best = e
		}
	}

	if best != nil {
		best.current -= v.totalWeight
	}

	return best.server
}
