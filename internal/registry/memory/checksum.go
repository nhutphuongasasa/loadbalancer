package memory

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

func (r *InMemoryRegistry) RefreshAllChecksums() {
	if !r.isDirty {
		return
	}

	r.mux.RLock()
	snapshot := r.snapshotListFromRegistry()
	r.mux.RUnlock()

	//tao map de luu tru ket qua checksum cho tung servername
	newChecksums := make(map[string]uint64, len(snapshot))

	//copy dnah sach cac servername va thuc hien sort vi map golang khong co thu tu
	serviceNames := sortListServerName(snapshot)

	//truy cap instances cau cac servername va tinh toan checksum 1 lan nhieu instance cua 1 servername
	for _, serviceName := range serviceNames {
		if instances := snapshot[serviceName]; len(instances) > 0 {
			newChecksums[serviceName] = calculateServiceChecksum(instances)
		}
	}

	//thuc hien thay doi gia tri checksum duoc registry luu tru
	r.mux.Lock()
	r.changeChecksumInRegistry(newChecksums)
	r.mux.Unlock()
}

func sortListServerName(snapshot map[string]map[string]*model.Server) []string {
	//copy dnah sach cac servername va thuc hien sort vi map golang khong co thu tu
	serviceNames := make([]string, 0, len(snapshot))
	for name := range snapshot {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	return serviceNames
}

func (r *InMemoryRegistry) snapshotListFromRegistry() map[string]map[string]*model.Server {
	snapshot := make(map[string]map[string]*model.Server, len(r.services))
	for name, instances := range r.services {
		// Tạo một map mới cho từng service để cách ly hoàn toàn với Registry
		subCopy := make(map[string]*model.Server, len(instances))
		for id, srv := range instances {
			subCopy[id] = srv
		}
		snapshot[name] = subCopy
	}
	return snapshot
}

func (r *InMemoryRegistry) changeChecksumInRegistry(newChecksums map[string]uint64) {
	if r.checksumServices == nil {
		r.checksumServices = make(map[string]uint64)
	}

	//duyet va gna gia tri vao moi vao
	for serviceName, checksum := range newChecksums {
		r.checksumServices[serviceName] = checksum
	}

	//xoa nhung cai khong co
	for serviceName := range r.checksumServices {
		if _, exists := newChecksums[serviceName]; !exists {
			delete(r.checksumServices, serviceName)
		}
	}
}

func (r *InMemoryRegistry) copyServiceInstances(serviceName string) map[string]*model.Server {
	instances, ok := r.services[serviceName]
	if !ok || len(instances) == 0 {
		return nil
	}

	copyInstances := make(map[string]*model.Server, len(instances))
	for id, srv := range instances {
		copyInstances[id] = srv
	}
	return copyInstances
}

func (r *InMemoryRegistry) CompareAndFetch(remoteChecksums map[string]uint64) map[string]map[string]*model.Server {
	r.mux.RLock()
	defer r.mux.RUnlock()

	diffData := make(map[string]map[string]*model.Server)

	// 1. Tìm các dịch vụ khác biệt checksum hoặc remote đang có mà cần update
	r.getUpdatedFromRemote(remoteChecksums, diffData)

	// 2. Tìm các dịch vụ mới hoàn toàn ở local mà remote chưa có
	r.getNewLocalServices(remoteChecksums, diffData)

	return diffData
}

func (r *InMemoryRegistry) getUpdatedFromRemote(remoteChecksums map[string]uint64, diffData map[string]map[string]*model.Server) {
	for serviceName, rChecksum := range remoteChecksums {
		localChecksum, exists := r.checksumServices[serviceName]

		// Nếu không tồn tại hoặc checksum khác nhau -> Cần lấy dữ liệu mới
		if !exists || localChecksum != rChecksum {
			diffData[serviceName] = r.copyServiceInstances(serviceName)
		}
	}
}

func (r *InMemoryRegistry) getNewLocalServices(remoteChecksums map[string]uint64, diffData map[string]map[string]*model.Server) {
	for serviceName := range r.checksumServices {
		if _, existsInRemote := remoteChecksums[serviceName]; !existsInRemote {
			diffData[serviceName] = r.copyServiceInstances(serviceName)
		}
	}
}

func (r *InMemoryRegistry) GetServiceChecksum(serviceName string) uint64 {
	r.mux.RLock()
	defer r.mux.RUnlock()

	if r.checksumServices == nil {
		return 0
	}
	return r.checksumServices[serviceName]
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

// func (r *InMemoryRegistry) DetectChangedServices(newChecksums map[string]uint64) map[string]uint64 {
// 	r.mux.RLock()
// 	defer r.mux.RUnlock()

// 	changed := make(map[string]uint64)

// 	for serviceName, newCS := range newChecksums {
// 		oldCS := r.checksumServices[serviceName]
// 		if oldCS != newCS {
// 			changed[serviceName] = newCS
// 		}
// 	}

// 	for serviceName := range r.checksumServices {
// 		if _, stillExists := newChecksums[serviceName]; !stillExists {
// 			changed[serviceName] = 0
// 		}
// 	}

// 	return changed
// }

// func (r *InMemoryRegistry) HasServiceChanged(serviceName string) bool {
// 	old := r.GetServiceChecksum(serviceName)

// 	r.mux.RLock()
// 	instances, exists := r.services[serviceName]
// 	r.mux.RUnlock()

// 	if !exists {
// 		return old != 0
// 	}

// 	return old != calculateServiceChecksum(instances)
// }
