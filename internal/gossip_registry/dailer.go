package gossip_registry

import (
	"fmt"
	"net"
	"time"
)

type SyncDataMsg struct {
	NodeName     string                       `json:"node_name"`
	ChecksumData map[string]uint64            `json:"checksum_data"`
	Data         map[string][]BackendSnapshot `json:"data"`
}

// [FIX 2026-04-24] SyncDataViaStream nhan host va syncPort thay vi *memberlist.Node
// tranh dung n.Port (port cua Memberlist) - can port rieng cho TCP sync
func (g *GossipFactory) SyncDataViaStream(peerHost string, syncPort int) {
	address := net.JoinHostPort(peerHost, fmt.Sprintf("%d", syncPort))
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		g.logger.Warn(
			"gossip: cannot dial for stream sync",
			"address", address,
			"err", err,
		)
		return
	}
	defer conn.Close()

	//gui request checksum data
	req := g.buildSyncDataMsg()

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
			"address", address,
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

// [FIX 2026-04-24] xoa tham so kind khong duoc su dung; ham chi can checksum hien tai
func (g *GossipFactory) buildSyncDataMsg() SyncDataMsg {
	checksumData := g.cluster.reg.GetChecksum()
	return SyncDataMsg{
		NodeName:     g.cluster.selfName,
		ChecksumData: checksumData,
		Data:         nil,
	}
}

func serviceNamesFrom(data map[string][]BackendSnapshot) []string {
	names := make([]string, 0, len(data))
	for serviceName := range data {
		names = append(names, serviceName)
	}
	return names
}
