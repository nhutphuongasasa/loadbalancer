// strategies/ip_hash.go
package strategies

import (
	"hash/crc32"
	"net"
	"strings"
	"sync/atomic"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

type iphashView struct {
	servers []*model.Server
}

type IPHash struct {
	ptr atomic.Pointer[iphashView]
}

func NewIPHash() Strategy {
	return &IPHash{}
}

// Update: chỉ chạy ở single-writer (applyStateChange)
func (ih *IPHash) Update(backends []*model.Server) {
	if len(backends) == 0 {
		ih.ptr.Store(nil)
		return
	}

	// Copy slice để atomic swap an toàn (không bị mutate từ ngoài)
	servers := make([]*model.Server, len(backends))
	copy(servers, backends)

	ih.ptr.Store(&iphashView{servers: servers})
}

// Pick: hoàn toàn lock-free, hot-path cực nhanh
func (ih *IPHash) Pick(clientIP string) *model.Server {
	view := ih.ptr.Load()
	if view == nil || len(view.servers) == 0 {
		return nil
	}

	ip := extractIP(clientIP)
	if ip == "" {
		// fallback: lấy server đầu tiên trong danh sách healthy
		return view.servers[0]
	}

	hash := crc32.ChecksumIEEE([]byte(ip))
	index := int(hash % uint32(len(view.servers)))

	return view.servers[index]
}

func extractIP(addr string) string {
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	host = strings.Trim(host, "[]")
	return host
}
