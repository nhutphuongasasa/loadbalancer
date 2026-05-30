package circuitBreaker

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
	"github.com/sony/gobreaker"
)

// ============================================================
// Helpers
// ============================================================

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestBreaker(t *testing.T, cfg *config.CircuitBreakerConfig) *sonyGoBreaker {
	t.Helper()
	if cfg == nil {
		cfg = &config.CircuitBreakerConfig{
			MaxConsecutiveFailures: 3,
			Timeout:                100 * time.Millisecond,
			Interval:               10 * time.Second,
		}
	}
	b, err := NewSonyGoBreaker("test-breaker", cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewSonyGoBreaker: %v", err)
	}
	return b
}

var errBackend = errors.New("backend error")

func failFn() (interface{}, error) { return nil, errBackend }
func okFn() (interface{}, error)   { return "ok", nil }

// triggerOpen đẩy circuit về trạng thái Open
func triggerOpen(t *testing.T, b *sonyGoBreaker, times int) {
	t.Helper()
	for i := 0; i < times; i++ {
		b.Execute(failFn)
	}
}

// ============================================================
// NewSonyGoBreaker — validation
// ============================================================

func TestNewSonyGoBreaker_Valid(t *testing.T) {
	b, err := NewSonyGoBreaker("cb-svc-i001", defaultTestConfig, discardLogger())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil breaker")
	}
}

func TestNewSonyGoBreaker_EmptyName_Error(t *testing.T) {
	_, err := NewSonyGoBreaker("", defaultTestConfig, discardLogger())
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestNewSonyGoBreaker_ZeroMaxFailures_Error(t *testing.T) {
	cfg := &config.CircuitBreakerConfig{
		MaxConsecutiveFailures: 0,
		Timeout:                1 * time.Second,
	}
	_, err := NewSonyGoBreaker("cb", cfg, discardLogger())
	if err == nil {
		t.Error("expected error for MaxConsecutiveFailures=0")
	}
}

func TestNewSonyGoBreaker_ZeroTimeout_Error(t *testing.T) {
	cfg := &config.CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		Timeout:                0,
	}
	_, err := NewSonyGoBreaker("cb", cfg, discardLogger())
	if err == nil {
		t.Error("expected error for Timeout=0")
	}
}

func TestNewSonyGoBreaker_NilConfig_UsesDefault(t *testing.T) {
	b, err := NewSonyGoBreaker("cb", nil, discardLogger())
	if err != nil {
		t.Fatalf("nil config should use default, got error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil breaker")
	}
}

func TestNewSonyGoBreaker_NilLogger_UsesDefault(t *testing.T) {
	b, err := NewSonyGoBreaker("cb", defaultTestConfig, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.logger == nil {
		t.Error("logger should not be nil")
	}
}

// ============================================================
// MustNewSonyGoBreaker
// ============================================================

func TestMustNewSonyGoBreaker_Valid(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic: %v", r)
		}
	}()
	MustNewSonyGoBreaker("cb", defaultTestConfig, discardLogger())
}

func TestMustNewSonyGoBreaker_InvalidConfig_Panics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic for invalid config")
		}
	}()
	MustNewSonyGoBreaker("", defaultTestConfig, discardLogger())
}

// ============================================================
// State helpers
// ============================================================

func TestIsClosed_Initially(t *testing.T) {
	b := newTestBreaker(t, nil)
	if !b.IsClosed() {
		t.Error("new breaker should be closed")
	}
	if b.IsOpen() {
		t.Error("new breaker should not be open")
	}
	if b.IsHalfOpen() {
		t.Error("new breaker should not be half-open")
	}
}

func TestIsOpen_AfterFailures(t *testing.T) {
	b := newTestBreaker(t, &config.CircuitBreakerConfig{
		MaxConsecutiveFailures: 2,
		Timeout:                100 * time.Millisecond,
		Interval:               10 * time.Second,
	})

	triggerOpen(t, b, 2)

	if !b.IsOpen() {
		t.Error("expected circuit to be open after consecutive failures")
	}
	if b.IsClosed() {
		t.Error("circuit should not be closed when open")
	}
}

// ============================================================
// Execute
// ============================================================

func TestExecute_Success(t *testing.T) {
	b := newTestBreaker(t, nil)
	result, err := b.Execute(okFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("got %v, want ok", result)
	}
}

func TestExecute_PropagatesError(t *testing.T) {
	b := newTestBreaker(t, nil)
	_, err := b.Execute(failFn)
	if !errors.Is(err, errBackend) {
		t.Errorf("expected errBackend, got %v", err)
	}
}

