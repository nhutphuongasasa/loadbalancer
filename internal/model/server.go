// model/server.go
package model

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

type ServerPair struct {
	ServerName string
	InstanceId string
}

type Input struct {
	ServiceName string            `json:"serviceName"`
	InstanceID  string            `json:"instanceID,omitempty"`
	Host        string            `json:"host,omitempty"`
	Port        int               `json:"port,omitempty"`
	Weight      int               `json:"weight,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type Server struct {
	InstanceID    string
	ServiceName   string
	Host          string
	Port          int
	Health        bool
	LastHeartbeat time.Time
	Metadata      map[string]string
	TTL           time.Duration
	mux           sync.RWMutex
	Weight        int
	proxy         *httputil.ReverseProxy
	activeConns   int32
}

func NewServer(
	id, serviceName, host string,
	port, weight int,
	metadata map[string]string,
	transport http.RoundTripper,
) *Server {
	if weight <= 0 {
		weight = 10
	}

	u := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", host, port),
	}

	proxy := httputil.NewSingleHostReverseProxy(u)
	originalDirector := proxy.Director

	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		if _, ok := req.Header["X-Forwarded-For"]; !ok {
			req.Header.Set("X-Forwarded-For", req.RemoteAddr)
		} else {
			req.Header.Set("X-Forwarded-For", req.Header.Get("X-Forwarded-For")+", "+req.RemoteAddr)
		}

		req.Header.Set("X-Forwarded-By", "My-Load-Balancer")
		req.Header.Set("X-Source-UI", "true")
		req.Header.Set("X-Target-Instance-ID", id)
		req.Host = u.Host
		req.Header.Set("X-Forwarded-Proto", "https")
	}

	// Transport (có thể custom timeout, keep-alive, TLS, v.v.)
	if transport != nil {
		proxy.Transport = transport
	}

	// Error handler khi backend down / timeout
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "Backend service unreachable or unavailable", http.StatusServiceUnavailable)
	}

	proxy.Transport = transport

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "Backend service unreachable or unavailable", http.StatusServiceUnavailable)
	}

	return &Server{
		InstanceID:    id,
		ServiceName:   serviceName,
		Host:          host,
		Port:          port,
		Health:        true,
		LastHeartbeat: time.Now(),
		Metadata:      metadata,
		TTL:           30 * time.Second,
		Weight:        weight,
		proxy:         proxy,
	}
}

func (s *Server) GetID() string {
	return s.InstanceID
}

func (s *Server) GetServiceName() string {
	return s.ServiceName
}

func (s *Server) GetAddr() string {
	return fmt.Sprintf("http://%s:%d", s.Host, s.Port)
}

func (s *Server) GetMetadata() map[string]string {
	s.mux.RLock()
	defer s.mux.RUnlock()
	copyMap := make(map[string]string, len(s.Metadata))
	for k, v := range s.Metadata {
		copyMap[k] = v
	}
	return copyMap
}

func (s *Server) IsHealthy() bool {
	s.mux.RLock()
	defer s.mux.RUnlock()
	return s.Health
}

func (s *Server) GetLastHeartbeat() time.Time {
	s.mux.RLock()
	defer s.mux.RUnlock()
	return s.LastHeartbeat
}

func (s *Server) SetAlive(status bool) {
	s.mux.Lock()
	s.Health = status
	if status {
		s.LastHeartbeat = time.Now()
	}
	s.mux.Unlock()
}

func (s *Server) GetWeight() int {
	s.mux.RLock()
	defer s.mux.RUnlock()
	if s.Weight > 0 {
		return s.Weight
	}
	return 1
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.proxy.ServeHTTP(w, r)
}

func (s *Server) GetProxy() *httputil.ReverseProxy {
	return s.proxy
}

func (s *Server) IncConn() {
	atomic.AddInt32(&s.activeConns, 1)
}

func (s *Server) DecConn() {
	atomic.AddInt32(&s.activeConns, -1)
}

func (s *Server) GetActiveConns() int32 {
	return atomic.LoadInt32(&s.activeConns)
}
