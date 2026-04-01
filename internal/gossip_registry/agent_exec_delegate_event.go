package gossip_registry

import (
	"encoding/json"
	"log/slog"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
)

type AgentDelegate struct {
	registryList registry.RegistryAdapter
	logger       *slog.Logger
}

// xu ly logic khi join cac agent vao danh sach back end
func (a *AgentDelegate) onAgentJoin(n *memberlist.Node) {
	meta, ok := parseAgentMeta(n.Meta)
	if !ok {
		a.logger.Warn("gossip: bad agent meta on join", "node", n.Name)
		return
	}
	// Build nhanh Server model day vao danh sach vào registry.
	srv := buildServer(n, meta)
	if err := a.registryList.Register(srv); err != nil {
		a.logger.Warn(
			"gossip: register agent failed",
			"node", n.Name,
			"service", meta.ServiceName,
			"instance_id", meta.InstanceID,
			"err", err,
		)
		return
	}
	a.logger.Info("gossip: backend agent joined",
		"node", n.Name,
		"service", meta.ServiceName,
		"addr", srv.Host,
		"port", srv.Port,
		"weight", meta.Weight,
	)
}

// xu ly logic khi leave cac agent khoi danh sach back end
func (a *AgentDelegate) onAgentLeave(n *memberlist.Node) {
	meta, ok := parseAgentMeta(n.Meta)
	if !ok {
		a.logger.Warn(
			"gossip: bad agent meta on leave, cannot deregister without service_name",
			"node", n.Name,
		)
		return
	}
	if err := a.registryList.Deregister(meta.ServiceName, meta.InstanceID); err != nil {
		a.logger.Warn(
			"gossip: deregister agent failed",
			"node", n.Name,
			"service", meta.ServiceName,
			"instance_id", meta.InstanceID,
			"err", err,
		)
		return
	}
	a.logger.Info(
		"gossip: backend agent left",
		"node", n.Name,
		"service", meta.ServiceName,
	)
}

func (a *AgentDelegate) onAgentUpdate(n *memberlist.Node) {
	meta, ok := parseAgentMeta(n.Meta)
	if !ok {
		a.logger.Warn(
			"gossip: bad agent meta on update",
			"node", n.Name,
		)
		return
	}
	srv := buildServer(n, meta)
	// Deregister với service name cũ (có thể không đổi), rồi register lại
	_ = a.registryList.Deregister(meta.ServiceName, meta.InstanceID)
	if err := a.registryList.Register(srv); err != nil {
		a.logger.Warn(
			"gossip: re-register agent on update failed",
			"node", n.Name,
			"service", meta.ServiceName,
			"instance_id", meta.InstanceID,
			"err", err,
		)
		return
	}
	a.logger.Info(
		"gossip: backend agent updated",
		"node", n.Name,
		"service", meta.ServiceName,
		"weight", meta.Weight,
	)
}

// helper parseAgentMeta unmarshal AgentMeta va validate cac field can thiet.
func parseAgentMeta(raw []byte) (*AgentMeta, bool) {
	var m AgentMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	if m.Role != RoleBackend || m.Port <= 0 || m.ServiceName == "" {
		return nil, false
	}
	return &m, true
}
