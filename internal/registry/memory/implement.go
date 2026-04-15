package memory

import (
	"context"
	"errors"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
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
		r.logger.Warn("Fallback resilient transport created for server", "instance", srv.InstanceID)
	}

	r.mux.Lock()
	defer r.mux.Unlock()

	if err := r.checkServer(srv); err != nil {
		r.logger.Error(
			"Cannot register server",
			"service", srv.ServiceName,
			"id", srv.InstanceID,
			"err", err,
		)
		return err
	}

	r.setupNewInstance(srv)

	// r.ensureWorkerForService(srv.ServiceName)

	r.updateGlobalInstanceVersionData(registry.VersionDataBackend + 1)
	r.logger.Info("Server registered", "service", srv.ServiceName, "id", srv.InstanceID)
	return nil
}

/*
*Kiem tra server co ton tia va so luong instance da dat toi da chua
 */
func (r *InMemoryRegistry) checkServer(srv *model.Server) error {
	if _, ok := r.services[srv.ServiceName]; !ok {
		r.services[srv.ServiceName] = make(map[string]*model.Server)
	}

	// Gioi han cua  instance per service
	if len(r.services[srv.ServiceName]) >= registry.MaxInstancesPerService {
		return errors.New("max instances per service reached")
	}

	//kiem tra ton tai cua instance Id
	if existing, exists := r.services[srv.ServiceName][srv.InstanceID]; exists {
		if existing.Host != srv.Host || existing.Port != srv.Port {
			return errors.New("instance ID already exists with different host/port")
		}
	}

	return nil
}

/*
*khoi tao cac thong so co ban va day voa channel cho server pool
 */
func (r *InMemoryRegistry) setupNewInstance(srv *model.Server) {
	srv.LastSeen = time.Now()
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

			r.updateGlobalInstanceVersionData(registry.VersionDataBackend + 1)
			r.logger.Info("Server deregistered", "service", serviceName, "id", instanceID)
			return nil
		}
	}
	return errors.New("server not found")
}

/*
* ham thuc hien update lao thong tin instance co trong danh sach
 */
func (r *InMemoryRegistry) UpdateStatus(serviceName, instanceID string, alive bool) {
	r.mux.Lock()
	defer r.mux.Unlock()

	//Lay doi tuong instance va kiem ra
	if instances, ok := r.services[serviceName]; ok {
		if existing, exists := instances[instanceID]; exists {
			wasHealthy := existing.IsHealthy()
			existing.SetAlive(alive)

			// day vao channel de update server_pool
			r.updateChan <- existing
			r.updateGlobalInstanceVersionData(registry.VersionDataBackend + 1)
			r.logger.Debug("Health state changed",
				"service", serviceName,
				"id", instanceID,
				"from", wasHealthy,
				"to", alive,
			)
		}
	}
}

/*
*Tra ve channel de health checker gui thong tin alive cua instance
 */
func (r *InMemoryRegistry) GetUpdateChan() <-chan *model.Server {
	return r.updateChan
}

/*
*Duyet danh sach instance cua 1 loai server name de kiem tra xem nhung instance nao con song
 */
func (r *InMemoryRegistry) Discover(_ context.Context, serviceName string) ([]*model.Server, error) {
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

// Tra ve tat ca server dang quan li
func (r *InMemoryRegistry) ListAll() map[string][]*model.Server {
	r.mux.RLock()
	defer r.mux.RUnlock()

	result := make(map[string][]*model.Server)
	for svcName, instances := range r.services {
		for _, srv := range instances {
			result[svcName] = append(result[svcName], srv)
		}
	}
	return result
}

func (r *InMemoryRegistry) updateGlobalInstanceVersionData(version registry.VersionData) {
	registry.VersionDataBackend = registry.VersionData(version)
}
