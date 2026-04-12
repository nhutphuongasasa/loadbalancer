package gossip_registry

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"

	"github.com/hashicorp/memberlist"
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

func writeBytes(kind msgKind, conn net.Conn, q *broadcastQueue) error {
	if _, err := conn.Write([]byte{byte(kind)}); err != nil {
		return err
	}
	return nil
}
