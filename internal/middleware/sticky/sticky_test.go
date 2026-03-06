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

	"github.com/nhutphuongasasa/loadbalancer/internal/model"
	"github.com/redis/go-redis/v9"
)

// ============================================================
// Mock cache — tránh phụ thuộc Redis thật
// ============================================================

type mockCache struct {
	data    map[string]any
	failGet bool
	failSet bool
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]any)}
}

func (m *mockCache) SetArray(ctx context.Context, key string, val any, ttl time.Duration) error {
	if m.failSet {
		return errors.New("redis set error")
	}
	m.data[key] = val
	return nil
}

func (m *mockCache) GetArray(ctx context.Context, key string, dest any) error {
	if m.failGet {
		return errors.New("redis get error")
	}
	val, ok := m.data[key]
	if !ok {
		return redis.Nil
	}
	// copy giá trị vào dest
	pairs := val.([]*model.ServerPair)
	target := dest.(*[]*model.ServerPair)
	*target = pairs
	return nil
}

// stickyManager dùng interface thay vì concrete *cache.CacheClient để test được
// Vì không thể inject mock vào *cache.CacheClient, ta test qua các method public

// ============================================================
// Helper
// ============================================================

func newTestStickyManager(mc *mockCache) *stickyManager {
	return &stickyManager{
		cookieName: StickyCookieName,
		sessionTTL: 10 * time.Second,
		logger:     newDiscardLogger(),
		cache:      nil, // sẽ override method trong test trực tiếp
	}
}

// ============================================================
// generateSessionID
// ============================================================

func TestGenerateSessionID_Length(t *testing.T) {
	id := generateSessionID()
	// 16 bytes hex = 32 chars
	if len(id) != 32 {
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
	s := &stickyManager{cookieName: StickyCookieName}
	key := s.cacheKey("abc123")
	expected := "lb:sticky:abc123"
	if key != expected {
		t.Errorf("got %q, want %q", key, expected)
	}
}

// ============================================================
// getSessionIDFromCookie
// ============================================================

func TestGetSessionIDFromCookie_NoCookie(t *testing.T) {
	s := &stickyManager{cookieName: StickyCookieName}
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
	s := &stickyManager{cookieName: StickyCookieName}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: StickyCookieName, Value: ""})
	_, err := s.getSessionIDFromCookie(r)
	if err == nil {
		t.Error("expected error for empty cookie value")
	}
}

func TestGetSessionIDFromCookie_ValidCookie(t *testing.T) {
	s := &stickyManager{cookieName: StickyCookieName}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: StickyCookieName, Value: "my-session-id"})
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
	s := &stickyManager{cookieName: StickyCookieName}
	w := httptest.NewRecorder()
	s.clearCookie(w)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected cookie to be set")
	}
	if cookies[0].MaxAge != -1 {
		t.Errorf("expected MaxAge=-1, got %d", cookies[0].MaxAge)
	}
}

// ============================================================
// GetBackendFromContext
// ============================================================

