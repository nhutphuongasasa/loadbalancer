package gossip_registry

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
)

// jsonUnmarshal là alias nội bộ để các file khác trong package dùng chung.
var jsonUnmarshal = json.Unmarshal
var jsonMarshal = json.Marshal

// ─── Broadcast Types ──────────────────────────────────────────────────────────

// msgKind phân biệt loại message trong broadcast để router đúng handler.
type msgKind byte

const (
	kindHealth msgKind = 0x01
	kindState  msgKind = 0x02
)

// framedMsg: 1 byte kind + payload JSON
// Format: [kind][json bytes...]
type framedMsg struct {
	kind    msgKind
	payload []byte
}

func encodeFrame(kind msgKind, v any) []byte {
	b, _ := jsonMarshal(v)
	out := make([]byte, 1+len(b))
	out[0] = byte(kind)
	copy(out[1:], b)
	return out
}

func decodeFrame(b []byte) (msgKind, []byte, bool) {
	if len(b) < 2 {
		return 0, nil, false
	}
	return msgKind(b[0]), b[1:], true
}

// ─── healthBroadcast ──────────────────────────────────────────────────────────

// healthBroadcast implement memberlist.Broadcast cho HealthMsg.
type healthBroadcast struct {
	msg    []byte
	notify chan struct{}
}

// Invalidates: nếu 2 message cùng InstanceID thì message cũ bị thay thế.
func (b *healthBroadcast) Invalidates(other memberlist.Broadcast) bool {
	ob, ok := other.(*healthBroadcast)
	if !ok {
		return false
	}
	_, myPayload, ok1 := decodeFrame(b.msg)
	_, otherPayload, ok2 := decodeFrame(ob.msg)
	if !ok1 || !ok2 {
		return false
	}
	var thisMsg, otherMsg HealthMsg
	_ = jsonUnmarshal(myPayload, &thisMsg)
	_ = jsonUnmarshal(otherPayload, &otherMsg)
	return thisMsg.InstanceID == otherMsg.InstanceID
}

func (b *healthBroadcast) Message() []byte { return b.msg }

func (b *healthBroadcast) Finished() {
	if b.notify != nil {
		close(b.notify)
	}
}

// ─── broadcastQueue ───────────────────────────────────────────────────────────

// broadcastQueue quản lý hàng đợi broadcast và implement memberlist.Delegate.
// Nó nhận raw bytes từ gossip, decode frame, rồi route sang đúng handler:
//   - kindHealth → RegistryAdapter.UpdateStatus
//   - kindState  → ClusterManager.MergeState
type broadcastQueue struct {
	mu      sync.Mutex
	queue   *memberlist.TransmitLimitedQueue
	reg     registry.RegistryAdapter
	cluster ClusterEventHandler // interface, tránh import cycle
	logger  *slog.Logger
}

// ClusterEventHandler là interface để broadcastQueue gọi vào ClusterManager
// mà không cần import trực tiếp (giúp tránh circular dep nếu tách package sau).
type ClusterEventHandler interface {
	// MergeState xử lý khi nhận được ClusterStateMsg từ LB node khác.
	MergeState(msg ClusterStateMsg)
	// OnHealthBroadcast thông báo để ClusterManager cập nhật tracking.
	OnHealthBroadcast(msg HealthMsg)
}

func newBroadcastQueue(
	reg registry.RegistryAdapter,
	cluster ClusterEventHandler,
	log *slog.Logger,
) *broadcastQueue {
	q := &broadcastQueue{
		reg:     reg,
		cluster: cluster,
		logger:  log,
	}
	q.queue = &memberlist.TransmitLimitedQueue{
		NumNodes:       func() int { return 3 }, // sẽ được inject sau khi có list
		RetransmitMult: 2,
	}
	return q
}

// SetNumNodesFunc inject hàm lấy số member thực tế từ memberlist.
func (q *broadcastQueue) SetNumNodesFunc(fn func() int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queue.NumNodes = fn
}

// ─── Gửi broadcasts ───────────────────────────────────────────────────────────

// BroadcastHealthChange đẩy HealthMsg vào queue để broadcast ra cluster.
// Gọi bởi GossipRegistry khi LB detect health change từ active health check.
func (q *broadcastQueue) BroadcastHealthChange(instanceID, svcName string, alive bool, sourceLB string) {
	msg := HealthMsg{
		InstanceID:  instanceID,
		ServiceName: svcName,
		Alive:       alive,
		Timestamp:   time.Now(),
		SourceLB:    sourceLB,
	}
	q.queue.QueueBroadcast(&healthBroadcast{
		msg: encodeFrame(kindHealth, msg),
	})
}

// BroadcastState đẩy ClusterStateMsg để sync toàn bộ backend list sang LB mới join.
func (q *broadcastQueue) BroadcastState(msg ClusterStateMsg) {
	// State msg không cần Invalidates (mỗi lần là snapshot mới)
	q.queue.QueueBroadcast(&rawBroadcast{
		msg: encodeFrame(kindState, msg),
	})
}

// rawBroadcast dùng cho state sync — không Invalidates nhau.
type rawBroadcast struct {
	msg []byte
}

func (b *rawBroadcast) Invalidates(memberlist.Broadcast) bool { return false }
func (b *rawBroadcast) Message() []byte                       { return b.msg }
func (b *rawBroadcast) Finished()                             {}

// ─── memberlist.Delegate impl ─────────────────────────────────────────────────

func (q *broadcastQueue) NodeMeta(_ int) []byte { return nil }

// NotifyMsg nhận message từ gossip, decode frame rồi route đúng handler.
func (q *broadcastQueue) NotifyMsg(b []byte) {
	if len(b) == 0 {
		return
	}
	kind, payload, ok := decodeFrame(b)
	if !ok {
		return
	}

	switch kind {
	case kindHealth:
		var msg HealthMsg
		if err := jsonUnmarshal(payload, &msg); err != nil {
			return
		}
		// 1. Cập nhật registry (trạng thái backend)
		q.reg.UpdateStatus(msg.InstanceID, msg.ServiceName, msg.Alive)
		// 2. Thông báo cluster manager theo dõi
		if q.cluster != nil {
			q.cluster.OnHealthBroadcast(msg)
		}
		q.logger.Debug("gossip: health broadcast received",
			"instance", msg.InstanceID,
			"alive", msg.Alive,
			"from_lb", msg.SourceLB,
		)

	case kindState:
		var msg ClusterStateMsg
		if err := jsonUnmarshal(payload, &msg); err != nil {
			return
		}
		if q.cluster != nil {
			q.cluster.MergeState(msg)
		}
		q.logger.Debug("gossip: state sync received",
			"from_lb", msg.FromLB,
			"backends", len(msg.Backends),
		)
	}
}

func (q *broadcastQueue) GetBroadcasts(overhead, limit int) [][]byte {
	return q.queue.GetBroadcasts(overhead, limit)
}

// LocalState được gọi khi join cluster — gửi toàn bộ state của LB này.
// ClusterManager sẽ build snapshot và trả về bytes.
func (q *broadcastQueue) LocalState(_ bool) []byte {
	if q.cluster == nil {
		return nil
	}
	// Không implement ở đây — xem MergeRemoteState
	return nil
}

func (q *broadcastQueue) MergeRemoteState(_ []byte, _ bool) {}
