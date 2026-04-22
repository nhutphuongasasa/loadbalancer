package gossip_registry

import (
	"fmt"
	"net"
	"time"

	"github.com/hashicorp/memberlist"
)

type SyncDataMsg struct {
	NodeName     string                       `json:"node_name"`
	ChecksumData map[string]uint64            `json:"checksum_data"`
	Data         map[string][]BackendSnapshot `json:"data"`
}

func (g *GossipFactory) SyncDataViaStream(n *memberlist.Node) {
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

	//gui request checksum data
	req := g.buildSyncDataMsg(kindCheckSum)

	if err := writePacket(conn, kindCheckSum, req); err != nil {
		g.logger.Warn(
			"gossip: failed to write version check request in stream sync",
			"err", err,
		)
		return
	}

	//doc phan hoi tu peer
	kind, resp, err := readPacketSyncDataMsg(conn)
	if err != nil {
		g.logger.Warn(
			"gossip: failed to read sync response in stream sync",
			"node", n.Name,
		)
		return
	}

	switch kind {
	case kindACK:
		g.logger.Debug(
			"gossip: peer acknowledged version data is equal, no need to sync",
			"from", resp.NodeName,
		)
		return

	case kindOutdatedData:
		// Merge data listener gửi về (những gì dialer đang thiếu)
		if resp.Data != nil {
			g.logger.Debug(
				"gossip: received outdated data from listener, merging",
				"from", resp.NodeName,
				"services", len(resp.Data),
			)
			converted := convertSnapshotToServers(resp.Data)
			g.cluster.reg.MergeServices(converted)
		}

		// Gửi ngược lại đúng các serviceName mà listener quan tâm
		localData := g.cluster.GetCheckRegisterAdapter().FetchByServiceNames(serviceNamesFrom(resp.Data))
		replyMsg := SyncDataMsg{
			NodeName: g.cluster.selfName,
			Data:     convertServersToSnapshot(localData),
		}
		if err := writePacket(conn, kindOutdatedData, replyMsg); err != nil {
			g.logger.Warn("gossip: failed to send local data back to listener", "err", err)
		}
	default:
		g.logger.Warn(
			"gossip: unknown stream kind received",
			"kind", kind,
		)
		return
	}

}

func (g *GossipFactory) buildSyncDataMsg(kind msgKind) SyncDataMsg {
	checksumData := g.cluster.reg.GetChecksum()
	msg := SyncDataMsg{
		NodeName:     g.cluster.selfName,
		ChecksumData: checksumData,
		Data:         nil,
	}

	return msg
}

func serviceNamesFrom(data map[string][]BackendSnapshot) []string {
	names := make([]string, 0, len(data))
	for serviceName := range data {
		names = append(names, serviceName)
	}
	return names
}
