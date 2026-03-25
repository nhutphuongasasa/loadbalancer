package gossip_registry

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

// healthBroadcast implement memberlist.Broadcast
// Mỗi health-change message sẽ được wrap vào đây để broadcast
type healthBroadcast struct {
	msg    []byte
	notify chan struct{}
}

func (b *healthBroadcast) Invalidates(other memberlist.Broadcast) bool {
	// Nếu 2 msg cùng InstanceID thì msg mới invalidate msg cũ
	var thisMsg, otherMsg HealthMsg
	_ = json.Unmarshal(b.msg, &thisMsg)

	ob, ok := other.(*healthBroadcast)
	if !ok {
		return false
	}
	_ = json.Unmarshal(ob.msg, &otherMsg)
	return thisMsg.InstanceID == otherMsg.InstanceID
}

func (b *healthBroadcast) Message() []byte { return b.msg }

func (b *healthBroadcast) Finished() {
	if b.notify != nil {
		close(b.notify)
	}
}

// broadcastQueue quản lý hàng đợi broadcast và implement memberlist.Delegate
type broadcastQueue struct {
	mu       sync.Mutex
	queue    *memberlist.TransmitLimitedQueue
	registry RegistryAdapter
	logger   *slog.Logger
}

func newBroadcastQueue(registry RegistryAdapter, log *slog.Logger) *broadcastQueue {
	q := &broadcastQueue{
		registry: registry,
		logger:   log,
	}
	q.queue = &memberlist.TransmitLimitedQueue{
		NumNodes:       func() int { return 3 }, // sẽ được update sau khi có list
		RetransmitMult: 2,
	}
	return q
}

// SetNumNodesFunc cho phép inject hàm lấy số node thực tế từ memberlist
func (q *broadcastQueue) SetNumNodesFunc(fn func() int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queue.NumNodes = fn
}

// BroadcastHealthChange gửi health msg ra toàn cluster
func (q *broadcastQueue) BroadcastHealthChange(instanceID, svcName string, alive bool) {
	msg := HealthMsg{
		InstanceID:  instanceID,
		ServiceName: svcName,
		Alive:       alive,
		Timestamp:   time.Now(),
	}
	q.queue.QueueBroadcast(&healthBroadcast{msg: encodeHealthMsg(msg)})
}

// --- implement memberlist.Delegate ---

func (q *broadcastQueue) NodeMeta(_ int) []byte { return nil }

func (q *broadcastQueue) NotifyMsg(b []byte) {
	if len(b) == 0 {
		return
	}
	msg, ok := decodeHealthMsg(b)
	if !ok {
		return
	}

	// Tìm server trong registry và update
	// Vì RegistryAdapter không có GetByID, ta dùng UpdateStatus với dummy server
	srv := &model.Server{ //nolint:exhaustruct
		InstanceID:  msg.InstanceID,
		ServiceName: msg.ServiceName,
	}
	q.registry.UpdateStatus(srv, msg.Alive)

	q.logger.Debug("GossipRegistry: received health broadcast",
		"instance", msg.InstanceID,
		"alive", msg.Alive)
}

func (q *broadcastQueue) GetBroadcasts(overhead, limit int) [][]byte {
	return q.queue.GetBroadcasts(overhead, limit)
}

func (q *broadcastQueue) LocalState(_ bool) []byte          { return nil }
func (q *broadcastQueue) MergeRemoteState(_ []byte, _ bool) {}
