package sticky

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
	"github.com/nhutphuongasasa/loadbalancer/internal/model"
)

// ============================================================
// Helpers
// ============================================================

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func okTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func defaultTestCfg() *config.StickySessionConfig {
	return &config.StickySessionConfig{
		CookieName: "lb_sid",
		TTL:        10 * time.Second,
	}
}

// newBareStickyManager tạo stickyManager không có cache
// dùng cho test không cần Redis (cookie, context, session ID)
func newBareStickyManager() *stickyManager {
	return &stickyManager{
		cookieName: "lb_sid",
		sessionTTL: 10 * time.Second,
		logger:     newDiscardLogger(),
		cache:      nil,
	}
}

// ============================================================
// stickyManagerHook — override Middleware để inject mock cache
// không cần Redis thật, không thay đổi production code
// ============================================================

var errRedisDown = errors.New("redis connection refused")

type stickyManagerHook struct {
	stickyManager
	hookResult []*model.ServerPair
	hookKey    string
	hookErr    error
}

func (s *stickyManagerHook) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID, err := s.getSessionIDFromCookie(r)
		if err != nil || sessionID == "" {
			next.ServeHTTP(w, r)
			return
		}

		// inject hook thay vì gọi Redis thật
		serverPair, cacheKey, err := s.hookResult, s.hookKey, s.hookErr

		if err != nil {
			s.logger.Warn("Sticky session lookup failed", "session_id", sessionID, "err", err)
			s.clearCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		if serverPair == nil {
			s.logger.Warn("Sticky session expired or not found", "session_id", sessionID)
			s.clearCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), StickyBackendKey, serverPair)
		ctx = context.WithValue(ctx, CacheKey, cacheKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ============================================================
// generateSessionID
// ============================================================

func TestGenerateSessionID_Length(t *testing.T) {
	id := generateSessionID()
	if len(id) != 32 { // 16 bytes → 32 hex chars
		t.Errorf("expected length 32, got %d", len(id))
	}
}

func TestGenerateSessionID_Unique(t *testing.T) {
	ids := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id := generateSessionID()
		if _, exists := ids[id]; exists {
			t.Fatal("generated duplicate session ID")
		}
		ids[id] = struct{}{}
	}
}

// ============================================================
// cacheKey
// ============================================================

func TestCacheKey_Format(t *testing.T) {
	s := newBareStickyManager()
	key := s.cacheKey("abc123")
	if key != "lb:sticky:abc123" {
		t.Errorf("got %q, want lb:sticky:abc123", key)
	}
}

// ============================================================
// getSessionIDFromCookie
// ============================================================

func TestGetSessionIDFromCookie_NoCookie(t *testing.T) {
	s := newBareStickyManager()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	id, err := s.getSessionIDFromCookie(r)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty id, got %q", id)
	}
}

func TestGetSessionIDFromCookie_EmptyValue(t *testing.T) {
	s := newBareStickyManager()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "lb_sid", Value: ""})
	_, err := s.getSessionIDFromCookie(r)
	if err == nil {
		t.Error("expected error for empty cookie value")
	}
}

func TestGetSessionIDFromCookie_ValidCookie(t *testing.T) {
	s := newBareStickyManager()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "lb_sid", Value: "my-session-id"})
	id, err := s.getSessionIDFromCookie(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "my-session-id" {
		t.Errorf("got %q, want my-session-id", id)
	}
}

// ============================================================
// clearCookie
// ============================================================

func TestClearCookie_SetsMaxAgeNegative(t *testing.T) {
	s := newBareStickyManager()
	w := httptest.NewRecorder()
	s.clearCookie(w)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected cookie to be set")
	}
	if cookies[0].MaxAge != -1 {
		t.Errorf("expected MaxAge=-1, got %d", cookies[0].MaxAge)
	}
	if cookies[0].Name != "lb_sid" {
		t.Errorf("expected cookie name lb_sid, got %q", cookies[0].Name)
	}
}

// ============================================================
// GetBackendFromContext
// ============================================================

