package gossip_registry

import (
	"log/slog"
	"sync"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
)

// quan li cac node lb cluster
type ClusterManager struct {
	selfName string                   // ten cua lb node nay
	nodes    map[string]*LBNodeInfo   // thong tin cac node lb khac
	reg      registry.RegistryAdapter //noi quan li danh sach server
	mu       sync.RWMutex
	logger   *slog.Logger
}

// selfName: ten cua lb node nay trung voi name trong memberlist.config khi duoc khoi tao
func NewClusterManager(selfName string, reg registry.RegistryAdapter, log *slog.Logger) *ClusterManager {
	if log == nil {
		log = slog.Default()
	}
	return &ClusterManager{
		selfName: selfName,
		nodes:    make(map[string]*LBNodeInfo),
		reg:      reg,
		logger:   log,
	}
}

// thuc thi khi co lb moi join vao cluster
// Gửi state snapshot cho LB peer mới để đồng bộ danh sách backend.
func (m *ClusterManager) onLBJoin(info LBNodeInfo) {
	// Bỏ qua chính mình
	if info.Name == m.selfName {
		return
	}
	info.JoinedAt = time.Now()

	m.mu.Lock()
	m.nodes[info.Name] = &info
	nodeCount := len(m.nodes)
	m.mu.Unlock()

	m.logger.Info(
		"cluster: LB peer joined",
		"peer", info.Name,
		"host", info.Host,
		"total_lb_nodes", nodeCount,
	)
}

// thuc thi khi co lb leave cluster
func (m *ClusterManager) onLBLeave(nodeName string) {
	if nodeName == m.selfName {
		return
	}
	m.mu.Lock()
	_, existed := m.nodes[nodeName]
	delete(m.nodes, nodeName)
	nodeCount := len(m.nodes)
	m.mu.Unlock()

	if existed {
		m.logger.Info(
			"cluster: LB peer left",
			"peer", nodeName,
			"remaining_lb_nodes", nodeCount,
		)
	}
}

// thuc thi khi co lb update (thay doi meta)
func (m *ClusterManager) onLBUpdate(info LBNodeInfo) {
	if info.Name == m.selfName {
		return
	}
	m.mu.Lock()
	if existing, ok := m.nodes[info.Name]; ok {
		info.JoinedAt = existing.JoinedAt //giu nguyen thoi gian join ban dau
	} else {
		info.JoinedAt = time.Now()
	}
	m.nodes[info.Name] = &info
	m.mu.Unlock()

	m.logger.Info("cluster: LB peer updated", "peer", info.Name)
}

// xu li logic khi nhan duoc message back end quan ba join lb cua broadcastQueue
func (m *ClusterManager) OnHealthBroadcast(msg HealthMsg) {
	action := msg.Action
	switch action {
	case ActionJoin:
		m.reg.Register(&model.Server{
			InstanceID:  msg.InstanceID,
			ServiceName: msg.ServiceName,
			Health:      msg.Alive,
			Host:        msg.Host,
			Port:        msg.Port,
			Weight:      msg.Weight,
		})
	case ActionLeave:
		m.reg.Deregister(msg.ServiceName, msg.InstanceID)
	case ActionUpdate:
		m.reg.UpdateStatus(msg.ServiceName, msg.InstanceID, msg.Alive)
	}

	m.logger.Debug("cluster: health event tracked",
		"instance", msg.InstanceID,
		"service", msg.ServiceName,
		"alive", msg.Alive,
		"reported_by", msg.SourceLB,
	)
}

func (m *ClusterManager) GetCheckRegisterAdapter() registry.RegistryAdapter {
	return m.reg
}

// tao state snapshot cua lb hien tai
func (m *ClusterManager) BuildSnapshot() map[string][]BackendSnapshot {
	servers := m.reg.ListAll()
	snapshots := make(map[string][]BackendSnapshot)

	for svcName, srvList := range servers {
		for _, srv := range srvList {
			snapshots[svcName] = append(snapshots[svcName], BackendSnapshot{
				InstanceID:  srv.InstanceID,
				ServiceName: srv.ServiceName,
				Host:        srv.Host,
				Port:        srv.Port,
				Weight:      srv.Weight,
				Alive:       srv.Health,
			})
		}
	}
	return snapshots
}

// tra ve danh sach LB peer dang active trong cluster
func (m *ClusterManager) Peers() []LBNodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]LBNodeInfo, 0, len(m.nodes))
	for _, n := range m.nodes {
		out = append(out, *n)
	}
	return out
}

// tra ve so luong LB peer dang active trong cluster
func (m *ClusterManager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.nodes)
}

// tra ve LB peer theo ten, neu khong tim thay tra ve false
func (m *ClusterManager) GetPeer(name string) (*LBNodeInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.nodes[name]
	if !ok {
		return nil, false
	}
	cp := *n
	return &cp, true
}
