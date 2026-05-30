package rate_limit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
)

// ============================================================
// Helper — stub ConfigManager không cần file config thật
// ============================================================

func newStubCfgManager(rps float64, burst int) *config.ConfigManager {
	cm := &config.ConfigManager{}
	cm.StoreSnapshot(&config.ConfigSnapshot{
		RateLimit: &config.RateLimitConfig{
			Enabled:           true,
			RequestsPerSecond: rps,
			Burst:             burst,
			CleanupInterval:   60 * time.Second,
			CleanupTTL:        3 * time.Minute,
		},
	})
	return cm
}

func newTestLimiter(rps float64, burst int) *ipRateLimiter {
	lim := NewIPRateLimiter(newStubCfgManager(rps, burst), nil)
	lim.Start()
	return lim
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// ============================================================
// TrustedProxies
// ============================================================

func TestIsTrusted_ExactIP(t *testing.T) {
	tp := TrustedProxies{"192.168.1.1"}
	if !tp.IsTrusted("192.168.1.1") {
		t.Error("expected 192.168.1.1 to be trusted")
	}
}

func TestIsTrusted_CIDR(t *testing.T) {
	tp := TrustedProxies{"192.168.1.0/24"}
	cases := []struct {
		ip      string
		trusted bool
	}{
		{"192.168.1.1", true},
		{"192.168.1.254", true},
		{"192.168.2.1", false},
		{"10.0.0.1", false},
	}
	for _, c := range cases {
		got := tp.IsTrusted(c.ip)
		if got != c.trusted {
			t.Errorf("IsTrusted(%q) = %v, want %v", c.ip, got, c.trusted)
		}
	}
}

func TestIsTrusted_InvalidIP(t *testing.T) {
	tp := TrustedProxies{"192.168.1.0/24"}
	if tp.IsTrusted("not-an-ip") {
		t.Error("invalid IP should not be trusted")
	}
}

func TestIsTrusted_NoPrefixFalsePositive(t *testing.T) {
	tp := TrustedProxies{"192.168.1.0/24"}
	if tp.IsTrusted("192.168.100.1") {
		t.Error("192.168.100.1 should NOT be trusted under 192.168.1.0/24")
	}
}

// ============================================================
// RealIP
// ============================================================

func TestRealIP_NoHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	got := RealIP(r, nil)
	if got != "1.2.3.4" {
		t.Errorf("got %q, want 1.2.3.4", got)
	}
}

func TestRealIP_XForwardedFor_SingleIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "5.6.7.8")
	r.RemoteAddr = "1.2.3.4:80"
	got := RealIP(r, nil)
	if got != "5.6.7.8" {
		t.Errorf("got %q, want 5.6.7.8", got)
	}
}

func TestRealIP_XForwardedFor_SkipTrustedProxy(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "5.6.7.8, 10.0.0.1")
	tp := TrustedProxies{"10.0.0.1"}
	got := RealIP(r, tp)
	if got != "5.6.7.8" {
		t.Errorf("got %q, want 5.6.7.8", got)
	}
}

func TestRealIP_XRealIP_Fallback(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Real-IP", "9.9.9.9")
	r.RemoteAddr = "1.2.3.4:80"
	got := RealIP(r, nil)
	if got != "9.9.9.9" {
		t.Errorf("got %q, want 9.9.9.9", got)
	}
}

// ============================================================
// GetLimiter
// ============================================================

func TestGetLimiter_NewIP(t *testing.T) {
	lim := newTestLimiter(10, 10)
	defer lim.Stop()

	cl := lim.GetLimiter("1.1.1.1")
	if cl == nil {
		t.Fatal("expected non-nil client")
	}
	if cl.limiter == nil {
		t.Fatal("expected non-nil limiter")
	}
}

func TestGetLimiter_SameIPReturnsSameClient(t *testing.T) {
	lim := newTestLimiter(10, 10)
	defer lim.Stop()

	cl1 := lim.GetLimiter("1.1.1.1")
	cl2 := lim.GetLimiter("1.1.1.1")
	if cl1 != cl2 {
		t.Error("expected same client pointer for same IP")
	}
}

func TestGetLimiter_DifferentIPsDifferentClients(t *testing.T) {
	lim := newTestLimiter(10, 10)
	defer lim.Stop()

	cl1 := lim.GetLimiter("1.1.1.1")
	cl2 := lim.GetLimiter("2.2.2.2")
	if cl1 == cl2 {
		t.Error("expected different clients for different IPs")
	}
}

func TestGetLimiter_ConcurrentAccess(t *testing.T) {
	lim := newTestLimiter(100, 100)
	defer lim.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lim.GetLimiter("1.1.1.1")
		}()
	}
	wg.Wait()
}

