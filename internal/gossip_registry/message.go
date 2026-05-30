package gossip_registry

import "github.com/hashicorp/memberlist"

// healthBroadcast implement memberlist.Broadcast cho HealthMsg.
type healthBroadcast struct {
	instanceID string
	msg        []byte
}

// xi li cac tin nhan bi trung, loi thoi ,...
func (b *healthBroadcast) Invalidates(other memberlist.Broadcast) bool {
	ob, ok := other.(*healthBroadcast)
	if !ok {
		return false
	}
	return b.instanceID == ob.instanceID
}

func (b *healthBroadcast) Message() []byte {
	return b.msg
}

// ham finish se duoc goi khi event duoc xu li ly xong(sau khi load den cac peer khac) thi dong channel notify
func (b *healthBroadcast) Finished() {
}

// rawBroadcast dùng cho state sync — không Invalidates nhau.
type rawBroadcast struct {
	msg []byte
}

func (b *rawBroadcast) Invalidates(memberlist.Broadcast) bool {
	return false
}

func (b *rawBroadcast) Message() []byte {
	return b.msg
}
func (b *rawBroadcast) Finished() {

}
