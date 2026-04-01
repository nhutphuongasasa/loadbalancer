package gossip_registry

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
)

// broadcastQueue quản lý broadcast queue và implement memberlist.Delegate
type broadcastQueue struct {
	queue   *memberlist.TransmitLimitedQueue
	reg     registry.RegistryAdapter
	cluster ClusterEventHandler
	logger  *slog.Logger

	mu       sync.RWMutex // Dùng RWMutex để giảm contention
	selfMeta []byte       // Cache marshal sẵn (tối ưu NodeMeta)
	role     string
}

func newBroadcastQueue(
	reg registry.RegistryAdapter,
	cluster ClusterEventHandler,
	logger *slog.Logger,
) *broadcastQueue {
	q := &broadcastQueue{
		reg:     reg,
		cluster: cluster,
		logger:  logger,
	}

	q.queue = &memberlist.TransmitLimitedQueue{
		NumNodes:       func() int { return 3 }, // sẽ được override sau
		RetransmitMult: 3,
	}

	return q
}

// SetNumNodesFunc inject hàm trả về số node hiện tại trong cluster.
func (q *broadcastQueue) SetNumNodesFunc(fn func() int) {
	q.queue.NumNodes = fn
}

// setup metadate cho node nay truoc khi join cluster
func (q *broadcastQueue) SetSelfMeta(meta interface{}) {
	if meta == nil {
		q.mu.Lock()
		q.selfMeta = nil
		q.mu.Unlock()
		return
	}

	data, err := json.Marshal(meta)
	if err != nil {
		q.logger.Error(
			"gossip: failed to marshal node meta",
			"err", err,
		)
		return
	}

	q.mu.Lock()
	q.selfMeta = data
	q.mu.Unlock()
}

// BroadcastHealthChange broadcast thay đổi health của backend
func (q *broadcastQueue) BroadcastHealthChange(instanceID, svcName string, alive bool, sourceLB string) {
	msg := HealthMsg{
		InstanceID:  instanceID,
		ServiceName: svcName,
		Alive:       alive,
		Timestamp:   time.Now().UTC(),
		SourceLB:    sourceLB,
	}

	q.queue.QueueBroadcast(
		&healthBroadcast{
			msg: encodeFrame(kindHealth, msg),
		},
	)
}

// BroadcastState broadcast toàn bộ state (khi LB mới join)
func (q *broadcastQueue) BroadcastState(msg ClusterStateMsg) {
	q.queue.QueueBroadcast(
		&rawBroadcast{
			msg: encodeFrame(kindState, msg),
		},
	)
}

// cung cpa du lieu metadata chinh node nay cho metadata cua struct node trong memberlist
// chi duoc goi 1 lan khi join cluster
func (q *broadcastQueue) NodeMeta(limit int) []byte {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if len(q.selfMeta) == 0 || len(q.selfMeta) > limit {
		return nil
	}
	return q.selfMeta
}

// ham xu li message duoc lan truyen tu cac node lb khac trong cluster
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
		if err := json.Unmarshal(payload, &msg); err != nil {
			q.logger.Warn("gossip: failed to unmarshal health msg", "err", err)
			return
		}

		q.reg.UpdateStatus(msg.InstanceID, msg.ServiceName, msg.Alive)

		if q.cluster != nil {
			q.cluster.OnHealthBroadcast(msg)
		}

		q.logger.Debug("gossip: health received",
			"instance", msg.InstanceID,
			"alive", msg.Alive,
			"from", msg.SourceLB,
		)

	case kindState:
		var msg ClusterStateMsg
		if err := json.Unmarshal(payload, &msg); err != nil {
			q.logger.Warn("gossip: failed to unmarshal state msg", "err", err)
			return
		}

		if q.cluster != nil {
			q.cluster.MergeState(msg)
		}

		q.logger.Debug("gossip: state sync received",
			"from", msg.FromLB,
			"backends", len(msg.Backends),
		)
	}
}

// GetBroadcasts trả về các message đang chờ broadcast. Được memberlist gọi định kỳ.
func (q *broadcastQueue) GetBroadcasts(overhead, limit int) [][]byte {
	return q.queue.GetBroadcasts(overhead, limit)
}

func (q *broadcastQueue) LocalState(join bool) []byte {
	// Hiện tại không cần sync full state qua LocalState
	return nil
}

func (q *broadcastQueue) MergeRemoteState(buf []byte, join bool) {
	// Không dùng vì đang xử lý qua NotifyMsg + BroadcastState
}
