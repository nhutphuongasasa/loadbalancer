package gossip_registry

import (
	"encoding/json"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
)

type msgKind byte

const (
	RoleBackend = "backend"
	RoleLB      = "lb"
)

const (
	kindHealthAndWeight msgKind = 0x01
	kindClusterState    msgKind = 0x02
	kindSecurity        msgKind = 0x03
	kindACK             msgKind = 0x07
	kindError           msgKind = 0x08
	kindOutdatedData    msgKind = 0x12
	kindCheckSum        msgKind = 0x13
)

const (
	ActionJoin   AgentAction = "join"
	ActionLeave  AgentAction = "leave"
	ActionUpdate AgentAction = "update"
)

// AgentMeta la metadata cua backend agent node.
type AgentMeta struct {
	Role        string `json:"role"`
	ServiceName string `json:"service_name"`
	InstanceID  string `json:"instance_id"`
	Port        int    `json:"port"`
	Weight      int    `json:"weight"`
}

// LBMeta la metadata cua mot LB node trong cluster.
type LBMeta struct {
	Role          string `json:"role"`
	NodeName      string `json:"node_name"`
	AdvertisePort int    `json:"advertise_port"`
	BindPort      int    `json:"bind_port"`
	// [FIX 2026-04-24] them SyncPort: port rieng cho TCP custom sync stream (= BindPort + 1000)
	SyncPort int `json:"sync_port"`
}

func roleFromRaw(raw []byte) string {
	var probe struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	return probe.Role
}

type LBNodeInfo struct {
	Name     string
	Host     string
	BindPort int
	// [FIX 2026-04-24] them SyncPort de luu port TCP sync cua peer LB
	SyncPort int
	JoinedAt time.Time
}

type HealthMsg struct {
	InstanceID  string      `json:"instance_id"`
	ServiceName string      `json:"service_name"`
	Alive       bool        `json:"alive"`
	Timestamp   time.Time   `json:"timestamp"`
	Action      AgentAction `json:"action"`
	Host        string      `json:"host"`
	Port        int         `json:"port"`
	Weight      int         `json:"weight"`
	SourceLB    string      `json:"source_lb"`
}

type ClusterStateMsg struct {
	Backends  map[string][]BackendSnapshot `json:"backends"`
	FromLB    string                       `json:"from_lb"`
	Timestamp time.Time                    `json:"timestamp"`
}

type BackendSnapshot struct {
	InstanceID  string `json:"instance_id"`
	ServiceName string `json:"service_name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Weight      int    `json:"weight"`
	Alive       bool   `json:"alive"`
}

type ClusterEventHandler interface {
	GetCheckRegisterAdapter() registry.RegistryAdapter
	OnHealthBroadcast(msg HealthMsg)
	BuildSnapshot() map[string][]BackendSnapshot
	// GetSelfName() string
	// MergeRemoteState(msg SyncDataMsg) error
}

type AgentAction string
