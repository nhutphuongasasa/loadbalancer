package gossip_registry

import (
	"log/slog"
	"sync"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
)

// LBNodeInfo chứa thông tin về một LB peer trong cluster.
type LBNodeInfo struct {
	Name     string    // memberlist node name (unique)
	Host     string    // địa chỉ IP
	BindPort int       // gossip bind port
	HTTPAPI  int       // HTTP API port (tuỳ chọn, để health check LB peer)
	JoinedAt time.Time // thời điểm join
}

// ClusterManager quản lý trạng thái của LB cluster:
//  1. Track danh sách LB nodes (join/leave/update)
//  2. Forward health events nhận từ broadcast sang RegistryAdapter
//  3. Đồng bộ state (backend list snapshot) khi LB node mới join
//
// ClusterManager implement ClusterEventHandler nên broadcastQueue
// có thể gọi vào mà không cần biết concrete type.
type ClusterManager struct {
	mu sync.RWMutex

	selfName string                 // tên của LB node này
	nodes    map[string]*LBNodeInfo // key = node Name
	reg      registry.RegistryAdapter

	// queue dùng để gửi state snapshot khi LB mới join.
	// Được inject sau khi broadcastQueue được tạo (tránh circular init).
	queue *broadcastQueue

	logger *slog.Logger
}

// NewClusterManager tạo ClusterManager mới.
// selfName: tên của LB node này (phải trùng với cfg.Name của memberlist).
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

// setQueue inject broadcastQueue sau khi nó được tạo.
// Gọi bởi GossipRegistry trong quá trình khởi tạo.
func (m *ClusterManager) setQueue(q *broadcastQueue) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = q
}

// ─── LB Node lifecycle ────────────────────────────────────────────────────────

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

	m.logger.Info("cluster: LB peer joined",
		"peer", info.Name,
		"host", info.Host,
		"total_lb_nodes", nodeCount,
	)

	// Gửi state snapshot cho LB peer mới để đồng bộ danh sách backend.
	m.sendStateSnapshot(info.Name)
}

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
		m.logger.Info("cluster: LB peer left",
			"peer", nodeName,
			"remaining_lb_nodes", nodeCount,
		)
	}
}

func (m *ClusterManager) onLBUpdate(info LBNodeInfo) {
	if info.Name == m.selfName {
		return
	}
	m.mu.Lock()
	if existing, ok := m.nodes[info.Name]; ok {
		info.JoinedAt = existing.JoinedAt // giữ nguyên thời điểm join
	} else {
		info.JoinedAt = time.Now()
	}
	m.nodes[info.Name] = &info
	m.mu.Unlock()

	m.logger.Info("cluster: LB peer updated", "peer", info.Name)
}

// ─── ClusterEventHandler impl ─────────────────────────────────────────────────

// OnHealthBroadcast nhận HealthMsg từ broadcast (đã được registry update bởi broadcastQueue).
// ClusterManager dùng để tracking thống kê / audit trail nếu cần.
func (m *ClusterManager) OnHealthBroadcast(msg HealthMsg) {
	// Hiện tại chỉ log — registry đã được update trong broadcastQueue.NotifyMsg.
	// Có thể mở rộng: ghi metrics, alert khi nhiều backend down liên tục, v.v.
	m.logger.Debug("cluster: health event tracked",
		"instance", msg.InstanceID,
		"service", msg.ServiceName,
		"alive", msg.Alive,
		"reported_by", msg.SourceLB,
	)
}

// MergeState nhận ClusterStateMsg từ LB peer và merge vào registry.
// Chỉ apply những backend chưa có hoặc có timestamp cũ hơn.
func (m *ClusterManager) MergeState(msg ClusterStateMsg) {
	if msg.FromLB == m.selfName {
		return // bỏ qua state của chính mình
	}

	m.logger.Info("cluster: merging state from peer",
		"from", msg.FromLB,
		"backends", len(msg.Backends),
	)

	for _, b := range msg.Backends {
		srv := &model.Server{
			InstanceID:  b.InstanceID,
			ServiceName: b.ServiceName,
			Host:        b.Host,
			Port:        b.Port,
			Weight:      b.Weight,
			Health:      b.Alive,
		}
		// Register sẽ upsert — nếu đã tồn tại thì registry quyết định merge policy.
		if err := m.reg.Register(srv); err != nil {
			m.logger.Warn("cluster: merge state register failed",
				"instance", b.InstanceID, "err", err)
		}
		// Sync trạng thái health
		m.reg.UpdateStatus(b.InstanceID, b.ServiceName, b.Alive)
	}
}

// ─── State Snapshot ───────────────────────────────────────────────────────────

// sendStateSnapshot gửi snapshot toàn bộ backend list cho LB peer vừa join.
func (m *ClusterManager) sendStateSnapshot(targetNode string) {
	m.mu.RLock()
	q := m.queue
	m.mu.RUnlock()

	if q == nil {
		m.logger.Warn("cluster: cannot send state snapshot, queue not ready")
		return
	}

	backends := m.buildSnapshot()
	if len(backends) == 0 {
		return
	}

	stateMsg := ClusterStateMsg{
		Backends:  backends,
		FromLB:    m.selfName,
		Timestamp: time.Now(),
	}
	q.BroadcastState(stateMsg)
	m.logger.Info("cluster: sent state snapshot",
		"to", targetNode,
		"backends", len(backends),
	)
}

// buildSnapshot lấy danh sách backend hiện tại từ registry và convert sang snapshot.
func (m *ClusterManager) buildSnapshot() []BackendSnapshot {
	servers := m.reg.ListAll()
	snapshots := make([]BackendSnapshot, 0, len(servers))
	for i := range servers {
		srv := &servers[i]
		// Hàm ListAll trả về []mà một số field có mutex nê ta dùng index để lấy reference
		snapshots = append(snapshots, BackendSnapshot{
			InstanceID:  srv.InstanceID,
			ServiceName: srv.ServiceName,
			Host:        srv.Host,
			Port:        srv.Port,
			Weight:      srv.Weight,
			Alive:       srv.Health,
		})
	}
	return snapshots
}

// ─── Public query API ─────────────────────────────────────────────────────────

// Peers trả về danh sách LB nodes đang active trong cluster (không kể chính mình).
func (m *ClusterManager) Peers() []LBNodeInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]LBNodeInfo, 0, len(m.nodes))
	for _, n := range m.nodes {
		out = append(out, *n)
	}
	return out
}

// PeerCount trả về số LB peer đang active.
func (m *ClusterManager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.nodes)
}

// GetPeer trả về thông tin của một LB peer theo tên.
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
