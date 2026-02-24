// internal/server/serverpool.go
package server

import (
	"log/slog"
	"sync"

	"github.com/nhutphuongasasa/loadbalancer/internal/balancer/strategies"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
	"go.uber.org/atomic"
)

type subPool struct {
	backends []*model.Server
	strategy strategies.Strategy
}

type ServerPool struct {
	healthyAtomic   atomic.Value // *map[string]*subPool
	logger          *slog.Logger
	updateChan      <-chan *model.Server
	strategyFactory func(serviceName string) strategies.Strategy
	done            chan struct{}
	mu              sync.RWMutex
}

func NewServerPool(
	logger *slog.Logger,
	updateChan <-chan *model.Server,
	strategyFactory func(string) strategies.Strategy,
) *ServerPool {
	pool := &ServerPool{
		logger:          logger,
		updateChan:      updateChan,
		strategyFactory: strategyFactory,
		done:            make(chan struct{}),
	}

	initialMap := make(map[string]*subPool)
	pool.healthyAtomic.Store(&initialMap)

	go pool.listenUpdates()

	return pool
}

func (p *ServerPool) listenUpdates() {
	for {
		select {
		case <-p.done:
			return
		case srv, ok := <-p.updateChan:
			if !ok {
				return
			}
			p.applyStateChange(srv)
		}
	}
}

func (p *ServerPool) applyStateChange(srv *model.Server) {
	p.mu.Lock()
	defer p.mu.Unlock()

	currentPtr := p.healthyAtomic.Load().(*map[string]*subPool)
	current := *currentPtr

	newMap := make(map[string]*subPool)
	for k, v := range current {
		newMap[k] = v
	}

	svcName := srv.ServiceName
	oldSub, exists := newMap[svcName]

	var strategy strategies.Strategy
	var oldBackends []*model.Server // Tạo biến đệm

	if exists {
		strategy = oldSub.strategy
		oldBackends = oldSub.backends // Lấy danh sách cũ
	} else {
		strategy = p.strategyFactory(svcName)
		oldBackends = []*model.Server{} // Service mới thì danh sách cũ rỗng
	}

	newList := make([]*model.Server, 0)
	found := false

	// Duyệt trên oldBackends an toàn hơn
	for _, e := range oldBackends {
		if e.GetID() == srv.GetID() {
			found = true
			if srv.IsHealthy() {
				newList = append(newList, srv)
			}
		} else {
			newList = append(newList, e)
		}
	}

	if !found && srv.IsHealthy() {
		newList = append(newList, srv)
	}

	newMap[svcName] = &subPool{
		backends: newList,
		strategy: strategy,
	}

	p.healthyAtomic.Store(&newMap)

	p.logger.Debug("ServerPool updated",
		"service", svcName,
		"total_healthy", len(newList),
		"server_id", srv.GetID(),
		"is_healthy", srv.IsHealthy(),
	)
}

func (p *ServerPool) PickBackend(serviceName string, clientIP string) *model.Server {
	currentPtr := p.healthyAtomic.Load().(*map[string]*subPool)
	current := *currentPtr

	sub, ok := current[serviceName]
	if !ok || len(sub.backends) == 0 {
		p.logger.Warn("No healthy servers for service", "service", serviceName)
		return nil
	}

	return sub.strategy.Pick(sub.backends, clientIP)
}

func (p *ServerPool) GetInstanceServer(serviceName, instanceId string) *model.Server {
	currentPtr := p.healthyAtomic.Load().(*map[string]*subPool)
	current := *currentPtr

	sub, ok := current[serviceName]
	if !ok || len(sub.backends) == 0 {
		return nil
	}

	for _, srv := range sub.backends {
		if srv.GetID() == instanceId {
			if srv.IsHealthy() {
				return srv
			}
			return nil
		}
	}

	return nil
}
func (p *ServerPool) Close() {
	p.logger.Info("Closing server pool")
	close(p.done)
}
