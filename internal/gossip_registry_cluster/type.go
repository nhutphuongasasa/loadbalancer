package gossip_registry

import (
	"encoding/json"
	"time"
)

/*
Package gossip_registry dinh nghia giao thuc trao doi du lieu cho he thong Service Discovery.

Cu the file nay xu ly:
- Dinh nghia NodeMeta: Metadata cua cac backend node (phai khop voi sidecar-agent).
- HealthMsg: Cau truc ban tin broadcast de thong bao trang thai song/chet (Alive/Dead) cua service.
- Helper functions: Ho tro viec dong goi (Marshal) va giai ma (Unmarshal) du lieu JSON.
*/

// NodeMeta phai khop voi struct Meta ben sidecar-agent de nhan thong tin tuong ung
type NodeMeta struct {
	Role   string `json:"role"`
	Weight int    `json:"weight"`
	Port   int    `json:"port"`
}

// HealthMsg la message broadcast giua cac LB node
// Khi co 1 LB detect duoc health change tu gossip event
type HealthMsg struct {
	InstanceID  string    `json:"instance_id"`
	ServiceName string    `json:"server_name"`
	Alive       bool      `json:"alive"`
	Timestamp   time.Time `json:"timestamp"`
}

// Giai ma metadata snag NodeMeta
func parseMeta(raw []byte) (*NodeMeta, bool) {
	var m NodeMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	if m.Role != "backend" || m.Port <= 0 {
		return nil, false
	}
	return &m, true
}

// helper chueyn health message thanh byte
func encodeHealthMsg(msg HealthMsg) []byte {
	b, _ := json.Marshal(msg)
	return b
}

// helper chuyen byte thanh health message struct
func decodeHealthMsg(b []byte) (*HealthMsg, bool) {
	var msg HealthMsg
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}
