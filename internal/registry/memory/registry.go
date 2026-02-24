package memory

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/health"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry/provider"
)

type InMemoryRegistry struct {
	mux             sync.RWMutex
	services        map[string]map[string]*model.Server
	updateChan      chan *model.Server
	workers         map[string]*workerState
	workersMux      sync.Mutex
	checkInterval   time.Duration
	providerChannel provider.ProviderChannel

	logger *slog.Logger

	startOne sync.Once
	stopOne  sync.Once
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

type workerState struct {
	checker   *health.HeathChecker
	lastIndex int // vi tri bat dau cho batch tiep theo
}

func NewInMemoryRegistry(logger *slog.Logger, checkInterval time.Duration, providerChannel provider.ProviderChannel) *InMemoryRegistry {
	if logger == nil {
		logger = slog.Default()
	}

	if checkInterval == 0 {
		checkInterval = 10 * time.Second
	}

	reg := &InMemoryRegistry{
		services:        make(map[string]map[string]*model.Server),
		updateChan:      make(chan *model.Server, 64),
		logger:          logger,
		workers:         make(map[string]*workerState),
		checkInterval:   checkInterval,
		providerChannel: providerChannel,
	}

	reg.ctx, reg.cancel = context.WithCancel(context.Background())
	return reg
}

/*
* ham thuc hien update lao thong tin instance co trong danh sach
 */
func (r *InMemoryRegistry) UpdateStatus(srv *model.Server, alive bool) {
	r.mux.Lock()
	defer r.mux.Unlock()

	if instances, ok := r.services[srv.ServiceName]; ok {
		if existing, exists := instances[srv.InstanceID]; exists {
			wasHealthy := existing.IsHealthy()
			existing.SetAlive(alive)

			if wasHealthy != alive {
				r.updateChan <- existing
				r.logger.Info("Health state changed",
					"service", srv.ServiceName,
					"id", srv.InstanceID,
					"from", wasHealthy,
					"to", alive,
				)
			}
		}
	}
}

func (r *InMemoryRegistry) Start() {
	r.startOne.Do(func() {
		r.ctx, r.cancel = context.WithCancel(context.Background())
		r.wg.Add(1)
		go r.ServerGate()
		r.logger.Info("Start registry successfully")
	})
}

func (i *InMemoryRegistry) Stop() {
	i.stopOne.Do(func() {
		if i.cancel != nil {
			i.logger.Info("Starting stop registry")
			i.cancel()
		}
		i.wg.Wait()
		i.logger.Info("Complete stop registry")
	})
}

func (r *InMemoryRegistry) ServerGate() {
	defer r.wg.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case srv := <-r.providerChannel:
			r.Register(srv)
			r.logger.Debug("List of server registry")
		}
	}
}

/*
*Duyet danh sach instance cua 1 loai server name de kiem tra xem nhung instance nao con song
 */
func (r *InMemoryRegistry) Discover(serviceName string) ([]*model.Server, error) {
	r.mux.RLock()
	defer r.mux.RUnlock()

	instances, ok := r.services[serviceName]
	if !ok || len(instances) == 0 {
		return nil, errors.New("no servers")
	}

	var healthy []*model.Server
	for _, srv := range instances {
		if srv.IsHealthy() {
			healthy = append(healthy, srv)
		}
	}

	if len(healthy) == 0 {
		return nil, errors.New("no healthy servers")
	}

	return healthy, nil
}

/*
*Tra ve channel de health checker gui thong tin alive cua instance
 */
func (r *InMemoryRegistry) GetUpdateChan() <-chan *model.Server {
	return r.updateChan
}
