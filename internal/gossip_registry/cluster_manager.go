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
	mu sync.RWMutex

	selfName string                 // ten cua lb node nay
	nodes    map[string]*LBNodeInfo // key = node Name
	reg      registry.RegistryAdapter

	// queue dung de gui state snapshot den node LB moi join.
	queue *broadcastQueue

	logger *slog.Logger
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

func (m *ClusterManager) setQueue(q *broadcastQueue) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = q
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

// xu li logic merge state khi nhan duoc message state snapshot tu broadcastQueue
// dung de dong bo toan bo state backend list
func (m *ClusterManager) MergeState(msg ClusterStateMsg) {
	if msg.FromLB == m.selfName {
		return
	}

	m.logger.Info(
		"cluster: merging state from peer",
		"from", msg.FromLB,
		"backends", len(msg.Backends),
	)

	// for _, b := range msg.Backends {
	// 	srv := &model.Server{
	// 		InstanceID:  b.InstanceID,
	// 		ServiceName: b.ServiceName,
	// 		Host:        b.Host,
	// 		Port:        b.Port,
	// 		Weight:      b.Weight,
	// 		Health:      b.Alive,
	// 	}
	// 	if err := m.reg.Register(srv); err != nil {
	// 		m.logger.Warn(
	// 			"cluster: merge state register failed",
	// 			"instance", b.InstanceID,
	// 			"err", err,
	// 		)
	// 	} else {
	// 		m.reg.UpdateStatus(b.ServiceName, b.InstanceID, b.Alive)
	// 	}
	// }
}

func (m *ClusterManager) GetSelfName() string {
	return m.selfName
}

// sendStateSnapshot gui snapshot toan bo backend list cho LB peer vừa join co the that bai
// func (m *ClusterManager) sendStateSnapshot(targetNode string) {
// 	m.mu.RLock()
// 	q := m.queue
// 	m.mu.RUnlock()

// 	if q == nil {
// 		m.logger.Warn("cluster: cannot send state snapshot, queue not ready")
// 		return
// 	}

// 	backends := m.BuildSnapshot()
// 	if len(backends) == 0 {
// 		return
// 	}

// 	stateMsg := ClusterStateMsg{
// 		Backends:  backends,
// 		FromLB:    m.selfName,
// 		Timestamp: time.Now(),
// 	}
// 	q.BroadcastLBState(stateMsg)
// 	m.logger.Info(
// 		"cluster: sent state snapshot",
// 		"to", targetNode,
// 		"backends", len(backends),
// 	)
// }

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
