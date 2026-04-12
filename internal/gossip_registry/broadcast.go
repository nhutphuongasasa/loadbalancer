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

	//doc byte dau tien de biet yeu cau
	kind, req, err := readPacketSyncDataMsg(conn)
	if err != nil {
		q.logger.Warn(
			"gossip: failed to read packet sync data msg",
			"err", err,
		)
		return
	}

	switch kind {
	case kindCheckVersion:
		localVersion := registry.VersionDataBackend

		if req.VersionData == localVersion {
			//version ban nhau khong can gui du lieu
			q.logger.Info(
				"gossip: version data is equal in stream sync",
				"from", req.NodeName,
				"version", req.VersionData,
			)

			writeSeedBytes(kindOk, conn, q)
			return
		} else if req.VersionData > localVersion {
			//local version  dang cu cna duoc cap nhat tu node gui reqquest
			writeSeedBytes(kindOutdatedData, conn, q)

			//nhan data snapshot du lieu tu peer
			kind, SyncDataMsg, err := readPacketSyncDataMsg(conn)
			if err != nil {
				q.logger.Warn(
					"gossip: failed to read packet sync data msg",
					"err", err,
				)
				return
			}
			if kind != kindRequestFullData {
				q.logger.Warn(
					"gossip: unexpected kind received in stream sync",
					"kind", kind,
				)
				return
			}

			//tien hanh cap nhat data cua local lb
			q.logger.Debug(
				"Starting sync data local lb ",
				"from peer", SyncDataMsg.NodeName,
				"version merge remote", SyncDataMsg.VersionData,
				"data", SyncDataMsg.Data,
			)

			ok := q.cluster.MergeRemoteState(SyncDataMsg)

			//gui response OK sau khi cap nhat xong
			if ok != nil {
				writeSeedBytes(kindFailed, conn, q)
				q.logger.Warn(
					"gossip: failed to merge full data from peer",
					"from", SyncDataMsg.NodeName,
					"version", SyncDataMsg.VersionData,
					"err", ok,
				)
			} else {
				writeSeedBytes(kindOk, conn, q)
				q.logger.Debug(
					"gossip: successfully merged full data from peer",
					"from", SyncDataMsg.NodeName,
					"version", SyncDataMsg.VersionData,
				)
			}

			q.logger.Info(
				"gossip: successfully merged full data from peer",
				"from", SyncDataMsg.NodeName,
				"version", SyncDataMsg.VersionData,
			)
			return
		} else {
			//version cua peer bi outdated, can cap nhat lai peer
			//gui ca seed byte lan du lieu
			q.logger.Warn(
				"gossip: version data is outdated in stream sync",
				"from", req.NodeName,
				"version", req.VersionData,
			)

			SyncDataMsg := q.buildSyncDataMsg(kindOutdatedData)
			writePacket(conn, kindOutdatedData, SyncDataMsg)
			return
		}
	default:
		q.logger.Warn("gossip: unknown stream kind received", "kind", kind)
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
	return nil
}

func (q *broadcastQueue) MergeRemoteState(buf []byte, join bool) {
}

func (q *broadcastQueue) buildSyncDataMsg(kind msgKind) SyncDataMsg {
	msg := SyncDataMsg{
		NodeName:    q.cluster.GetSelfName(),
		VersionData: registry.VersionDataBackend,
		Data:        nil,
	}
	if kind == kindRequestFullData {
		data := q.cluster.BuildSnapshot()
		msg.Data = data
	}
	return msg
}
