package memory

import (
	"errors"
	"fmt"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
	"github.com/nhutphuongasasa/loadbalancer/internal/resilience"
)

//can dam bao lam sao 1 server thuc te chi co 1 instance thoi
/*
*ham thuc hien dang ky 1 server dao danh sach server dang quan li
 */
func (r *InMemoryRegistry) Register(srv *model.Server) error {
	if srv.InstanceID == "" || srv.ServiceName == "" || srv.Host == "" || srv.Port <= 0 {
		return errors.New("invalid server data")
	}

	if srv.GetProxy().Transport == nil {
		r.createResilienceProxy(srv)
		r.logger.Warn("Fallback resilient transport created for server", "instance", srv.InstanceID)
	}

	if !r.checkServer(srv) {
		return errors.New("max instances per service reached")
	}

	r.mux.Lock()
	defer r.mux.Unlock()

	r.setupNewInstance(srv)

	r.ensureWorkerForService(srv.ServiceName)

	r.logger.Info("Server registered", "service", srv.ServiceName, "id", srv.InstanceID)
	return nil
}

func (r *InMemoryRegistry) createResilienceProxy(srv *model.Server) {
	breaker := resilience.NewSonyGoBreaker(
		fmt.Sprintf("cb-default-%s", srv.InstanceID),
		3, 5*time.Second, 10*time.Second, r.logger,
	)

	retryPol := resilience.NewExponentialRetry(3, 200*time.Millisecond, 3*time.Second, 0.2, r.logger)

	srv.GetProxy().Transport = resilience.NewResilientTransport(registry.GlobalBaseTransport, breaker, retryPol, r.logger)
}

/*
*Kiem tra server co ton tia va so luong instance da dat toi da chua
 */
func (r *InMemoryRegistry) checkServer(srv *model.Server) bool {
	if _, ok := r.services[srv.ServiceName]; !ok {
		r.services[srv.ServiceName] = make(map[string]*model.Server)
	}

	// Giới hạn số instance per service
	if len(r.services[srv.ServiceName]) >= maxInstancesPerService {
		r.logger.Error("Cannot register new server: max instances reached",
			"service", srv.ServiceName,
			"max_allowed", maxInstancesPerService,
			"current", len(r.services[srv.ServiceName]),
		)
		return false
	}

	return true
}

/*
*khoi tao cac thong so co ban va day voa channel cho server pool
 */
func (r *InMemoryRegistry) setupNewInstance(srv *model.Server) {
	srv.LastHeartbeat = time.Now()
	srv.Health = true
	srv.TTL = 30 * time.Second

	r.services[srv.ServiceName][srv.InstanceID] = srv

	r.updateChan <- srv
}

/*
*Loai bo 1 insatnce ra khoi danh sach quan li
 */
func (r *InMemoryRegistry) Deregister(serviceName, instanceID string) error {
	r.mux.Lock()
	defer r.mux.Unlock()

	if instances, ok := r.services[serviceName]; ok {
		if srv, exists := instances[instanceID]; exists {
			delete(instances, instanceID)
			if len(instances) == 0 {
				delete(r.services, serviceName)
			}

			r.updateChan <- srv

			if len(instances) == 0 {
				r.removeWorkerLocked(serviceName)
			}

			r.logger.Info("Server deregistered", "service", serviceName, "id", instanceID)
			return nil
		}
	}
	return errors.New("server not found")
}