func TestGetBackendFromContext_NoValue(t *testing.T) {
	s := newBareStickyManager()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	pairs, ok := s.GetBackendFromContext(r)
	if ok || pairs != nil {
		t.Error("expected false and nil when no value in context")
	}
}

func TestGetBackendFromContext_WrongType(t *testing.T) {
	s := newBareStickyManager()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(r.Context(), StickyBackendKey, "wrong-type")
	r = r.WithContext(ctx)
	_, ok := s.GetBackendFromContext(r)
	if ok {
		t.Error("expected false for wrong type in context")
	}
}

func TestGetBackendFromContext_EmptySlice(t *testing.T) {
	s := newBareStickyManager()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(r.Context(), StickyBackendKey, []*model.ServerPair{})
	r = r.WithContext(ctx)
	_, ok := s.GetBackendFromContext(r)
	if ok {
		t.Error("expected false for empty slice")
	}
}

func TestGetBackendFromContext_ValidPairs(t *testing.T) {
	s := newBareStickyManager()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	pairs := []*model.ServerPair{{ServerName: "srv1", InstanceId: "i-123"}}
	ctx := context.WithValue(r.Context(), StickyBackendKey, pairs)
	r = r.WithContext(ctx)

	got, ok := s.GetBackendFromContext(r)
	if !ok {
		t.Fatal("expected true")
	}
	if len(got) != 1 || got[0].ServerName != "srv1" {
		t.Errorf("unexpected result: %+v", got)
	}
}

// ============================================================
// GetCacheKeyFromContext
// ============================================================

func TestGetCacheKeyFromContext_NoValue(t *testing.T) {
	s := newBareStickyManager()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if key := s.GetCacheKeyFromContext(r); key != "" {
		t.Errorf("expected empty string, got %q", key)
	}
}

func TestGetCacheKeyFromContext_WrongType(t *testing.T) {
	s := newBareStickyManager()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(r.Context(), CacheKey, 12345)
	r = r.WithContext(ctx)
	if key := s.GetCacheKeyFromContext(r); key != "" {
		t.Errorf("expected empty string for wrong type, got %q", key)
	}
}

func TestGetCacheKeyFromContext_ValidKey(t *testing.T) {
	s := newBareStickyManager()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(r.Context(), CacheKey, "lb:sticky:abc")
	r = r.WithContext(ctx)
	if key := s.GetCacheKeyFromContext(r); key != "lb:sticky:abc" {
		t.Errorf("got %q, want lb:sticky:abc", key)
	}
}

// ============================================================
// NewStickyManager
// ============================================================

func TestNewStickyManager_NilCfg_UsesDefault(t *testing.T) {
	sm := NewStickyManager(nil, newDiscardLogger(), nil)
	m := sm.(*stickyManager)
	def := config.DefaultStickySessionConfig()
	if m.sessionTTL != def.TTL {
		t.Errorf("nil cfg: got TTL %v, want %v", m.sessionTTL, def.TTL)
	}
	if m.cookieName != def.CookieName {
		t.Errorf("nil cfg: got cookieName %q, want %q", m.cookieName, def.CookieName)
	}
}

func TestNewStickyManager_CustomCfg(t *testing.T) {
	cfg := &config.StickySessionConfig{
		CookieName: "my_cookie",
		TTL:        5 * time.Minute,
	}
	sm := NewStickyManager(cfg, newDiscardLogger(), nil)
	m := sm.(*stickyManager)
	if m.sessionTTL != 5*time.Minute {
		t.Errorf("got %v, want 5m", m.sessionTTL)
	}
	if m.cookieName != "my_cookie" {
		t.Errorf("got %q, want my_cookie", m.cookieName)
	}
}

func TestNewStickyManager_ZeroTTL_UsesDefault(t *testing.T) {
	cfg := &config.StickySessionConfig{CookieName: "lb_sid", TTL: 0}
	sm := NewStickyManager(cfg, newDiscardLogger(), nil)
	m := sm.(*stickyManager)
	def := config.DefaultStickySessionConfig()
	if m.sessionTTL != def.TTL {
		t.Errorf("zero TTL should fallback to default, got %v", m.sessionTTL)
	}
}

