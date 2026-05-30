package gossip_registry

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
)

type GossipFactory struct {
	cfg      *memberlist.Config
	list     *memberlist.Memberlist
	cluster  *ClusterManager
	queue    *broadcastQueue
	isJoined bool
	mu       sync.Mutex
	logger   *slog.Logger
	// [FIX 2026-04-24] tcp listener rieng cho custom sync stream (HandleStream)
	syncListener net.Listener
}

type Options struct {
	NodeName      string //name cua node lb nay
	BindPort      int    //port de listen cac su kien tu cac node lb khac
	AdvertisePort int    //port de cac node lb khac ket noi den
	Logger        *slog.Logger
}

func NewGossipRegistry(opts Options, reg registry.RegistryAdapter) *GossipFactory {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	clusterMgr := NewClusterManager(opts.NodeName, reg, opts.Logger)

	queue := newBroadcastQueue(clusterMgr, opts.Logger)

	clusterDelegate := newClusterDelegate(clusterMgr, opts.Logger)

	agentDelegate := &AgentDelegate{
		registryList: reg,
		logger:       opts.Logger,
	}

	eventDelegate := newEventDelegate(clusterDelegate, agentDelegate, opts.Logger)

	cfg := memberlist.DefaultLANConfig()
	cfg.Name = opts.NodeName
	cfg.BindPort = opts.BindPort
	cfg.AdvertisePort = opts.BindPort //la port de cac lb khac biet duong dne node lb nay
	cfg.LogOutput = io.Discard
	cfg.Events = eventDelegate
	cfg.PushPullInterval = 0 //disable push/pull tu custom sync data

	gf := &GossipFactory{
		cfg:     cfg,
		cluster: clusterMgr,
		queue:   queue,
		logger:  opts.Logger,
	}

	// [FIX 2026-04-24] inject syncFunc vao clusterDelegate sau khi gf duoc khoi tao
	// dung closure capture gf de goi SyncDataViaStream khi co LB peer moi join
	clusterDelegate.setSyncFunc(func(host string, syncPort int) {
		gf.SyncDataViaStream(host, syncPort)
	})

	return gf
}

func (g *GossipFactory) Start(opts Options, seeds []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// [FIX 2026-04-24] set SyncPort = BindPort + 1000 vao LBMeta de peer biet port TCP sync
	syncPort := opts.BindPort + 1000
	lbMeta := LBMeta{
		Role:          RoleLB,
		NodeName:      opts.NodeName,
		AdvertisePort: opts.AdvertisePort,
		BindPort:      opts.BindPort,
		SyncPort:      syncPort,
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

	// [FIX 2026-04-24] khoi dong TCP listener de nhan ket noi sync tu peer LB
	// peer se dial vao syncPort nay sau khi nhan LBMeta
	ln, lnErr := net.Listen("tcp", fmt.Sprintf(":%d", syncPort))
	if lnErr != nil {
		_ = list.Shutdown()
		return fmt.Errorf("gossip: tcp sync listener on port %d: %w", syncPort, lnErr)
	}
	g.syncListener = ln
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener da bi dong (Stop() duoc goi)
			}
			go g.HandleStream(conn)
		}
	}()
	g.logger.Info("gossip: tcp sync listener started", "port", syncPort)

	if len(seeds) > 0 {
		n, err := list.Join(seeds)
		if err != nil {
			g.logger.Warn(
				"gossip: initial join partial",
				"contacted", n,
				"err", err,
			)
		} else {
			g.isJoined = true
			g.logger.Info(
				"gossip: joined cluster",
				"node", g.cluster.selfName,
				"peers_contacted", n,
			)
		}
	} else {
		g.isJoined = true
		g.logger.Info(
			"gossip: started single-node cluster",
			"node", g.cluster.selfName,
		)
	}
	return nil
}

func (g *GossipFactory) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// [FIX 2026-04-24] dong TCP sync listener truoc khi shutdown memberlist
	if g.syncListener != nil {
		_ = g.syncListener.Close()
		g.syncListener = nil
	}

	if g.list == nil {
		return
	}
	if g.isJoined {
		_ = g.list.Leave(3e9)
	}
	_ = g.list.Shutdown()
	g.isJoined = false
	g.logger.Info(
		"gossip: stopped",
		"node", g.cluster.selfName,
	)
}

func (g *GossipFactory) GetCluster() *ClusterManager {
	return g.cluster
}

func (g *GossipFactory) GetNumberOfMembers() int {
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
