package strategies

import (
	"hash/crc32"
	"net"
	"strings"

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

type IPHash struct{}

func NewIPHash() *IPHash {
	return &IPHash{}
}

func (ih *IPHash) Pick(servers []*model.Server, clientIP string) *model.Server {
	if len(servers) == 0 {
		return nil
	}

	ip := extractIP(clientIP)
	if ip == "" {
		return ih.fallback(servers)
	}

	hash := crc32.ChecksumIEEE([]byte(ip))

	// Chỉ xét healthy servers
	var healthy []*model.Server
	for _, srv := range servers {
		if srv.IsHealthy() {
			healthy = append(healthy, srv)
		}
	}

	if len(healthy) == 0 {
		return ih.fallback(servers)
	}

	index := int(hash % uint32(len(healthy)))

	return healthy[index]
}

func (ih *IPHash) fallback(servers []*model.Server) *model.Server {
	if len(servers) == 0 {
		return nil
	}
	return servers[0] // hoặc random nếu muốn
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
