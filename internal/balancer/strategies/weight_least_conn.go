package strategies

import (
	"math"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

type WeightedLeastConnections struct{}

func NewWeightedLeastConnections() Strategy {
	return &WeightedLeastConnections{}
}

func (w *WeightedLeastConnections) Pick(servers []*model.Server, _ string) *model.Server {
	if len(servers) == 0 {
		return nil
	}

	var best *model.Server
	minScore := math.MaxFloat64

	for _, srv := range servers {
		if !srv.IsHealthy() {
			continue
		}

		weight := float64(srv.GetWeight())
		if weight <= 0 {
			weight = 1
		}

		conns := float64(srv.GetActiveConns())
		score := conns / weight

		if score < minScore {
			minScore = score
			best = srv
			continue
		}

		// Tie-breaker: nếu score bằng nhau, ưu tiên server có weight cao hơn
		if score == minScore && best != nil && srv.GetWeight() > best.GetWeight() {
			best = srv
		}
	}

	if best == nil && len(servers) > 0 {
		return servers[0] // fallback
	}

	return best
}
