package gossip_registry

import (
	"encoding/json"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
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

// day health state cua back end vao  broadcast thay đổi health của backend
// func (q *broadcastQueue) BroadcastBackendHealthChange(instanceID, svcName string, alive bool, sourceLB string, action AgentAction) {
// 	msg := HealthMsg{
// 		InstanceID:  instanceID,
// 		ServiceName: svcName,
// 		Alive:       alive,
// 		Timestamp:   time.Now().UTC(),
// 		Action:      action,
// 		SourceLB:    sourceLB,
// 	}

// 	q.queue.QueueBroadcast(
// 		&healthBroadcast{
// 			msg: encodeFrame(kindHealth, msg),
// 		},
// 	)
// }

// day health cua lb vao broadcast toàn bộ state (khi LB mới join)

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

// giu ket noi tcp de cac node khac phuc vu dong bo data custom
func (q *broadcastQueue) HandleStream(conn net.Conn) {
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	var kind [1]byte
	if _, err := conn.Read(kind[:]); err != nil {
		return // Kết nối rác hoặc lỗi mạng
	}

	switch msgKind(kind[0]) {
	case kindClusterState: // Hoặc một kind cụ thể bạn dùng cho Sync TCP
		var req SyncDataMsg
		decoder := json.NewDecoder(conn)
		if err := decoder.Decode(&req); err != nil {
			q.logger.Warn("gossip: failed to decode sync request over stream", "err", err)
			return
		}

		q.logger.Debug("gossip: stream sync request received", "from", req.NodeName, "version", req.VersionData)

		if req.VersionData < registry.VersionDataBackend {
			resp := SyncDataMsg{
				NodeName:    q.cluster.GetSelfName(),
				VersionData: registry.VersionDataBackend,
				Data:        q.cluster.BuildSnapshot(),
			}

			encoder := json.NewEncoder(conn)
			if err := encoder.Encode(resp); err != nil {
				q.logger.Error("gossip: failed to send sync response over stream", "err", err)
			}
		}

	default:
		q.logger.Warn("gossip: unknown stream kind received", "kind", kind[0])
	}
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

	case kindCheckVersion:
		var msg SyncDataMsg
		if err := json.Unmarshal(payload, &msg); err != nil {
			q.logger.Warn(
				"gossip: failed to unmarshal check version msg",
				"err", err,
			)
			return
		}

		q.logger.Debug(
			"gossip: check version received",
			"from", msg.NodeName,
			"version", msg.VersionData,
		)

		if q.cluster != nil {
			if msg.VersionData == registry.VersionDataBackend {

			} else {
				if msg.VersionData < registry.VersionDataBackend {

				} else {
				}
			}
		}
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
	backends := q.cluster.BuildSnapshot()
	if len(backends) == 0 {
		return nil
	}

	msg := ClusterStateMsg{
		Backends:  backends,
		FromLB:    q.cluster.GetSelfName(),
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		q.logger.Error("gossip: failed to marshal LocalState", "err", err)
		return nil
	}

	q.logger.Debug("gossip: LocalState called",
		"join", join,
		"backends", len(backends),
	)
	return data
}

func (q *broadcastQueue) MergeRemoteState(buf []byte, join bool) {
	if len(buf) == 0 {
		return
	}

	var msg ClusterStateMsg
	if err := json.Unmarshal(buf, &msg); err != nil {
		q.logger.Warn("gossip: failed to unmarshal MergeRemoteState", "err", err)
		return
	}

	q.logger.Debug("gossip: MergeRemoteState called",
		"join", join,
		"from", msg.FromLB,
		"backends", len(msg.Backends),
	)

	if q.cluster != nil {
		q.cluster.MergeState(msg)
	}
}
