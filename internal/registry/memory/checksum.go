package memory

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

func (r *InMemoryRegistry) RefreshAllChecksums() {
	r.mux.RLock()
	snapshot := make(map[string]map[string]*model.Server, len(r.services))
	for name, instances := range r.services {
		snapshot[name] = instances
	}
	r.mux.RUnlock()

	newChecksums := make(map[string]uint64, len(snapshot))

	serviceNames := make([]string, 0, len(snapshot))
	for name := range snapshot {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	for _, serviceName := range serviceNames {
		if instances := snapshot[serviceName]; len(instances) > 0 {
			newChecksums[serviceName] = calculateServiceChecksum(instances)
		}
	}

	r.mux.Lock()
	if r.checksumServices == nil {
		r.checksumServices = make(map[string]uint64)
	}

	for serviceName, checksum := range newChecksums {
		r.checksumServices[serviceName] = checksum
	}

	for serviceName := range r.checksumServices {
		if _, exists := newChecksums[serviceName]; !exists {
			delete(r.checksumServices, serviceName)
		}
	}
	r.mux.Unlock()
}

func (r *InMemoryRegistry) GetServiceChecksum(serviceName string) uint64 {
	r.mux.RLock()
	defer r.mux.RUnlock()

	if r.checksumServices == nil {
		return 0
	}
	return r.checksumServices[serviceName]
}

func (r *InMemoryRegistry) DetectChangedServices(newChecksums map[string]uint64) map[string]uint64 {
	r.mux.RLock()
	defer r.mux.RUnlock()

	changed := make(map[string]uint64)

	for serviceName, newCS := range newChecksums {
		oldCS := r.checksumServices[serviceName]
		if oldCS != newCS {
			changed[serviceName] = newCS
		}
	}

	for serviceName := range r.checksumServices {
		if _, stillExists := newChecksums[serviceName]; !stillExists {
			changed[serviceName] = 0
		}
	}

	return changed
}

func (r *InMemoryRegistry) HasServiceChanged(serviceName string) bool {
	old := r.GetServiceChecksum(serviceName)

	r.mux.RLock()
	instances, exists := r.services[serviceName]
	r.mux.RUnlock()

	if !exists {
		return old != 0
	}

	return old != calculateServiceChecksum(instances)
}

func calculateServiceChecksum(instances map[string]*model.Server) uint64 {
	if len(instances) == 0 {
		return 0
	}

	servers := make([]*model.Server, 0, len(instances))
	for _, srv := range instances {
		servers = append(servers, srv)
	}

	sort.Slice(servers, func(i, j int) bool {
		return servers[i].InstanceID < servers[j].InstanceID
	})

	h := fnv.New64a()
	var sb strings.Builder

	for _, srv := range servers {
		sb.Reset()
		sb.WriteString(serviceChecksumBuilder(srv))
		h.Write([]byte(sb.String()))
	}

	return h.Sum64()
}

func serviceChecksumBuilder(srv *model.Server) string {
	var sb strings.Builder
	sb.WriteString(srv.InstanceID)
	sb.WriteByte('|')
	sb.WriteString(srv.Host)
	sb.WriteByte('|')
	sb.WriteString(strconv.Itoa(srv.Port))
	sb.WriteByte('|')
	sb.WriteString(strconv.Itoa(srv.Weight))
	sb.WriteByte('|')
	sb.WriteString(strconv.FormatBool(srv.Health))
	return sb.String()
}
