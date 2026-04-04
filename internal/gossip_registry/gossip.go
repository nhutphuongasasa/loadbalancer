package gossip_registry

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
)

type GossipRegistry struct {
	cfg     *memberlist.Config
	list    *memberlist.Memberlist
	cluster *ClusterManager
	queue   *broadcastQueue

	mu       sync.Mutex
	isJoined bool
	selfName string
	logger   *slog.Logger
}

type Options struct {
	NodeName string
	BindPort int
	// HTTPAPI là port HTTP API của LB node này (ghi vào LBMeta để peer biết).
	HTTPAPI int
	// Logger tuỳ chọn.
	Logger *slog.Logger
}

// NewGossipRegistry khởi tạo GossipRegistry và wire tất cả thành phần.
func NewGossipRegistry(opts Options, reg registry.RegistryAdapter) *GossipRegistry {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	// 1. Tạo ClusterManager — quản lý LB peers
	clusterMgr := NewClusterManager(opts.NodeName, reg, opts.Logger)

	// 2. Tạo broadcastQueue — route messages vào registry và cluster
	queue := newBroadcastQueue(clusterMgr, opts.Logger)

	// 3. Inject queue vào cluster manager (tránh circular init)
	clusterMgr.setQueue(queue)

	// 4. Tạo ClusterDelegate — xử lý LB node events
	clusterDelegate := newClusterDelegate(clusterMgr, opts.Logger)

	// 5. Tạo EventDelegate — phân luồng backend vs lb events
	eventDelegate := newEventDelegate(reg, clusterDelegate, queue, opts.Logger)

	// 6. Cấu hình memberlist
	cfg := memberlist.DefaultLANConfig()
	cfg.Name = opts.NodeName
	cfg.BindPort = opts.BindPort
	cfg.AdvertisePort = opts.BindPort
	cfg.LogOutput = io.Discard // dùng slog thay vì stdlib log của memberlist
	// cfg.Delegate được set trong Start() sau khi có LBMeta bytes
	cfg.Events = eventDelegate // xử lý node events (join/leave/update)

	return &GossipRegistry{
		cfg:      cfg,
		cluster:  clusterMgr,
		queue:    queue,
		selfName: opts.NodeName,
		logger:   opts.Logger,
	}
}

// Start tạo memberlist, quảng bá LBMeta của node này, và join seeds.
// seeds là danh sách địa chỉ "host:port" của các LB node khác đã chạy.
func (g *GossipRegistry) Start(opts Options, seeds []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Encode LBMeta của node này — các peer sẽ nhận được qua Delegate.NodeMeta().
	lbMeta := LBMeta{
		Role:     RoleLB,
		BindPort: opts.BindPort,
		HTTPAPI:  opts.HTTPAPI,
	}
	metaBytes, err := json.Marshal(lbMeta)
	if err != nil {
		return fmt.Errorf("gossip: marshal LBMeta: %w", err)
	}

	// Wrap broadcastQueue với nodeMeta để override NodeMeta().
	// memberlist gọi Delegate.NodeMeta(limit) để lấy metadata của node này
	// khi announce tới các peer — đây là cách inject LBMeta vào gossip.
	g.cfg.Delegate = &nodeMeta{
		broadcastQueue: g.queue,
		meta:           metaBytes,
	}

	list, err := memberlist.Create(g.cfg)
	if err != nil {
		return fmt.Errorf("gossip: create memberlist: %w", err)
	}
	g.list = list

	// Inject số member thực tế vào TransmitLimitedQueue
	g.queue.SetNumNodesFunc(list.NumMembers)

	if len(seeds) > 0 {
		n, err := list.Join(seeds)
		if err != nil {
			// Không fatal — LB vẫn chạy, sẽ có node join sau
			g.logger.Warn("gossip: initial join partial",
				"contacted", n, "err", err)
		} else {
			g.isJoined = true
			g.logger.Info("gossip: joined cluster",
				"node", g.selfName,
				"peers_contacted", n,
			)
		}
	} else {
		// Single node — vẫn tạo cluster, chờ peer join
		g.isJoined = true
		g.logger.Info("gossip: started single-node cluster", "node", g.selfName)
	}
	return nil
}

// Stop rời cluster gracefully và shutdown memberlist.
func (g *GossipRegistry) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.list == nil {
		return
	}
	if g.isJoined {
		_ = g.list.Leave(3e9) // 3 giây timeout
	}
	_ = g.list.Shutdown()
	g.isJoined = false
	g.logger.Info("gossip: stopped", "node", g.selfName)
}

// BroadcastHealthChange cho phép LB chủ động broadcast health change
// (ví dụ từ active HTTP health checker của LB, không phải từ gossip event).
func (g *GossipRegistry) BroadcastHealthChange(instanceID, svcName string, alive bool, action AgentAction) {
	g.queue.BroadcastHealthChange(instanceID, svcName, alive, g.selfName, action)
}

// Cluster trả về ClusterManager để caller query thông tin LB peers.
func (g *GossipRegistry) Cluster() *ClusterManager {
	return g.cluster
}

// Members trả về tổng số node đang online trong gossip (bao gồm cả backend agents và LB nodes).
func (g *GossipRegistry) Members() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.list == nil {
		return 0
	}
	return g.list.NumMembers()
}

// ─── nodeMeta ─────────────────────────────────────────────────────────────────

// nodeMeta embed broadcastQueue và override NodeMeta() để inject LBMeta
// của node này vào gossip khi announce tới các peer.
// memberlist gọi Delegate.NodeMeta(limit int) để lấy metadata của node.
type nodeMeta struct {
	*broadcastQueue
	meta []byte
}

func (n *nodeMeta) NodeMeta(_ int) []byte {
	return n.meta
}
