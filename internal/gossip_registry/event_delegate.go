package gossip_registry

import (
	"log/slog"

	"github.com/hashicorp/memberlist"
)

// EventDelegate implement memberlist.EventDelegate.
// noi phan loai event delaget de day xuong agent event va cluster event
type EventDelegate struct {
	clusterDelegate *ClusterDelegate
	agentDelegate   *AgentDelegate
	logger          *slog.Logger
}

func newEventDelegate(
	cd *ClusterDelegate,
	ad *AgentDelegate,
	log *slog.Logger,
) *EventDelegate {
	return &EventDelegate{
		clusterDelegate: cd,
		agentDelegate:   ad,
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
