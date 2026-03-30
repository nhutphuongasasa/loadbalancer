package gossip_registry

import (
	"log/slog"
	"net"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
)

// EventDelegate implement memberlist.EventDelegate.
// Chỉ xử lý các node có role=backend (Agent).
// Các node role=lb được forward sang ClusterDelegate.
type EventDelegate struct {
	registry        registry.RegistryAdapter
	clusterDelegate *ClusterDelegate
	queue           *broadcastQueue
	logger          *slog.Logger
}

func newEventDelegate(
	r registry.RegistryAdapter,
	cd *ClusterDelegate,
	q *broadcastQueue,
	log *slog.Logger,
) *EventDelegate {
	return &EventDelegate{
		registry:        r,
		clusterDelegate: cd,
		queue:           q,
		logger:          log,
	}
}

// NotifyJoin được gọi khi bất kỳ node nào join cluster.
func (d *EventDelegate) NotifyJoin(n *memberlist.Node) {
	role := roleFromRaw(n.Meta)
	switch role {
	case RoleBackend:
		d.onAgentJoin(n)
	case RoleLB:
		d.clusterDelegate.OnLBJoin(n)
	default:
		d.logger.Warn(
			"gossip: unknown node role on join",
			"node", n.Name,
			"role", role,
		)
	}
}

// NotifyLeave được gọi khi bất kỳ node nào leave.
func (d *EventDelegate) NotifyLeave(n *memberlist.Node) {
	role := roleFromRaw(n.Meta)
	switch role {
	case RoleBackend:
		d.onAgentLeave(n)
	case RoleLB:
		d.clusterDelegate.OnLBLeave(n)
	default:
		d.logger.Warn(
			"gossip: unknown node role on leave",
			"node", n.Name,
			"role", role,
		)
	}
}

// NotifyUpdate được gọi khi metadata của node thay đổi.
func (d *EventDelegate) NotifyUpdate(n *memberlist.Node) {
	role := roleFromRaw(n.Meta)
	switch role {
	case RoleBackend:
		d.onAgentUpdate(n)
	case RoleLB:
		d.clusterDelegate.OnLBUpdate(n)
	default:
		d.logger.Warn(
			"gossip: unknown node role on update",
			"node", n.Name,
			"role", role,
		)
	}
}

func (d *EventDelegate) onAgentJoin(n *memberlist.Node) {
	meta, ok := parseAgentMeta(n.Meta)
	if !ok {
		d.logger.Warn("gossip: bad agent meta on join", "node", n.Name)
		return
	}
	srv := buildServer(n, meta)
	if err := d.registry.Register(srv); err != nil {
		d.logger.Warn("gossip: register agent failed",
			"node", n.Name, "err", err)
		return
	}
	d.logger.Info("gossip: backend agent joined",
		"node", n.Name,
		"service", meta.ServiceName,
		"addr", srv.Host,
		"port", srv.Port,
		"weight", meta.Weight,
	)
}

func (d *EventDelegate) onAgentLeave(n *memberlist.Node) {
	meta, ok := parseAgentMeta(n.Meta)
	if !ok {
		// Meta có thể đã bị xóa khi node down — dùng node name để deregister
		d.logger.Warn(
			"gossip: bad agent meta on leave, deregister by name",
			"node", n.Name,
		)
		//xoa back end ra khoi danh sach
		if err := d.registry.Deregister(meta.ServiceName, meta.InstanceID); err != nil {
			d.logger.Warn(
				"gossip: deregister by ID failed",
				"node", n.Name,
				"err", err,
			)
		}
		return
	}
	if err := d.registry.Deregister(meta.ServiceName, n.Name); err != nil {
		d.logger.Warn(
			"gossip: deregister agent failed",
			"node", n.Name,
			"err", err,
		)
		return
	}
	d.logger.Info(
		"gossip: backend agent left",
		"node", n.Name,
		"service", meta.ServiceName,
	)
}

func (d *EventDelegate) onAgentUpdate(n *memberlist.Node) {
	meta, ok := parseAgentMeta(n.Meta)
	if !ok {
		d.logger.Warn("gossip: bad agent meta on update", "node", n.Name)
		return
	}
	srv := buildServer(n, meta)
	// Deregister với service name cũ (có thể không đổi), rồi register lại
	_ = d.registry.Deregister(meta.ServiceName, n.Name)
	if err := d.registry.Register(srv); err != nil {
		d.logger.Warn("gossip: re-register agent on update failed",
			"node", n.Name, "err", err)
		return
	}
	d.logger.Info("gossip: backend agent updated",
		"node", n.Name, "service", meta.ServiceName, "weight", meta.Weight)
}

// parseAgentMeta unmarshal AgentMeta và validate các field bắt buộc.
func parseAgentMeta(raw []byte) (*AgentMeta, bool) {
	var m AgentMeta
	if err := jsonUnmarshal(raw, &m); err != nil {
		return nil, false
	}
	if m.Role != RoleBackend || m.Port <= 0 || m.ServiceName == "" {
		return nil, false
	}
	return &m, true
}

// buildServer tạo model.Server từ memberlist.Node và AgentMeta đã parse.
func buildServer(n *memberlist.Node, meta *AgentMeta) *model.Server {
	return &model.Server{
		InstanceID:  n.Name,
		ServiceName: meta.ServiceName,
		Host:        nodeHost(n),
		Port:        meta.Port,
		Weight:      meta.Weight,
		Health:      true,
	}
}

// nodeHost lấy địa chỉ IP của node.
func nodeHost(n *memberlist.Node) string {
	if n.Addr != nil {
		return n.Addr.String()
	}
	if host, _, err := net.SplitHostPort(n.Name); err == nil {
		return host
	}
	return n.Name
}
