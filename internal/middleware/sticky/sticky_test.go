package sticky

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"log/slog"
	"os"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
)

// ============================================================
// Helpers dung chung
// ============================================================

var (
	testKey16 = []byte("1234567890123456")                 // AES-128
	testKey24 = []byte("123456789012345678901234")         // AES-192
	testKey32 = []byte("12345678901234567890123456789012") // AES-256
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func newTestManager(t *testing.T) IStickier {
	t.Helper()
	cfg := &config.StickySessionConfig{
		CookieName:    "test_sticky",
		TTL:           5 * time.Minute,
		EncryptionKey: testKey32,
		Secure:        false,
	}
	m, err := NewStickyManager(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewStickyManager: %v", err)
	}
	return m
}

// ============================================================
// payload_test
// ============================================================

func TestNewPayload_Success(t *testing.T) {
	p, err := newPayload("auth", "auth-01", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("payload is nil")
	}
	id, ok := p.GetInstanceID("auth")
	if !ok || id != "auth-01" {
		t.Errorf("expected auth-01, got %q (ok=%v)", id, ok)
	}
	if p.ExpiresAt <= time.Now().Unix() {
		t.Error("ExpiresAt should be in the future")
	}
}

func TestNewPayload_EmptyArgs(t *testing.T) {
	cases := []struct {
		service  string
		instance string
	}{
		{"", "auth-01"},
		{"auth", ""},
		{"", ""},
	}
	for _, c := range cases {
		_, err := newPayload(c.service, c.instance, time.Minute)
		if err == nil {
			t.Errorf("expected error for service=%q instance=%q", c.service, c.instance)
		}
	}
}

func TestAddBinding_NewService(t *testing.T) {
	p, _ := newPayload("auth", "auth-01", time.Minute)

	if err := p.AddBinding("order", "order-05"); err != nil {
		t.Fatalf("AddBinding: %v", err)
	}
	id, ok := p.GetInstanceID("order")
	if !ok || id != "order-05" {
		t.Errorf("expected order-05, got %q", id)
	}
	if len(p.Bindings) != 2 {
		t.Errorf("expected 2 bindings, got %d", len(p.Bindings))
	}
}

func TestAddBinding_SameInstance_NoBoundAtChange(t *testing.T) {
	p, _ := newPayload("auth", "auth-01", time.Minute)
	before := p.Bindings["auth"].BoundAt

	time.Sleep(time.Millisecond * 10)
	_ = p.AddBinding("auth", "auth-01") // khong co gi thay doi

	after := p.Bindings["auth"].BoundAt
	if before != after {
		t.Error("BoundAt should NOT change when instanceID is the same")
	}
}

func TestAddBinding_DifferentInstance_UpdatesBoundAt(t *testing.T) {
	p, _ := newPayload("auth", "auth-01", time.Minute)
	before := p.Bindings["auth"].BoundAt

	time.Sleep(time.Millisecond * 10)
	_ = p.AddBinding("auth", "auth-02") // instance moi -> cap nhat

	after := p.Bindings["auth"].BoundAt
	if after == before {
		t.Error("BoundAt should be updated (different microsecond) when instanceID changes")
	}
	id, _ := p.GetInstanceID("auth")
	if id != "auth-02" {
		t.Errorf("expected auth-02, got %q", id)
	}
}

func TestAddBinding_EmptyArgs(t *testing.T) {
	p, _ := newPayload("auth", "auth-01", time.Minute)
	if err := p.AddBinding("", "auth-02"); err == nil {
		t.Error("expected error for empty serviceName")
	}
	if err := p.AddBinding("auth", ""); err == nil {
		t.Error("expected error for empty instanceID")
	}
}

func TestRemoveBinding(t *testing.T) {
	p, _ := newPayload("auth", "auth-01", time.Minute)
	_ = p.AddBinding("order", "order-05")

	p.RemoveBinding("auth")

	if p.HasBinding("auth") {
		t.Error("auth binding should be removed")
	}
	if !p.HasBinding("order") {
		t.Error("order binding should still exist")
	}
}

func TestRemoveBinding_NonExistent(t *testing.T) {
	p, _ := newPayload("auth", "auth-01", time.Minute)
	// Khong panic khi xoa key khong ton tai
	p.RemoveBinding("nonexistent")
}

func TestGetInstanceID_NotFound(t *testing.T) {
	p, _ := newPayload("auth", "auth-01", time.Minute)
	id, ok := p.GetInstanceID("order")
	if ok || id != "" {
		t.Errorf("expected not found, got id=%q ok=%v", id, ok)
	}
}

func TestPayloadValid_Expired(t *testing.T) {
	p, _ := newPayload("auth", "auth-01", -time.Second) // da het han
	if err := p.valid(); err != ErrPayloadExpired {
		t.Errorf("expected ErrPayloadExpired, got %v", err)
	}
}

func TestPayloadValid_NotExpired(t *testing.T) {
	p, _ := newPayload("auth", "auth-01", time.Minute)
	if err := p.valid(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestEncodeDecodePayload_RoundTrip(t *testing.T) {
	p, _ := newPayload("auth", "auth-01", time.Minute)
	_ = p.AddBinding("order", "order-05")

	data, err := p.encodePayload()
	if err != nil {
		t.Fatalf("encodePayload: %v", err)
	}

	p2, err := decodePayload(data)
	if err != nil {
		t.Fatalf("decodePayload: %v", err)
	}

	id, ok := p2.GetInstanceID("auth")
	if !ok || id != "auth-01" {
		t.Errorf("auth binding mismatch: %q", id)
	}
	id, ok = p2.GetInstanceID("order")
	if !ok || id != "order-05" {
		t.Errorf("order binding mismatch: %q", id)
	}
}

func TestDecodePayload_InvalidJSON(t *testing.T) {
	_, err := decodePayload([]byte("not-json"))
	if err != ErrPayloadInvalid {
		t.Errorf("expected ErrPayloadInvalid, got %v", err)
	}
}

func TestDecodePayload_EmptyBindings(t *testing.T) {
	data := []byte(`{"bindings":{},"expires_at":9999999999}`)
	_, err := decodePayload(data)
	if err != ErrPayloadInvalid {
		t.Errorf("expected ErrPayloadInvalid for empty bindings, got %v", err)
	}
}

// ============================================================
// crypto_test
// ============================================================

func TestNewSessionCrypto_ValidKeySizes(t *testing.T) {
	for _, key := range [][]byte{testKey16, testKey24, testKey32} {
		_, err := newSessionCrypto(key)
		if err != nil {
			t.Errorf("key len %d: unexpected error: %v", len(key), err)
		}
	}
}

func TestNewSessionCrypto_InvalidKeySize(t *testing.T) {
	for _, size := range []int{0, 8, 15, 17, 31, 33, 64} {
		_, err := newSessionCrypto(make([]byte, size))
		if err != ErrInvalidKeySize {
			t.Errorf("key len %d: expected ErrInvalidKeySize, got %v", size, err)
		}
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	c, _ := newSessionCrypto(testKey32)
	plaintext := []byte(`{"bindings":{"auth":{"instance_id":"auth-01","bound_at":1720000000}},"expires_at":9999999999}`)

	token, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	got, err := c.Decrypt(token)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Errorf("plaintext mismatch\nwant: %s\ngot:  %s", plaintext, got)
	}
}

func TestEncrypt_NonDeterministic(t *testing.T) {
	// Cung mot plaintext phai cho ra ciphertext khac nhau moi lan (do nonce ngau nhien)
	c, _ := newSessionCrypto(testKey32)
	pt := []byte("hello sticky")

	t1, _ := c.Encrypt(pt)
	t2, _ := c.Encrypt(pt)
	if t1 == t2 {
		t.Error("Encrypt should produce different ciphertext each call due to random nonce")
	}
}

func TestDecrypt_TamperedToken(t *testing.T) {
	c, _ := newSessionCrypto(testKey32)
	token, _ := c.Encrypt([]byte("hello"))

	// Lam gia mao: thay doi ky tu cuoi
	tampered := token[:len(token)-4] + "XXXX"
	_, err := c.Decrypt(tampered)
	if err != ErrDecryptFailed {
		t.Errorf("expected ErrDecryptFailed, got %v", err)
	}
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	c, _ := newSessionCrypto(testKey32)
	_, err := c.Decrypt("!!!not-base64!!!")
	if err != ErrDecryptFailed {
		t.Errorf("expected ErrDecryptFailed, got %v", err)
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	c, _ := newSessionCrypto(testKey32)
	// Chi co 4 byte, ngan hon nonce+overhead (28 byte)
	import64 := "AAAA"
	_, err := c.Decrypt(import64)
	if err == nil {
		t.Error("expected error for short ciphertext")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	c1, _ := newSessionCrypto(testKey32)
	c2, _ := newSessionCrypto([]byte("99999999999999999999999999999999"))

	token, _ := c1.Encrypt([]byte("secret"))
	_, err := c2.Decrypt(token)
	if err != ErrDecryptFailed {
		t.Errorf("expected ErrDecryptFailed with wrong key, got %v", err)
	}
}

// ============================================================
// manager_test
// ============================================================

func TestNewStickyManager_Success(t *testing.T) {
	m := newTestManager(t)
	if m == nil {
		t.Fatal("manager is nil")
	}
}

func TestNewStickyManager_InvalidKey(t *testing.T) {
	cfg := &config.StickySessionConfig{
		CookieName:    "test_sticky",
		TTL:           time.Minute,
		EncryptionKey: []byte("short"),
	}
	_, err := NewStickyManager(cfg, nil)
	if err == nil {
		t.Error("expected error for invalid key size")
	}
}

func TestBindService_NewClient(t *testing.T) {
	m := newTestManager(t)
	p, err := m.BindService(nil, "auth", "auth-01")
	if err != nil {
		t.Fatalf("BindService: %v", err)
	}
	id, ok := p.GetInstanceID("auth")
	if !ok || id != "auth-01" {
		t.Errorf("expected auth-01, got %q", id)
	}
}

func TestBindService_ExistingClient_AddService(t *testing.T) {
	m := newTestManager(t)
	p, _ := m.BindService(nil, "auth", "auth-01")
	p, err := m.BindService(p, "order", "order-05")
	if err != nil {
		t.Fatalf("BindService second: %v", err)
	}
	if len(p.Bindings) != 2 {
		t.Errorf("expected 2 bindings, got %d", len(p.Bindings))
	}
}

func TestBindService_EmptyArgs(t *testing.T) {
	m := newTestManager(t)
	_, err := m.BindService(nil, "", "auth-01")
	if err == nil {
		t.Error("expected error for empty serviceName")
	}
}

func TestRemoveBinding_Manager(t *testing.T) {
	m := newTestManager(t)
	p, _ := m.BindService(nil, "auth", "auth-01")
	_, _ = m.BindService(p, "order", "order-05")

	p = m.RemoveBinding(p, "auth")
	if p.HasBinding("auth") {
		t.Error("auth should be removed")
	}
	if !p.HasBinding("order") {
		t.Error("order should remain")
	}
}

func TestRemoveBinding_NilPayload(t *testing.T) {
	m := newTestManager(t)
	result := m.RemoveBinding(nil, "auth")
	if result != nil {
		t.Error("expected nil when payload is nil")
	}
}

func TestFlushSession_SetsCookie(t *testing.T) {
	m := newTestManager(t)
	p, _ := m.BindService(nil, "auth", "auth-01")

	w := httptest.NewRecorder()
	if err := m.FlushSession(w, p); err != nil {
		t.Fatalf("FlushSession: %v", err)
	}

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookie set")
	}
	if cookies[0].Name != "test_sticky" {
		t.Errorf("expected cookie name test_sticky, got %q", cookies[0].Name)
	}
	if cookies[0].Value == "" {
		t.Error("cookie value should not be empty")
	}
}

func TestFlushSession_NilPayload(t *testing.T) {
	m := newTestManager(t)
	w := httptest.NewRecorder()
	if err := m.FlushSession(w, nil); err == nil {
		t.Error("expected error for nil payload")
	}
}

func TestMiddleware_NoCookie_PassThrough(t *testing.T) {
	m := newTestManager(t)
	called := false
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, ok := m.GetSessionFromContext(r)
		if ok {
			t.Error("should not have session in context when no cookie")
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Error("next handler was not called")
	}
}

func TestMiddleware_ValidCookie_InjectsContext(t *testing.T) {
	m := newTestManager(t)

	// Tao cookie hop le
	p, _ := m.BindService(nil, "auth", "auth-01")
	_, _ = m.BindService(p, "order", "order-05")
	w := httptest.NewRecorder()
	_ = m.FlushSession(w, p)
	cookieHeader := w.Result().Header.Get("Set-Cookie")

	// Gui request voi cookie do
	called := false
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		payload, ok := m.GetSessionFromContext(r)
		if !ok || payload == nil {
			t.Error("expected session in context")
			return
		}
		id, ok := payload.GetInstanceID("auth")
		if !ok || id != "auth-01" {
			t.Errorf("expected auth-01, got %q", id)
		}
		id, ok = payload.GetInstanceID("order")
		if !ok || id != "order-05" {
			t.Errorf("expected order-05, got %q", id)
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", cookieHeader)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Error("next handler was not called")
	}
}

func TestMiddleware_TamperedCookie_ClearsAndPassThrough(t *testing.T) {
	m := newTestManager(t)

	called := false
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, ok := m.GetSessionFromContext(r)
		if ok {
			t.Error("should not have session in context for tampered cookie")
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "test_sticky", Value: "tampered.invalid.token"})
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	if !called {
		t.Error("next handler was not called")
	}
	// Cookie phai bi xoa (MaxAge=-1)
	cookies := rw.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "test_sticky" && c.MaxAge == -1 {
			return // pass
		}
	}
	t.Error("expected cookie to be cleared with MaxAge=-1")
}

func TestMiddleware_ExpiredCookie_ClearsAndPassThrough(t *testing.T) {
	// Tao manager voi TTL rat ngan
	cfg := &config.StickySessionConfig{
		CookieName:    "test_sticky",
		TTL:           time.Millisecond,
		EncryptionKey: testKey32,
	}
	m, _ := NewStickyManager(cfg, testLogger())

	p, _ := m.BindService(nil, "auth", "auth-01")
	w := httptest.NewRecorder()
	_ = m.FlushSession(w, p)
	cookieHeader := w.Result().Header.Get("Set-Cookie")

	time.Sleep(5 * time.Millisecond) // cho het han

	called := false
	handler := m.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		_, ok := m.GetSessionFromContext(r)
		if ok {
			t.Error("should not have session in context for expired cookie")
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", cookieHeader)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Error("next handler was not called")
	}
}

// ============================================================
// Integration: full round-trip multi-hop
// ============================================================

func TestIntegration_MultiHopRouting(t *testing.T) {
	m := newTestManager(t)

	// --- Request 1: client moi, bind auth ---
	var payload *SessionPayload
	payload, _ = m.BindService(payload, "auth", "auth-01")

	w1 := httptest.NewRecorder()
	_ = m.FlushSession(w1, payload)
	cookie1 := w1.Result().Header.Get("Set-Cookie")

	// --- Request 2: client cu, them order binding ---
	req2 := httptest.NewRequest(http.MethodGet, "/order", nil)
	req2.Header.Set("Cookie", cookie1)

	var capturedPayload *SessionPayload
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := m.GetSessionFromContext(r)
		p, _ = m.BindService(p, "order", "order-05")
		_ = m.FlushSession(w, p)
		capturedPayload = p
	}))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	// Sau request 2 phai co ca 2 binding
	if !capturedPayload.HasBinding("auth") {
		t.Error("auth binding should persist after second request")
	}
	if !capturedPayload.HasBinding("order") {
		t.Error("order binding should be added")
	}

	// --- Request 3: instance order chet, xoa binding order ---
	cookie2 := w2.Result().Header.Get("Set-Cookie")
	req3 := httptest.NewRequest(http.MethodGet, "/order", nil)
	req3.Header.Set("Cookie", cookie2)

	handler2 := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := m.GetSessionFromContext(r)
		p = m.RemoveBinding(p, "order")
		_ = m.FlushSession(w, p)
		capturedPayload = p
	}))
	w3 := httptest.NewRecorder()
	handler2.ServeHTTP(w3, req3)

	if capturedPayload.HasBinding("order") {
		t.Error("order binding should be removed")
	}
	if !capturedPayload.HasBinding("auth") {
		t.Error("auth binding should still exist after removing order")
	}
}

func TestIntegration_NoCollision_TwoServices(t *testing.T) {
	m := newTestManager(t)

	// Bind 3 service cung luc
	p, _ := m.BindService(nil, "auth", "auth-01")
	p, _ = m.BindService(p, "order", "order-05")
	p, _ = m.BindService(p, "promo", "promo-02")

	w := httptest.NewRecorder()
	_ = m.FlushSession(w, p)
	cookieHeader := w.Result().Header.Get("Set-Cookie")

	// Doc lai cookie
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", cookieHeader)

	var result *SessionPayload
	handler := m.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		result, _ = m.GetSessionFromContext(r)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	expected := map[string]string{
		"auth":  "auth-01",
		"order": "order-05",
		"promo": "promo-02",
	}
	for svc, want := range expected {
		got, ok := result.GetInstanceID(svc)
		if !ok || got != want {
			t.Errorf("service %q: expected %q, got %q (ok=%v)", svc, want, got, ok)
		}
	}
}
