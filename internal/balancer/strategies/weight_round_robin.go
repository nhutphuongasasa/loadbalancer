package strategies

import (
	"math"
	"sync"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

type WeightedServer struct {
	server          *model.Server
	effectiveWeight int // điều chỉnh theo health
	currentWeight   int // biến đổi trong quá trình chọn
}

type WeightedRoundRobin struct {
	mu sync.Mutex // bảo vệ danh sách weighted servers nếu cần rebuild
}

func NewWeightedRoundRobin() Strategy {
	return &WeightedRoundRobin{}
}

// Pick chọn server theo smooth weighted round-robin
func (w *WeightedRoundRobin) Pick(backends []*model.Server, _ string) *model.Server {
	if len(backends) == 0 {
		return nil
	}

	var totalWeight int
	weighted := make([]*WeightedServer, 0, len(backends))

	for _, s := range backends {
		if !s.IsHealthy() {
			continue // hoặc giảm weight thay vì skip hoàn toàn
		}

		weight := s.GetWeight()
		if weight <= 0 {
			weight = 1
		}

		ws := &WeightedServer{
			server:          s,
			effectiveWeight: weight,
			currentWeight:   weight, // khởi tạo ban đầu = weight
		}
		weighted = append(weighted, ws)
		totalWeight += ws.effectiveWeight
	}

	if len(weighted) == 0 || totalWeight == 0 {
		// fallback: chọn server đầu tiên hoặc random, tùy bạn
		if len(backends) > 0 {
			return backends[0]
		}
		return nil
	}

	var best *WeightedServer
	var bestCurrentWeight = math.MinInt

	w.mu.Lock()
	defer w.mu.Unlock()

	// Tăng currentWeight cho tất cả và tìm max
	for _, ws := range weighted {
		ws.currentWeight += ws.effectiveWeight

		if ws.currentWeight > bestCurrentWeight {
			best = ws
			bestCurrentWeight = ws.currentWeight
		}
	}

	if best == nil {
		return nil
	}

	// Chọn và trừ tổng weight để "smooth"
	best.currentWeight -= totalWeight

	return best.server
}
