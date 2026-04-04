package gossip_registry

import (
	"encoding/json"
	"net"

	"github.com/hashicorp/memberlist"
)

type msgKind byte

const (
	kindHealth msgKind = 0x01
	kindState  msgKind = 0x02
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
