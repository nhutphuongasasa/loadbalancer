package gossip_registry

import (
	"log/slog"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
)

// EventDelegate implement memberlist.EventDelegate.
// Chi xu i cac event co  role=backend nghia la chi xu ly eventt cua aget.
// Các node role=lb được forward sang ClusterDelegate.
type EventDelegate struct {
	registryList    registry.RegistryAdapter
	clusterDelegate *ClusterDelegate
	agentDelegate   *AgentDelegate
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
		registryList:    r,
		clusterDelegate: cd,
		queue:           q,
		logger:          log,
	}
}

// NotifyJoin duoc goi khi bat ky node nào join cluster.
func (d *EventDelegate) NotifyJoin(n *memberlist.Node) {
	role := roleFromRaw(n.Meta)
	switch role {
	case RoleBackend:
		d.agentDelegate.onAgentJoin(n)
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

// NotifyLeave duoc goi khi bat ky node nào leave.
func (d *EventDelegate) NotifyLeave(n *memberlist.Node) {
	role := roleFromRaw(n.Meta)
	switch role {
	case RoleBackend:
		d.agentDelegate.onAgentLeave(n)
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

// NotifyUpdate duoc goi khi metadata của node thay doi.
func (d *EventDelegate) NotifyUpdate(n *memberlist.Node) {
	role := roleFromRaw(n.Meta)
	switch role {
	case RoleBackend:
		d.agentDelegate.onAgentUpdate(n)
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
