package rate_limit

import (
	"net"
	"net/http"
	"strings"
)

type TrustedProxies []string

func (t TrustedProxies) IsTrusted(ip string) bool {
	for _, p := range t {
		if p == ip || strings.HasPrefix(ip, p) {
			return true
		}
	}
	return false
}

// Lay ip thuc cua client, bo qua cac proxy tin cay
func RealIP(r *http.Request, trusted TrustedProxies) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ipStr := strings.TrimSpace(parts[i])
			ip, _, err := net.SplitHostPort(ipStr + ":80")
			if err != nil {
				ip = ipStr
			}
			if !trusted.IsTrusted(ip) {
				return ip
			}
		}
	}

	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
		return real
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