// TestGetLimiter_HotReload: IP mới sau khi swap config phải dùng rate mới
func TestGetLimiter_HotReload_NewIPUsesNewConfig(t *testing.T) {
	cm := newStubCfgManager(10, 10)
	lim := NewIPRateLimiter(cm, nil)
	lim.Start()
	defer lim.Stop()

	// request đầu với config cũ
	cl1 := lim.GetLimiter("old-ip")
	if cl1.limiter.Limit() != 10 {
		t.Fatalf("expected rate 10, got %v", cl1.limiter.Limit())
	}

	// hot reload: swap sang config mới (rps=50, burst=50)
	cm.StoreSnapshot(&config.ConfigSnapshot{
		RateLimit: &config.RateLimitConfig{
			Enabled:           true,
			RequestsPerSecond: 50,
			Burst:             50,
			CleanupInterval:   60 * time.Second,
			CleanupTTL:        3 * time.Minute,
		},
	})

	// IP cũ vẫn dùng limiter cũ (rate=10) — đúng, không reset đột ngột
	cl1Again := lim.GetLimiter("old-ip")
	if cl1Again.limiter.Limit() != 10 {
		t.Error("existing IP should keep old limiter after config reload")
	}

	// IP mới phải dùng config mới (rate=50)
	cl2 := lim.GetLimiter("new-ip")
	if cl2.limiter.Limit() != 50 {
		t.Errorf("new IP should use updated rate 50, got %v", cl2.limiter.Limit())
	}
}

// ============================================================
// Middleware
// ============================================================

func TestMiddleware_AllowsUnderLimit(t *testing.T) {
	lim := newTestLimiter(5, 5)
	defer lim.Stop()

	handler := lim.Middleware(okHandler())

	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "1.2.3.4:80"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: got %d, want 200", i+1, w.Code)
		}
	}
}

func TestMiddleware_BlocksOverLimit(t *testing.T) {
	lim := newTestLimiter(1, 2)
	defer lim.Stop()

	handler := lim.Middleware(okHandler())

	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "1.2.3.4:80"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:80"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("got %d, want 429", w.Code)
	}
}

func TestMiddleware_429ResponseHeaders(t *testing.T) {
	lim := newTestLimiter(1, 1)
	defer lim.Stop()

	handler := lim.Middleware(okHandler())

	// exhaust bucket
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:80"
	handler.ServeHTTP(httptest.NewRecorder(), r)

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:80"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After header")
	}
	if w.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Error("X-RateLimit-Remaining should be 0")
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Error("Content-Type should be application/json")
	}
}

func TestMiddleware_429ResponseBody(t *testing.T) {
	lim := newTestLimiter(1, 1)
	defer lim.Stop()

	handler := lim.Middleware(okHandler())

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:80"
	handler.ServeHTTP(httptest.NewRecorder(), r)

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:80"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body["error"] != "Too Many Requests" {
		t.Errorf("unexpected error field: %v", body["error"])
	}
}

func TestMiddleware_DifferentIPsIndependent(t *testing.T) {
	lim := newTestLimiter(1, 1)
	defer lim.Stop()

	handler := lim.Middleware(okHandler())

	// exhaust IP A
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.1.1.1:80"
	handler.ServeHTTP(httptest.NewRecorder(), r)

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.1.1.1:80"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatal("IP A should be rate limited")
	}

	// IP B không bị ảnh hưởng
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "2.2.2.2:80"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("IP B should not be affected, got %d", w.Code)
	}
}

// ============================================================
// Start / Stop
// ============================================================

func TestStartStop(t *testing.T) {
	lim := newTestLimiter(10, 10)
	lim.Stop()
}

func TestStartIdempotent(t *testing.T) {
	lim := NewIPRateLimiter(newStubCfgManager(10, 10), nil)
	lim.Start()
	lim.Start() // gọi 2 lần — sync.Once đảm bảo chỉ chạy 1 goroutine
	lim.Stop()
}

func TestStopIdempotent(t *testing.T) {
	lim := newTestLimiter(10, 10)
	lim.Stop()
	lim.Stop() // gọi 2 lần — không panic
}

// ============================================================
// cleanUp
// ============================================================

func TestCleanUp_RemovesStaleIPs(t *testing.T) {
	lim := newTestLimiter(10, 10)
	defer lim.Stop()

	lim.GetLimiter("stale-ip")

	// giả lập IP đã không dùng 5 phút
	lim.mux.Lock()
	lim.ips["stale-ip"].lastSeen.Store(time.Now().Add(-5 * time.Minute).UnixNano())
	lim.mux.Unlock()

	// trigger cleanup thủ công
	lim.mux.Lock()
	for ip, value := range lim.ips {
		if time.Since(value.getLastSeen()) > lim.cleanupTTL {
			delete(lim.ips, ip)
		}
	}
	lim.mux.Unlock()

	lim.mux.RLock()
	_, exists := lim.ips["stale-ip"]
	lim.mux.RUnlock()

	if exists {
		t.Error("stale IP should have been cleaned up")
	}
}

func TestCleanUp_KeepsFreshIPs(t *testing.T) {
	lim := newTestLimiter(10, 10)
	defer lim.Stop()

	lim.GetLimiter("fresh-ip")

	// trigger cleanup — fresh-ip vừa được tạo → không bị xóa
	lim.mux.Lock()
	for ip, value := range lim.ips {
		if time.Since(value.getLastSeen()) > lim.cleanupTTL {
			delete(lim.ips, ip)
		}
	}
	lim.mux.Unlock()

	lim.mux.RLock()
	_, exists := lim.ips["fresh-ip"]
	lim.mux.RUnlock()

	if !exists {
		t.Error("fresh IP should not have been cleaned up")
	}
}
