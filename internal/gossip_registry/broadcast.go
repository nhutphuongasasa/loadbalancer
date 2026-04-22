package gossip_registry

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/hashicorp/memberlist"
)

// broadcastQueue quản lý broadcast queue và implement memberlist.Delegate
type broadcastQueue struct {
	queue   *memberlist.TransmitLimitedQueue
	cluster ClusterEventHandler
	logger  *slog.Logger

	mu       sync.RWMutex // Dùng RWMutex để giảm contention
	selfMeta []byte       // Cache marshal sẵn (tối ưu NodeMeta)
	role     string
}

func newBroadcastQueue(
	cluster ClusterEventHandler,
	logger *slog.Logger,
) *broadcastQueue {
	q := &broadcastQueue{
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

// cung cpa du lieu metadata chinh node nay cho metadata cua struct node trong memberlist
// chi duoc goi 1 lan khi emberlist.Create() hoac Join(),
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
	case kindHealthAndWeight:
		var msg HealthMsg
		if err := json.Unmarshal(payload, &msg); err != nil {
			q.logger.Warn(
				"gossip: failed to unmarshal health msg",
				"err", err,
			)
			return
		}

		if q.cluster != nil {
			q.cluster.OnHealthBroadcast(msg)
		}

		q.logger.Debug(
			"gossip: health received",
			"instance", msg.InstanceID,
			"alive", msg.Alive,
			"from", msg.SourceLB,
		)

	case kindSecurity:
		// handle security msg (nếu có)

	default:
		q.logger.Warn("gossip: unknown msg kind", "kind", kind)
	}
}

// tra ve cac message broadcast san sang duoc gui di
func (q *broadcastQueue) GetBroadcasts(overhead, limit int) [][]byte {
	return q.queue.GetBroadcasts(overhead, limit)
}

func (q *broadcastQueue) LocalState(join bool) []byte {
	return nil
}

func (q *broadcastQueue) MergeRemoteState(buf []byte, join bool) {
}

// func (q *broadcastQueue) buildSyncDataMsg(kind msgKind) SyncDataMsg {
// 	msg := SyncDataMsg{
// 		NodeName: q.cluster.GetSelfName(),
// 		Data:     nil,
// 	}
// 	if kind == kindRequestFullData {
// 		data := q.cluster.BuildSnapshot()
// 		msg.Data = data
// 	}
// 	return msg
// }
