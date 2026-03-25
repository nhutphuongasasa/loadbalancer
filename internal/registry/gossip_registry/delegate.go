package gossip_registry

import (
	"log/slog"
	"net"
	"strconv"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

// RegistryAdapter là interface tối giản mà GossipRegistry cần
// từ InMemoryRegistry — giúp dễ test/mock.
type RegistryAdapter interface {
	Register(srv *model.Server) error
	Deregister(serviceName, instanceID string) error
	UpdateStatus(srv *model.Server, alive bool)
}

// EventDelegate implement memberlist.EventDelegate
// Nhận NotifyJoin / NotifyLeave / NotifyUpdate từ cluster
type EventDelegate struct {
	registry RegistryAdapter
	queue    *broadcastQueue // để broadcast health msg khi cần
	logger   *slog.Logger
}

func newEventDelegate(r RegistryAdapter, q *broadcastQueue, log *slog.Logger) *EventDelegate {
	return &EventDelegate{registry: r, queue: q, logger: log}
}

func (d *EventDelegate) NotifyJoin(n *memberlist.Node) {
	meta, ok := parseMeta(n.Meta)
	if !ok {
		return // bỏ qua LB node hoặc node không có meta hợp lệ
	}
	srv := buildServer(n, meta)
	if err := d.registry.Register(srv); err != nil {
		d.logger.Warn("GossipRegistry: register failed",
			"node", n.Name, "err", err)
		return
	}
	d.logger.Info("GossipRegistry: backend joined",
		"node", n.Name, "addr", srv.Host, "port", srv.Port)
}

func (d *EventDelegate) NotifyLeave(n *memberlist.Node) {
	meta, ok := parseMeta(n.Meta)
	if !ok {
		return
	}
	svcName := serviceNameFromMeta(meta)
	if err := d.registry.Deregister(svcName, n.Name); err != nil {
		d.logger.Warn("GossipRegistry: deregister failed",
			"node", n.Name, "err", err)
		return
	}
	d.logger.Info("GossipRegistry: backend left", "node", n.Name)
}

func (d *EventDelegate) NotifyUpdate(n *memberlist.Node) {
	// Node update metadata (ví dụ port đổi) — re-register
	meta, ok := parseMeta(n.Meta)
	if !ok {
		return
	}
	srv := buildServer(n, meta)
	// Deregister trước, rồi register lại với meta mới
	svcName := serviceNameFromMeta(meta)
	_ = d.registry.Deregister(svcName, n.Name)
	if err := d.registry.Register(srv); err != nil {
		d.logger.Warn("GossipRegistry: re-register on update failed",
			"node", n.Name, "err", err)
	}
}

// ---helpers---

func buildServer(n *memberlist.Node, meta *NodeMeta) *model.Server {
	return &model.Server{
		InstanceID:  n.Name, // "app1", "app2"...
		ServiceName: serviceNameFromMeta(meta),
		Host:        nodeHost(n),
		Port:        meta.Port,
		Health:      true,
	}
}

// serviceNameFromMeta: hiện tại dùng role làm service name ("backend")
// Bạn có thể mở rộng NodeMeta thêm trường ServiceName sau.
func serviceNameFromMeta(m *NodeMeta) string {
	return m.Role // "backend"
}

func nodeHost(n *memberlist.Node) string {
	if n.Addr != nil {
		return n.Addr.String()
	}
	// fallback: lấy từ Name nếu encode địa chỉ trong đó
	if host, _, err := net.SplitHostPort(n.Name); err == nil {
		return host
	}
	return n.Name
}

func instanceIDFromName(name string) string { return name }

func portStr(p int) string { return strconv.Itoa(p) }
