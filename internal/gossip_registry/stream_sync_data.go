package gossip_registry

import (
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

	//gui request check version data
	req := g.buildSyncDataMsg(kindCheckVersion)

	if err := writePacket(conn, kindCheckVersion, req); err != nil {
		g.logger.Warn(
			"gossip: failed to write version check request in stream sync",
			"err", err,
		)
		return
	}

	//doc phan hoi tu peer sau khi gui request check version
	kind, resp, err := readPacketSyncDataMsg(conn)
	if err != nil {
		g.logger.Warn(
			"gossip: failed to read sync response in stream sync",
			"node", n.Name,
		)
		return
	}

	switch kind {
	case kindOk:
		//version data bang nhau khong can lam gi ca
		g.logger.Debug(
			"gossip: peer acknowledged version data is equal, no need to sync",
			"from", resp.NodeName,
			"version", resp.VersionData,
		)
		return
	case kindRequestFullData:
		//yeu cau lay du lieu data
		//build snapshot data va gui qua ben node yeu cau
		req := g.buildSyncDataMsg(kindRequestFullData)
		if err := writePacket(conn, kindRequestFullData, req); err != nil {
			g.logger.Warn(
				"gossip: failed to write full data request in stream sync",
				"err", err,
			)
			return
		}
		var finalStatus msgKind
		finalStatus, _, err = readPacket(conn)
		if err != nil {
			g.logger.Warn(
				"gossip: failed to read final status in stream sync",
				"err", err,
			)
			return
		}

		//kiem tra trang thai cap nhat data cua peer sau khi gui du lieu
		if finalStatus == kindOk {
			g.logger.Debug(
				"gossip: received OK response after full data request, no need to sync",
				"from", resp.NodeName,
				"version", resp.VersionData,
			)
		} else {
			g.logger.Warn(
				"gossip: peer failed to apply sync data",
				"from", resp.NodeName,
				"version", resp.VersionData,
			)
		}

		return
	case kindOutdatedData:
		//peer tra ve ket qua version local dang loi thoi
		g.logger.Warn(
			"gossip: peer reported local version data is outdated, starting sync data via peer",
			"from", resp.NodeName,
			"version", resp.VersionData,
		)
		if err := g.cluster.MergeRemoteState(resp); err != nil {
			g.logger.Warn("gossip: failed to merge state from peer", "err", err)
			return
		}
		g.logger.Info("gossip: merged outdated local state from peer", "from", resp.NodeName)
		return
	default:
		g.logger.Warn(
			"gossip: unknown stream kind received",
			"kind", kind,
		)
		return
	}

}

func (g *GossipRegistry) buildSyncDataMsg(kind msgKind) SyncDataMsg {
	msg := SyncDataMsg{
		NodeName:    g.selfName,
		VersionData: registry.VersionDataBackend,
		Data:        nil,
	}
	if kind == kindRequestFullData {
		data := g.cluster.BuildSnapshot()
		msg.Data = data
		return msg
	}
	return msg
}
