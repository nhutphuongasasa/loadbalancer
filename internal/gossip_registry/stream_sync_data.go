package gossip_registry

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/nhutphuongasasa/loadbalancer/internal/registry"
)

type SyncDataMsg struct {
	NodeName    string                       `json:"node_name"`
	VersionData registry.VersionData         `json:"version_data"`
	Data        map[string][]BackendSnapshot `json:"data"`
}

func (g *GossipRegistry) SyncDataViaStream(n *memberlist.Node) {
	address := net.JoinHostPort(n.Addr.String(), fmt.Sprintf("%d", n.Port))
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		g.logger.Warn(
			"gossip: cannot dial for stream sync",
			"node", n.Name,
			"err", err,
		)
		return
	}
	defer conn.Close()

	//gui byte de nhan dien requset check version
	if _, err := conn.Write([]byte{byte(kindClusterState)}); err != nil {
		return
	}

	//gui request check version data
	req := g.buildSyncDataMsg(kindCheckVersion)

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return
	}

	var resp SyncDataMsg
	decoder := json.NewDecoder(conn)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	//khong pahn hoi nghia la version bang nhau
	if err := decoder.Decode(&resp); err != nil {
		g.logger.Debug(
			"gossip: version data is equal in stream sync",
			"node", n.Name,
			"err", err,
		)
		return
	}

	if resp.Data != nil && g.cluster != nil {
		g.logger.Info(
			"gossip: version data is outdated",
			"from", resp.NodeName,
			"version", resp.VersionData,
		)

	}

}

func (g *GossipRegistry) buildSyncDataMsg(kind msgKind) SyncDataMsg {
	msg := SyncDataMsg{
		NodeName:    g.selfName,
		VersionData: registry.VersionDataBackend,
		Data:        nil,
	}
	switch kind {
	case kindCheckVersion:
		msg.Data = nil
		return msg

	case kindRequestFullData:
		data := g.cluster.BuildSnapshot()
		msg.Data = data
		return msg
	default:
		return msg
	}
}
