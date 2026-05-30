package gossip_registry

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

// encodeFrame encapsulates message voi 1 byte header de phan biet loai message.
func encodeFrame(kind msgKind, v any) []byte {
	b, _ := json.Marshal(v)
	out := make([]byte, 1+len(b))
	out[0] = byte(kind)
	copy(out[1:], b)
	return out
}

// decodeFrame tra ve msgKind va payload tu raw bytes, kiem tra do dai toi thieu.
func decodeFrame(b []byte) (msgKind, []byte, bool) {
	if len(b) < 2 {
		return 0, nil, false
	}
	return msgKind(b[0]), b[1:], true
}

// nodeHost lay dia chi IP cua node.
func nodeHost(n *memberlist.Node) string {
	if n.Addr != nil {
		return n.Addr.String()
	}
	if host, _, err := net.SplitHostPort(n.Name); err == nil {
		return host
	}
	return n.Name
}

func writeSeedBytes(kind msgKind, conn net.Conn, q *broadcastQueue) {
	if err := writeBytes(kind, conn, q); err != nil {
		q.logger.Debug(
			"gossip: failed to write seed bytes",
			"kind", kind,
			"err", err,
		)
	}
}

// helper write packet with header 5 bytes: 1 byte Kind + 4 bytes Length + data
func writePacket(conn net.Conn, kind msgKind, v any) error {
	var payload []byte
	if v != nil {
		var err error
		payload, err = json.Marshal(v)
		if err != nil {
			return err
		}
	}

	// Header 5 bytes: 1 byte Kind + 4 bytes Length
	packet := make([]byte, 5+len(payload))
	packet[0] = byte(kind)
	binary.BigEndian.PutUint32(packet[1:5], uint32(len(payload)))

	if len(payload) > 0 {
		copy(packet[5:], payload)
	}

	_, err := conn.Write(packet)
	return err
}

func readPacketSyncDataMsg(conn net.Conn) (kind msgKind, msg SyncDataMsg, err error) {
	kind, payload, err := readPacket(conn)
	if err != nil {
		return 0, SyncDataMsg{}, err
	}

	if err := json.Unmarshal(payload, &msg); err != nil {
		return 0, SyncDataMsg{}, err
	}

	return kind, msg, nil
}

// helper giai ma packet with header 5 bytes: 1 byte Kind + 4 bytes Length
func readPacket(conn net.Conn) (msgKind, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, err
	}

	kind := msgKind(header[0])
	length := binary.BigEndian.Uint32(header[1:5])

	if length == 0 {
		return kind, nil, nil
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return 0, nil, err
	}

	return kind, payload, nil
}

func writeBytes(kind msgKind, conn net.Conn, q *broadcastQueue) error {
	if _, err := conn.Write([]byte{byte(kind)}); err != nil {
		return err
	}
	return nil
}

// convertServersToSnapshot chuyển map[serviceName]map[instanceID]*model.Server
// thành map[serviceName][]BackendSnapshot để gửi qua wire
func convertServersToSnapshot(data map[string]map[string]*model.Server) map[string][]BackendSnapshot {
	result := make(map[string][]BackendSnapshot, len(data))

	for serviceName, instances := range data {
		snapshots := make([]BackendSnapshot, 0, len(instances))
		for _, srv := range instances {
			snapshots = append(snapshots, BackendSnapshot{
				InstanceID:  srv.InstanceID,
				ServiceName: srv.ServiceName,
				Host:        srv.Host,
				Port:        srv.Port,
				Weight:      srv.GetWeight(),
				Alive:       srv.IsHealthy(),
			})
		}
		result[serviceName] = snapshots
	}

	return result
}

// convertSnapshotToServers chuyển đổi map[serviceName][]BackendSnapshot
// thành map[serviceName]map[instanceID]*model.Server để merge vào registry
func convertSnapshotToServers(data map[string][]BackendSnapshot) map[string]map[string]*model.Server {
	result := make(map[string]map[string]*model.Server, len(data))

	for serviceName, snapshots := range data {
		instances := make(map[string]*model.Server, len(snapshots))
		for _, snap := range snapshots {
			srv := model.NewServer(
				snap.InstanceID,
				snap.ServiceName,
				snap.Host,
				snap.Port,
				snap.Weight,
				nil, // metadata
				nil, // transport
			)
			srv.SetAlive(snap.Alive)
			instances[snap.InstanceID] = srv
		}
		result[serviceName] = instances
	}

	return result
}