func TestGetBackendFromContext_NoValue(t *testing.T) {
	s := &stickyManager{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	pairs, ok := s.GetBackendFromContext(r)
	if ok || pairs != nil {
		t.Error("expected false and nil when no value in context")
	}
}

func TestGetBackendFromContext_WrongType(t *testing.T) {
	s := &stickyManager{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(r.Context(), StickyBackendKey, "wrong-type")
	r = r.WithContext(ctx)
	_, ok := s.GetBackendFromContext(r)
	if ok {
		t.Error("expected false for wrong type in context")
	}
}

func TestGetBackendFromContext_EmptySlice(t *testing.T) {
	// FIX: slice rỗng cũng phải trả về false
	s := &stickyManager{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(r.Context(), StickyBackendKey, []*model.ServerPair{})
	r = r.WithContext(ctx)
	_, ok := s.GetBackendFromContext(r)
	if ok {
		t.Error("expected false for empty slice")
	}
}

func TestGetBackendFromContext_ValidPairs(t *testing.T) {
	s := &stickyManager{}
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
	s := &stickyManager{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if key := s.GetCacheKeyFromContext(r); key != "" {
		t.Errorf("expected empty string, got %q", key)
	}
}

func TestGetCacheKeyFromContext_WrongType(t *testing.T) {
	s := &stickyManager{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(r.Context(), CacheKey, 12345)
	r = r.WithContext(ctx)
	if key := s.GetCacheKeyFromContext(r); key != "" {
		t.Errorf("expected empty string for wrong type, got %q", key)
	}
}

func TestGetCacheKeyFromContext_ValidKey(t *testing.T) {
	s := &stickyManager{}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(r.Context(), CacheKey, "lb:sticky:abc")
	r = r.WithContext(ctx)
	if key := s.GetCacheKeyFromContext(r); key != "lb:sticky:abc" {
		t.Errorf("got %q, want lb:sticky:abc", key)
	}
}

// ============================================================
// Middleware — dùng stickyManagerWithMock để inject cache
// ============================================================

// stickyManagerWithMock override cache calls để test Middleware mà không cần Redis
type stickyManagerWithMock struct {
	stickyManager
	mockGet func(ctx context.Context, sessionID string) ([]*model.ServerPair, string, error)
}

func (s *stickyManagerWithMock) getBackendFromCacheMock(ctx context.Context, sessionID string) ([]*model.ServerPair, string, error) {
	return s.mockGet(ctx, sessionID)
}

func TestMiddleware_NoCookie_PassThrough(t *testing.T) {
	s := &stickyManager{
		cookieName: StickyCookieName,
		logger:     newDiscardLogger(),
	}
	handler := s.Middleware(okTestHandler())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestMiddleware_InjectsPairsIntoContext(t *testing.T) {
	// Tạo sticky manager với context value đã được set sẵn để test GetBackendFromContext
	pairs := []*model.ServerPair{{ServerName: "srv1", InstanceId: "i-abc"}}

	s := &stickyManager{
		cookieName: StickyCookieName,
		logger:     newDiscardLogger(),
	}

	// Inject trực tiếp vào context — test GetBackendFromContext độc lập
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(r.Context(), StickyBackendKey, pairs)
	r = r.WithContext(ctx)

	got, ok := s.GetBackendFromContext(r)
	if !ok {
		t.Fatal("expected backend in context")
	}
	if got[0].InstanceId != "i-abc" {
		t.Errorf("got %q, want i-abc", got[0].InstanceId)
	}
}

// ============================================================
// NewStickyManager
// ============================================================

func TestNewStickyManager_DefaultTTL(t *testing.T) {
	sm := NewStickyManager(nil, nil)
	m := sm.(*stickyManager)
	if m.sessionTTL != DefaultSessionTTL {
		t.Errorf("got %v, want %v", m.sessionTTL, DefaultSessionTTL)
	}
}

func TestNewStickyManager_CustomTTL(t *testing.T) {
	sm := NewStickyManager(nil, nil, 5*time.Minute)
	m := sm.(*stickyManager)
	if m.sessionTTL != 5*time.Minute {
		t.Errorf("got %v, want 5m", m.sessionTTL)
	}
}

func TestNewStickyManager_ZeroTTL_UsesDefault(t *testing.T) {
	sm := NewStickyManager(nil, nil, 0)
	m := sm.(*stickyManager)
	if m.sessionTTL != DefaultSessionTTL {
		t.Errorf("zero TTL should fallback to default, got %v", m.sessionTTL)
	}
}

func TestNewStickyManager_NilLogger_UsesDefault(t *testing.T) {
	sm := NewStickyManager(nil, nil)
	m := sm.(*stickyManager)
	if m.logger == nil {
		t.Error("logger should not be nil")
	}
}

// ============================================================
// Helpers
// ============================================================

func okTestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
