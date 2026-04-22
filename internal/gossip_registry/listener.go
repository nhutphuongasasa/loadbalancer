package gossip_registry

import (
	"net"
	"time"
)

// giu ket noi tcp de cac node khac phuc vu dong bo data custom
func (g *GossipFactory) HandleStream(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	//doc 5 byte dau tien de biet kind va length
	kind, msg, err := readPacketSyncDataMsg(conn)
	if err != nil {
		g.logger.Warn(
			"gossip: failed to read packet sync data msg",
			"err", err,
		)
		return
	}

	switch kind {
	case kindCheckSum:
		//thuc hien check data khac biet
		result := g.cluster.GetCheckRegisterAdapter().CompareAndFetch(msg.ChecksumData)

		if result == nil {
			//khong co su khac biet tra ve kindOk
			writePacket(conn, kindOk, nil)
		} else {
			//co su khac biet tra ve danh danh sach backend qua ben kia
			//nhan danh sach back end cua ben kia tu so sanh
			//neu chua co thi them vao neu co roi thi check version cua back end ai cao hon thi dung cai do cung luc dong ket noi tcp

		}

	default:
		g.logger.Warn("gossip: unknown stream kind received", "kind", kind)
	}
}