func TestExecute_OpenCircuit_ReturnsCircuitError(t *testing.T) {
	b := newTestBreaker(t, &config.CircuitBreakerConfig{
		MaxConsecutiveFailures: 2,
		Timeout:                100 * time.Millisecond,
		Interval:               10 * time.Second,
	})

	triggerOpen(t, b, 2)

	_, err := b.Execute(okFn) // circuit open → không cho qua
	if err == nil {
		t.Fatal("expected error when circuit is open")
	}
	if !IsCircuitOpenError(err) {
		t.Errorf("expected circuit open error, got %v", err)
	}
}

func TestExecute_HalfOpen_AfterTimeout(t *testing.T) {
	b := newTestBreaker(t, &config.CircuitBreakerConfig{
		MaxConsecutiveFailures: 2,
		Timeout:                50 * time.Millisecond, // timeout ngắn để test nhanh
		Interval:               10 * time.Second,
	})

	triggerOpen(t, b, 2)
	if !b.IsOpen() {
		t.Fatal("expected open state")
	}

	// đợi timeout → chuyển sang HalfOpen
	time.Sleep(80 * time.Millisecond)

	// 1 request được phép qua trong HalfOpen
	_, err := b.Execute(okFn)
	if err != nil {
		t.Errorf("half-open should allow 1 request through, got: %v", err)
	}

	// sau success → circuit đóng lại
	if !b.IsClosed() {
		t.Error("circuit should close after success in half-open state")
	}
}

func TestExecute_HalfOpen_FailReopens(t *testing.T) {
	b := newTestBreaker(t, &config.CircuitBreakerConfig{
		MaxConsecutiveFailures: 2,
		Timeout:                50 * time.Millisecond,
		Interval:               10 * time.Second,
	})

	triggerOpen(t, b, 2)
	time.Sleep(80 * time.Millisecond) // → HalfOpen

	b.Execute(failFn) // fail trong HalfOpen → mở lại

	if !b.IsOpen() {
		t.Error("expected circuit to reopen after failure in half-open")
	}
}

// ============================================================
// IsCircuitOpenError
// ============================================================

func TestIsCircuitOpenError_OpenState(t *testing.T) {
	if !IsCircuitOpenError(gobreaker.ErrOpenState) {
		t.Error("ErrOpenState should be circuit open error")
	}
}

func TestIsCircuitOpenError_TooManyRequests(t *testing.T) {
	if !IsCircuitOpenError(gobreaker.ErrTooManyRequests) {
		t.Error("ErrTooManyRequests should be circuit open error")
	}
}

func TestIsCircuitOpenError_OtherError(t *testing.T) {
	if IsCircuitOpenError(errors.New("some other error")) {
		t.Error("regular error should not be circuit open error")
	}
}

func TestIsCircuitOpenError_Nil(t *testing.T) {
	if IsCircuitOpenError(nil) {
		t.Error("nil should not be circuit open error")
	}
}

// ============================================================
// State transitions — full cycle
// ============================================================

func TestCircuitBreaker_FullCycle(t *testing.T) {
	b := newTestBreaker(t, &config.CircuitBreakerConfig{
		MaxConsecutiveFailures: 3,
		Timeout:                50 * time.Millisecond,
		Interval:               10 * time.Second,
	})

	// 1. Closed → failures
	if !b.IsClosed() {
		t.Fatal("should start closed")
	}

	triggerOpen(t, b, 3)

	// 2. Open
	if !b.IsOpen() {
		t.Fatal("should be open after 3 failures")
	}

	// 3. Open → HalfOpen sau timeout
	time.Sleep(80 * time.Millisecond)

	// 4. HalfOpen → Closed sau success
	_, err := b.Execute(okFn)
	if err != nil {
		t.Fatalf("half-open request failed: %v", err)
	}
	if !b.IsClosed() {
		t.Error("should be closed after success in half-open")
	}
}

// ============================================================
// Config từ ConfigManager snapshot
// ============================================================

func TestNewSonyGoBreaker_FromConfigSnapshot(t *testing.T) {
	// simulate app.go lấy config từ ConfigManager
	cm := &config.ConfigManager{}
	cm.StoreSnapshot(&config.ConfigSnapshot{
		CircuitBreaker: &config.CircuitBreakerConfig{
			MaxConsecutiveFailures: 5,
			Timeout:                200 * time.Millisecond,
			Interval:               30 * time.Second,
		},
	})

	cbCfg := cm.GetCircuitBreakerConfig()
	b, err := NewSonyGoBreaker("cb-api-i001", cbCfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// trigger 5 failures → open
	triggerOpen(t, b, 5)
	if !b.IsOpen() {
		t.Error("expected open after 5 failures per config")
	}

	// 4 failures không đủ mở
	b2, _ := NewSonyGoBreaker("cb-api-i002", cbCfg, discardLogger())
	triggerOpen(t, b2, 4)
	if b2.IsOpen() {
		t.Error("4 failures should not open circuit (threshold=5)")
	}
}
