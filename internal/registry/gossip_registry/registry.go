package gossip_registry

import (
	"fmt"
	"log"
	"log/slog"
	"sync"

	"github.com/hashicorp/memberlist"
)

// GossipRegistry join cluster, lắng nghe events và bridge sang RegistryAdapter
type GossipRegistry struct {
	cfg      *memberlist.Config
	list     *memberlist.Memberlist
	delegate *EventDelegate
	queue    *broadcastQueue

	mu       sync.Mutex
	isJoined bool
	logger   *slog.Logger
}

type Options struct {
	NodeName string
	BindPort int
	Logger   *slog.Logger
}

func New(opts Options, registry RegistryAdapter) *GossipRegistry {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	queue := newBroadcastQueue(registry, opts.Logger)
	delegate := newEventDelegate(registry, queue, opts.Logger)

	cfg := memberlist.DefaultLANConfig()
	cfg.Name = opts.NodeName
	cfg.BindPort = opts.BindPort
	cfg.AdvertisePort = opts.BindPort
	cfg.LogOutput = log.Writer()

	// Delegate xử lý cả user messages (broadcast) lẫn node events
	cfg.Delegate = queue  // NotifyMsg + GetBroadcasts
	cfg.Events = delegate // NotifyJoin/Leave/Update

	return &GossipRegistry{
		cfg:      cfg,
		delegate: delegate,
		queue:    queue,
		logger:   opts.Logger,
	}
}

// Start tạo memberlist và join seeds
func (g *GossipRegistry) Start(seeds []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	list, err := memberlist.Create(g.cfg)
	if err != nil {
		return fmt.Errorf("gossip_registry: create memberlist: %w", err)
	}
	g.list = list

	// Inject hàm lấy số node thực tế cho TransmitLimitedQueue
	g.queue.SetNumNodesFunc(list.NumMembers)

	if len(seeds) > 0 {
		n, err := list.Join(seeds)
		if err != nil {
			// Không fatal — LB vẫn chạy được, sẽ có node join sau
			g.logger.Warn("GossipRegistry: initial join partial",
				"contacted", n, "err", err)
		} else {
			g.isJoined = true
			g.logger.Info("GossipRegistry: joined cluster", "nodes", n)
		}
	}
	return nil
}

// Stop rời cluster gracefully
func (g *GossipRegistry) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.list == nil {
		return
	}
	if g.isJoined {
		_ = g.list.Leave(3e9) // 3s timeout
	}
	_ = g.list.Shutdown()
	g.isJoined = false
	g.logger.Info("GossipRegistry: stopped")
}

// BroadcastHealthChange cho phép LB chủ động broadcast khi nó detect health change
// (ví dụ từ health checker HTTP riêng của LB)
func (g *GossipRegistry) BroadcastHealthChange(instanceID, svcName string, alive bool) {
	g.queue.BroadcastHealthChange(instanceID, svcName, alive)
}

// Members trả về số node hiện tại trong cluster (debug/metrics)
func (g *GossipRegistry) Members() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.list == nil {
		return 0
	}
	return g.list.NumMembers()
}
