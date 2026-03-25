package gossip_registry

import (
	"encoding/json"
	"time"
)

// NodeMeta khớp với struct Meta bên sidecar-agent
type NodeMeta struct {
	Role string `json:"role"` // "backend"
	Port int    `json:"port"` // 9001
}

// HealthMsg là message broadcast giữa các LB node
// khi một LB detect được health change từ gossip event
type HealthMsg struct {
	InstanceID  string    `json:"id"`
	ServiceName string    `json:"svc"`
	Alive       bool      `json:"alive"`
	Timestamp   time.Time `json:"ts"`
}

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

func encodeHealthMsg(msg HealthMsg) []byte {
	b, _ := json.Marshal(msg)
	return b
}

func decodeHealthMsg(b []byte) (*HealthMsg, bool) {
	var msg HealthMsg
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}
