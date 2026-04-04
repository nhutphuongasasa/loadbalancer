package gossip_registry

import (
	"encoding/json"
	"log/slog"

	"github.com/hashicorp/memberlist"
)

type ClusterDelegate struct {
	manager *ClusterManager
	logger  *slog.Logger
}

func newClusterDelegate(m *ClusterManager, log *slog.Logger) *ClusterDelegate {
	return &ClusterDelegate{
		manager: m,
		logger:  log,
	}
}

// duoc goi khi co LB node join cluster.
func (d *ClusterDelegate) OnLBJoin(n *memberlist.Node) {
	meta, ok := parseLBMeta(n.Meta)
	if !ok {
		d.logger.Warn(
			"gossip: bad LB meta on join",
			"node", n.Name,
		)
		return
	}
	info := LBNodeInfo{
		Name:     n.Name,
		Host:     nodeHost(n),
		BindPort: meta.BindPort,
		HTTPAPI:  meta.HTTPAPI,
	}
	d.manager.onLBJoin(info)
	d.logger.Info(
		"gossip: LB node joined cluster",
		"node", n.Name,
		"host", info.Host,
		"bind_port", meta.BindPort,
	)
}

// duoc goi khi co LB node leave cluster.
func (d *ClusterDelegate) OnLBLeave(n *memberlist.Node) {
	d.manager.onLBLeave(n.Name)
	d.logger.Info(
		"gossip: LB node left cluster",
		"node", n.Name,
	)
}

// duoc goi khi co LB node update
func (d *ClusterDelegate) OnLBUpdate(n *memberlist.Node) {
	meta, ok := parseLBMeta(n.Meta)
	if !ok {
		d.logger.Warn(
			"gossip: bad LB meta on update",
			"node", n.Name,
		)
		return
	}
	info := LBNodeInfo{
		Name:     n.Name,
		Host:     nodeHost(n),
		BindPort: meta.BindPort,
		HTTPAPI:  meta.HTTPAPI,
	}
	d.manager.onLBUpdate(info)
	d.logger.Info("gossip: LB node updated", "node", n.Name)
}

// helper parseLBMeta duoc goi khi co LB node join/update
func parseLBMeta(raw []byte) (*LBMeta, bool) {
	var m LBMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	if m.Role != RoleLB || m.BindPort <= 0 {
		return nil, false
	}
	return &m, true
}
