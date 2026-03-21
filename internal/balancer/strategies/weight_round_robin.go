package strategies

import (
	"sync"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

type wrrEntry struct {
	server        *model.Server
	weight        int64
	currentWeight int64
}

type view struct {
	entries     []*wrrEntry
	totalWeight int64
}

type WeightedRoundRobin struct {
	mu   sync.Mutex
	view *view
}

func NewWeightedRoundRobin() Strategy {
	return &WeightedRoundRobin{}
}

// thay doi danh sach server
func (w *WeightedRoundRobin) Update(backends []*model.Server) {
	entries := make([]*wrrEntry, 0, len(backends))
	var total int64

	oldView := func() *view {
		w.mu.Lock()
		v := w.view
		w.mu.Unlock()
		return v
	}()

	//luu 1 ban sao current weight cua cac server
	preserve := make(map[string]int64, len(backends))
	if oldView != nil {
		for _, e := range oldView.entries {
			preserve[e.server.GetID()] = e.currentWeight
		}
	}

	//duyet danh sach backend moi nhat duoc cap nhat
	//dua current weight cu vao neu khong co trong danh sach cu thi khoi tao current bang 0
	//dua vao dnah sach entries
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
			server:        s,
			weight:        wt,
			currentWeight: 0,
		}
		if oldCW, ok := preserve[s.GetID()]; ok {
			entry.currentWeight = oldCW
		}

		entries = append(entries, entry)
	}

	newView := &view{
		entries:     entries,
		totalWeight: total,
	}

	// Swap view trong lock de tranh race
	w.mu.Lock()
	w.view = newView
	w.mu.Unlock()
}

func (w *WeightedRoundRobin) Pick(_ string) *model.Server {
	w.mu.Lock()
	defer w.mu.Unlock()

	v := w.view
	if v == nil || len(v.entries) == 0 {
		return nil
	}

	// Fast path khi chi co 1 server
	if len(v.entries) == 1 {
		return v.entries[0].server
	}

	//khoi tao bien bestVal dung for de kiem gia tri lon nhat
	//sau do tru cho tong weight cua ca cum server de no quay ve current weight thap
	//viec chon total weight la co chung minh toan hoc no la 1 con so tot
	var best *wrrEntry
	bestVal := int64(-1 << 63) // MinInt64

	for _, e := range v.entries {
		e.currentWeight += e.weight
		if e.currentWeight > bestVal {
			bestVal = e.currentWeight
			best = e
		}
	}

	if best != nil {
		best.currentWeight -= v.totalWeight
	}

	return best.server
}
