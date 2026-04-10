package gossip_registry

import (
	"encoding/json"
	"time"
)

const (
	RoleBackend = "backend"
	RoleLB      = "lb"
)

// AgentMeta là metadata của backend agent node.
type AgentMeta struct {
	Role        string `json:"role"` // luôn = RoleBackend
	ServiceName string `json:"service_name"`
	InstanceID  string `json:"instance_id"`
	Port        int    `json:"port"`
	Weight      int    `json:"weight"`
}

// LBMeta là metadata của một LB node trong cluster.
type LBMeta struct {
	Role     string `json:"role"` // luôn = RoleLB
	BindPort int    `json:"bind_port"`
	// HTTPAPI  int    `json:"http_api"`
}

// roleFromRaw đọc trường role từ raw JSON meta mà không unmarshal toàn bộ.
func roleFromRaw(raw []byte) string {
	// unmarshal nhẹ chỉ lấy role
	var probe struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	return probe.Role
}

// chua thong tin cua ban than
type LBNodeInfo struct {
	Name     string
	Host     string
	BindPort int
	// HTTPAPI  int
	JoinedAt time.Time
}

// HealthMsg là message broadcast giữa các LB node khi phát hiện health change
// của một backend instance (từ active health check hoặc gossip event).
type HealthMsg struct {
	InstanceID  string      `json:"instance_id"`
	ServiceName string      `json:"service_name"`
	Alive       bool        `json:"alive"`
	Timestamp   time.Time   `json:"timestamp"`
	Action      AgentAction `json:"action"`
	Host        string      `json:"host"`
	Port        int         `json:"port"`
	Weight      int         `json:"weight"`
	// SourceLB là LB node nào phát hiện ra health change này.
	// Dùng để tránh vòng lặp broadcast và để debug.
	SourceLB string `json:"source_lb"`
}

// ClusterStateMsg là message đồng bộ toàn bộ trạng thái backend list
// khi một LB node mới join cluster (full-state sync).
type ClusterStateMsg struct {
	// Backends là snapshot danh sách backend hiện tại của LB node gửi.
	Backends  map[string][]BackendSnapshot `json:"backends"`
	FromLB    string                       `json:"from_lb"`
	Timestamp time.Time                    `json:"timestamp"`
}

// BackendSnapshot là snapshot của một backend instance tại thời điểm sync.
type BackendSnapshot struct {
	InstanceID  string `json:"instance_id"`
	ServiceName string `json:"service_name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Weight      int    `json:"weight"`
	Alive       bool   `json:"alive"`
}

// ClusterEventHandler là interface để broadcastQueue gọi vào ClusterManager
type ClusterEventHandler interface {
	MergeState(msg ClusterStateMsg)
	OnHealthBroadcast(msg HealthMsg)
	BuildSnapshot() map[string][]BackendSnapshot
	GetSelfName() string
}

type AgentAction string

const (
	ActionJoin   AgentAction = "join"
	ActionLeave  AgentAction = "leave"
	ActionUpdate AgentAction = "update"
)
