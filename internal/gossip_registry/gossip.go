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
	NodeName      string //name cua node lb nay
	BindPort      int    //port de listen cac su kien tu cac node lb khac
	AdvertisePort int    //port de cac node lb khac ket noi den
	Logger        *slog.Logger
}

func NewGossipRegistry(opts Options, reg registry.RegistryAdapter) *GossipRegistry {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	clusterMgr := NewClusterManager(opts.NodeName, reg, opts.Logger)

	queue := newBroadcastQueue(clusterMgr, opts.Logger)

	clusterMgr.setQueue(queue)

	clusterDelegate := newClusterDelegate(clusterMgr, opts.Logger)

	eventDelegate := newEventDelegate(reg, clusterDelegate, queue, opts.Logger)

	cfg := memberlist.DefaultLANConfig()
	cfg.Name = opts.NodeName
	cfg.BindPort = opts.BindPort
	cfg.AdvertisePort = opts.BindPort //la port de cac lb khac biet duong dne node lb nay
	cfg.LogOutput = io.Discard
	cfg.Events = eventDelegate

	return &GossipRegistry{
		cfg:      cfg,
		cluster:  clusterMgr,
		queue:    queue,
		selfName: opts.NodeName,
		logger:   opts.Logger,
	}
}

func (g *GossipRegistry) Start(opts Options, seeds []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	lbMeta := LBMeta{
		Role:     RoleLB,
		BindPort: opts.BindPort,
		// HTTPAPI:  opts.HTTPAPI,
	}
	metaBytes, err := json.Marshal(lbMeta)
	if err != nil {
		return fmt.Errorf("gossip: marshal LBMeta: %w", err)
	}

	g.cfg.Delegate = &nodeMeta{
		broadcastQueue: g.queue,
		meta:           metaBytes,
	}

	list, err := memberlist.Create(g.cfg)
	if err != nil {
		return fmt.Errorf("gossip: create memberlist: %w", err)
	}
	g.list = list

	g.queue.SetNumNodesFunc(list.NumMembers)

	if len(seeds) > 0 {
		n, err := list.Join(seeds)
		if err != nil {
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
		g.isJoined = true
		g.logger.Info("gossip: started single-node cluster", "node", g.selfName)
	}
	return nil
}

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

// func (g *GossipRegistry) BroadcastHealthChange(instanceID, svcName string, alive bool, action AgentAction) {
// 	g.queue.BroadcastBackendHealthChange(instanceID, svcName, alive, g.selfName, action)
// }

func (g *GossipRegistry) Cluster() *ClusterManager {
	return g.cluster
}

func (g *GossipRegistry) Members() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.list == nil {
		return 0
	}
	return g.list.NumMembers()
}

type nodeMeta struct {
	*broadcastQueue
	meta []byte
}

func (n *nodeMeta) NodeMeta(_ int) []byte {
	return n.meta
}
