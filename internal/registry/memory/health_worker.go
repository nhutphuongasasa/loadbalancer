package memory

import (
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/health"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
	"golang.org/x/sync/semaphore"
)

/*
*Kiem tra va khoi tao chi 1 worker health checker cho 1 load service name
 */
func (r *InMemoryRegistry) ensureWorkerForService(serviceName string) {
	r.workersMux.Lock()
	defer r.workersMux.Unlock()

	if _, exists := r.workers[serviceName]; exists {
		return
	}

	worker := health.NewHeathChecker(r.logger)
	r.workers[serviceName] = &workerState{
		checker:   worker,
		lastIndex: 0,
	}

	r.wg.Add(1)
	go r.workerLoop(serviceName, worker)

	r.logger.Info("Created health worker for service", "service", serviceName)
}

/*
*Loai bo worker health
 */
func (r *InMemoryRegistry) removeWorkerLocked(serviceName string) {
	if _, exists := r.workers[serviceName]; exists {
		delete(r.workers, serviceName)
		r.logger.Info("Removed health worker for empty service", "service", serviceName)
	}
}

/*
*Thuc hien vong lap de health checker hoat dong
 */
func (r *InMemoryRegistry) workerLoop(serviceName string, worker *health.HeathChecker) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	sem := semaphore.NewWeighted(int64(maxConcurrentCheck))

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			batch := r.extractBatch(serviceName)
			if len(batch) == 0 {
				r.logger.Info("Worker stopped: service empty", "service", serviceName)
				r.removeWorkerLocked(serviceName)
				return
			}

			r.executeBatchCheck(batch, worker, sem)
		}
	}
}

/*
* chia cac instance cung 1 loai server name thanh batch dua vao index
 */
func (r *InMemoryRegistry) extractBatch(serviceName string) []*model.Server {
	r.mux.RLock()
	instances, ok := r.services[serviceName]
	if !ok || len(instances) == 0 {
		r.mux.RUnlock()
		return nil
	}

	allServers := make([]*model.Server, 0, len(instances))
	for _, s := range instances {
		allServers = append(allServers, s)
	}
	r.mux.RUnlock()

	r.workersMux.Lock()
	defer r.workersMux.Unlock()

	state, ok := r.workers[serviceName]
	if !ok {
		return nil
	}

	start := state.lastIndex
	if start >= len(allServers) {
		start = 0
	}

	end := start + maxBatchSize
	if end > len(allServers) {
		end = len(allServers)
	}

	batch := allServers[start:end]
	state.lastIndex = end % len(allServers)

	return batch
}

/*
*Thuc hien goi ham go routine de tien hanh health check
 */
func (r *InMemoryRegistry) executeBatchCheck(batch []*model.Server, worker *health.HeathChecker, sem *semaphore.Weighted) {
	if err := sem.Acquire(r.ctx, 1); err != nil {
		return
	}

	go func() {
		defer sem.Release(1)
		worker.CheckServers(batch, r.UpdateStatus)
	}()
}
