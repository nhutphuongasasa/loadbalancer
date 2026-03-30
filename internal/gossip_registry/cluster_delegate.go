package gossip_registry

import (
	"log/slog"
	"net"

	"github.com/hashicorp/memberlist"
)

// ClusterDelegate xử lý gossip events từ các LB node khác trong cluster.
// Nó KHÔNG đụng vào RegistryAdapter — chỉ thông báo cho ClusterManager.
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

// OnLBJoin được gọi khi một LB node khác join cluster.
func (d *ClusterDelegate) OnLBJoin(n *memberlist.Node) {
	meta, ok := parseLBMeta(n.Meta)
	if !ok {
		d.logger.Warn("gossip: bad LB meta on join", "node", n.Name)
		return
	}
	info := LBNodeInfo{
		Name:     n.Name,
		Host:     lbNodeHost(n),
		BindPort: meta.BindPort,
		HTTPAPI:  meta.HTTPAPI,
	}
	d.manager.onLBJoin(info)
	d.logger.Info("gossip: LB node joined cluster",
		"node", n.Name,
		"host", info.Host,
		"bind_port", meta.BindPort,
	)
}

// OnLBLeave được gọi khi một LB node leave cluster.
func (d *ClusterDelegate) OnLBLeave(n *memberlist.Node) {
	d.manager.onLBLeave(n.Name)
	d.logger.Info("gossip: LB node left cluster", "node", n.Name)
}

// OnLBUpdate được gọi khi metadata của LB node thay đổi.
func (d *ClusterDelegate) OnLBUpdate(n *memberlist.Node) {
	meta, ok := parseLBMeta(n.Meta)
	if !ok {
		d.logger.Warn("gossip: bad LB meta on update", "node", n.Name)
		return
	}
	info := LBNodeInfo{
		Name:     n.Name,
		Host:     lbNodeHost(n),
		BindPort: meta.BindPort,
		HTTPAPI:  meta.HTTPAPI,
	}
	d.manager.onLBUpdate(info)
	d.logger.Info("gossip: LB node updated", "node", n.Name)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// parseLBMeta unmarshal và validate LBMeta.
func parseLBMeta(raw []byte) (*LBMeta, bool) {
	var m LBMeta
	if err := jsonUnmarshal(raw, &m); err != nil {
		return nil, false
	}
	if m.Role != RoleLB || m.BindPort <= 0 {
		return nil, false
	}
	return &m, true
}

func lbNodeHost(n *memberlist.Node) string {
	if n.Addr != nil {
		return n.Addr.String()
	}
	if host, _, err := net.SplitHostPort(n.Name); err == nil {
		return host
	}
	return n.Name
}
