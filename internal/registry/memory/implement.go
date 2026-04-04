package memory

import (
	"errors"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
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
