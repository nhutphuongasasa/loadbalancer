package rate_limit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func newTestLimiter(r rate.Limit, b int) *ipRateLimiter {
	lim := NewIPRateLimiter(r, b, nil)
	lim.Start()
	return lim
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

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

func TestMiddleware_AllowsUnderLimit(t *testing.T) {
	// bucket = 5, rate = 5/s — 3 request đầu phải pass
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

	// Exhaust
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

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "2.2.2.2:80"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("IP B should not be affected, got %d", w.Code)
	}
}

func TestStartStop(t *testing.T) {
	lim := newTestLimiter(10, 10)
	lim.Stop()
}

func TestStartIdempotent(t *testing.T) {
	lim := NewIPRateLimiter(10, 10, nil)
	lim.Start()
	lim.Start()
	lim.Stop()
}

func TestStopIdempotent(t *testing.T) {
	lim := newTestLimiter(10, 10)
	lim.Stop()
	lim.Stop()
}

func TestCleanUp_RemovesStaleIPs(t *testing.T) {
	lim := newTestLimiter(10, 10)
	defer lim.Stop()

	lim.GetLimiter("stale-ip")
	lim.mux.Lock()
	lim.ips["stale-ip"].lastSeen.Store(time.Now().Add(-5 * time.Minute).UnixNano())
	lim.mux.Unlock()

	lim.mux.Lock()
	for ip, value := range lim.ips {
		if time.Since(value.getLastSeen()) > 3*time.Minute {
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

	lim.mux.Lock()
	for ip, value := range lim.ips {
		if time.Since(value.getLastSeen()) > 3*time.Minute {
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