func TestNewStickyManager_EmptyCookieName_UsesDefault(t *testing.T) {
	cfg := &config.StickySessionConfig{CookieName: "", TTL: 10 * time.Second}
	sm := NewStickyManager(cfg, newDiscardLogger(), nil)
	m := sm.(*stickyManager)
	def := config.DefaultStickySessionConfig()
	if m.cookieName != def.CookieName {
		t.Errorf("empty cookieName should fallback to default, got %q", m.cookieName)
	}
}

func TestNewStickyManager_NilLogger_UsesDefault(t *testing.T) {
	sm := NewStickyManager(defaultTestCfg(), nil, nil)
	m := sm.(*stickyManager)
	if m.logger == nil {
		t.Error("logger should not be nil")
	}
}

// ============================================================
// Middleware — dùng stickyManagerHook, không cần Redis
// ============================================================

func TestMiddleware_NoCookie_PassThrough(t *testing.T) {
	s := newBareStickyManager()
	handler := s.Middleware(okTestHandler())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestMiddleware_EmptyCookieValue_PassThrough(t *testing.T) {
	s := newBareStickyManager()
	handler := s.Middleware(okTestHandler())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "lb_sid", Value: ""})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestMiddleware_SessionExpired_ClearsCookieAndPassesThrough(t *testing.T) {
	s := &stickyManagerHook{
		stickyManager: stickyManager{
			cookieName: "lb_sid",
			sessionTTL: 10 * time.Second,
			logger:     newDiscardLogger(),
		},
		hookResult: nil, // nil = session hết hạn
		hookErr:    nil,
	}

	handler := s.Middleware(okTestHandler())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "lb_sid", Value: "expired-session"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}

	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "lb_sid" && c.MaxAge == -1 {
			found = true
		}
	}
	if !found {
		t.Error("expected cookie to be cleared (MaxAge=-1)")
	}
}

func TestMiddleware_CacheError_ClearsCookieAndPassesThrough(t *testing.T) {
	s := &stickyManagerHook{
		stickyManager: stickyManager{
			cookieName: "lb_sid",
			sessionTTL: 10 * time.Second,
			logger:     newDiscardLogger(),
		},
		hookResult: nil,
		hookErr:    errRedisDown,
	}

	handler := s.Middleware(okTestHandler())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "lb_sid", Value: "some-session"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestMiddleware_ValidSession_InjectsContext(t *testing.T) {
	pairs := []*model.ServerPair{{ServerName: "srv1", InstanceId: "i-abc"}}

	s := &stickyManagerHook{
		stickyManager: stickyManager{
			cookieName: "lb_sid",
			sessionTTL: 10 * time.Second,
			logger:     newDiscardLogger(),
		},
		hookResult: pairs,
		hookKey:    "lb:sticky:valid-session",
		hookErr:    nil,
	}

	var capturedPairs []*model.ServerPair
	captureHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		val := r.Context().Value(StickyBackendKey)
		if val != nil {
			capturedPairs = val.([]*model.ServerPair)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := s.Middleware(captureHandler)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "lb_sid", Value: "valid-session"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
	if len(capturedPairs) == 0 || capturedPairs[0].InstanceId != "i-abc" {
		t.Errorf("expected backend injected into context, got %+v", capturedPairs)
	}
}

func TestMiddleware_ValidSession_InjectsCacheKey(t *testing.T) {
	pairs := []*model.ServerPair{{ServerName: "srv1", InstanceId: "i-abc"}}

	s := &stickyManagerHook{
		stickyManager: stickyManager{
			cookieName: "lb_sid",
			logger:     newDiscardLogger(),
		},
		hookResult: pairs,
		hookKey:    "lb:sticky:my-session",
		hookErr:    nil,
	}

	var capturedKey string
	captureHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedKey = s.GetCacheKeyFromContext(r)
		w.WriteHeader(http.StatusOK)
	})

	handler := s.Middleware(captureHandler)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "lb_sid", Value: "my-session"})
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if capturedKey != "lb:sticky:my-session" {
		t.Errorf("got cacheKey %q, want lb:sticky:my-session", capturedKey)
	}
}
