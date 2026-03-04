// package resilience

// import (
// 	"bytes"
// 	"context"
// 	"errors"
// 	"strings"
// 	"sync"
// 	"testing"
// 	"time"

// 	"log/slog"

// 	"github.com/stretchr/testify/assert"
// )

// type mockLogger struct {
// 	warnCount int
// 	lastMsgs  []string // lưu nhiều log để dễ check
// }

// func (m *mockLogger) Warn(msg string, args ...any) {
// 	m.warnCount++
// 	m.lastMsgs = append(m.lastMsgs, msg)
// }

// func (m *mockLogger) Enabled(context.Context, slog.Level) bool { return true }
// func (m *mockLogger) With(...any) *slog.Logger                 { return slog.Default() }
// func (m *mockLogger) Handler() slog.Handler                    { return nil }

// func TestExponentialRetry_SuccessOnFirstAttempt(t *testing.T) {
// 	policy := NewExponentialRetry(5, 100*time.Millisecond, 2*time.Second, 0.2, nil)

// 	called := 0
// 	err := policy.Do(func() error {
// 		called++
// 		return nil
// 	})

// 	assert.NoError(t, err)
// 	assert.Equal(t, 1, called)
// }

// func TestExponentialRetry_FailAllAttempts_ReturnLastError(t *testing.T) {
// 	policy := NewExponentialRetry(2, 10*time.Millisecond, 100*time.Millisecond, 0, nil)

// 	called := 0
// 	wantErr := errors.New("permanent error")

// 	err := policy.Do(func() error {
// 		called++
// 		return wantErr
// 	})

// 	assert.Equal(t, wantErr, err)
// 	assert.Equal(t, 3, called)
// }

// func TestDoAny_ReturnsValue_WhenSuccess(t *testing.T) {
// 	policy := NewExponentialRetry(3, 50*time.Millisecond, time.Second, 0.1, nil)

// 	type myType struct{ Value int }

// 	got, err := policy.DoAny(func() (any, error) {
// 		return myType{Value: 42}, nil
// 	})

// 	assert.NoError(t, err)
// 	assert.Equal(t, myType{Value: 42}, got)
// }

// func TestDoWithResult_GenericType(t *testing.T) {
// 	policy := NewExponentialRetry(1, 10*time.Millisecond, 100*time.Millisecond, 0, nil)

// 	type user struct{ ID string }

// 	got, err := DoWithResult[user](policy, func() (user, error) {
// 		return user{ID: "u123"}, nil
// 	})
// 	assert.NoError(t, err)
// 	assert.Equal(t, "u123", got.ID)
// }

// func TestRetry_BackoffAndJitter(t *testing.T) {
// 	var buf bytes.Buffer
// 	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
// 	logger := slog.New(handler)

// 	policy := NewExponentialRetry(3, 100*time.Millisecond, 800*time.Millisecond, 0.5, logger)

// 	_ = policy.Do(func() error { return errors.New("fail") })

// 	logOutput := buf.String()

// 	assert.Contains(t, logOutput, "Retry attempt failed")
// 	assert.Equal(t, strings.Count(logOutput, "Retry attempt failed"), 3, "should have 3 warn logs")
// }

// func TestDefaultValues_WhenInvalidInput(t *testing.T) {
// 	policy := NewExponentialRetry(
// 		-1,
// 		-50*time.Millisecond,
// 		0,
// 		1.5,
// 		nil,
// 	)

// 	// Vì chung package → truy cập field private được
// 	assert.Equal(t, 3, policy.maxRetries)
// 	assert.Equal(t, 200*time.Millisecond, policy.baseDelay)
// 	assert.Equal(t, 5*time.Second, policy.maxDelay)
// 	assert.Equal(t, 0.1, policy.jitterFactor)

// 	// Kiểm tra hành vi bổ sung
// 	called := 0
// 	_ = policy.Do(func() error {
// 		called++
// 		return errors.New("fail")
// 	})
// 	assert.Equal(t, 4, called) // default 3 retries + 1 initial
// }

// func TestConcurrentSafe_Rng(t *testing.T) {
// 	policy := NewExponentialRetry(5, 10*time.Millisecond, 100*time.Millisecond, 0.3, nil)

// 	var wg sync.WaitGroup
// 	for i := 0; i < 50; i++ {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			_ = policy.Do(func() error { return errors.New("test") })
// 		}()
// 	}
// 	wg.Wait()
// }

// Package resilience chứa các test cho cơ chế retry (cụ thể là Exponential Backoff with Jitter).
// File này kiểm tra tính đúng đắn của ExponentialRetry policy – một pattern phổ biến để xử lý lỗi tạm thời.

package resilience

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/stretchr/testify/assert"
)

// mockLogger giả lập slog.Logger chỉ để đếm số lần Warn và lưu lại message.
// Dùng trong test để kiểm tra logging mà không cần output thật ra console.
type mockLogger struct {
	warnCount int
	lastMsgs  []string // lưu nhiều log để dễ assert sau
}

func (m *mockLogger) Warn(msg string, args ...any) {
	m.warnCount++
	m.lastMsgs = append(m.lastMsgs, msg)
}

func (m *mockLogger) Enabled(context.Context, slog.Level) bool { return true }
func (m *mockLogger) With(...any) *slog.Logger                 { return slog.Default() }
func (m *mockLogger) Handler() slog.Handler                    { return nil }

// TestExponentialRetry_SuccessOnFirstAttempt
// Kiểm tra trường hợp cơ bản nhất: thành công ngay lần gọi đầu → không retry
func TestExponentialRetry_SuccessOnFirstAttempt(t *testing.T) {
	// maxRetries=5, base=100ms, max=2s, jitter=20%, không logger
	policy := NewExponentialRetry(5, 100*time.Millisecond, 2*time.Second, 0.2, nil)

	called := 0
	err := policy.Do(func() error {
		called++
		return nil // thành công ngay
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, called) // chỉ gọi 1 lần
}

// TestExponentialRetry_FailAllAttempts_ReturnLastError
// Kiểm tra: thất bại hết số lần retry → trả về lỗi cuối cùng
// Lưu ý: tổng số lần gọi = maxRetries + 1 (lần đầu + số lần retry)
func TestExponentialRetry_FailAllAttempts_ReturnLastError(t *testing.T) {
	// maxRetries=2 → gọi tối đa 3 lần (1 + 2 retry)
	policy := NewExponentialRetry(2, 10*time.Millisecond, 100*time.Millisecond, 0, nil)

	called := 0
	wantErr := errors.New("permanent error")

	err := policy.Do(func() error {
		called++
		return wantErr // luôn lỗi
	})

	assert.Equal(t, wantErr, err) // trả lỗi cuối cùng
	assert.Equal(t, 3, called)    // 1 lần đầu + 2 retry
}

// TestDoAny_ReturnsValue_WhenSuccess
// Kiểm tra method DoAny (trả về any + error) – dùng khi cần giá trị trả về
func TestDoAny_ReturnsValue_WhenSuccess(t *testing.T) {
	policy := NewExponentialRetry(3, 50*time.Millisecond, time.Second, 0.1, nil)

	type myType struct{ Value int }

	got, err := policy.DoAny(func() (any, error) {
		return myType{Value: 42}, nil
	})

	assert.NoError(t, err)
	assert.Equal(t, myType{Value: 42}, got)
}

// TestDoWithResult_GenericType
// Kiểm tra generic helper DoWithResult[T] – cách dùng tiện lợi với type cụ thể
func TestDoWithResult_GenericType(t *testing.T) {
	policy := NewExponentialRetry(1, 10*time.Millisecond, 100*time.Millisecond, 0, nil)

	type user struct{ ID string }

	got, err := DoWithResult[user](policy, func() (user, error) {
		return user{ID: "u123"}, nil
	})

	assert.NoError(t, err)
	assert.Equal(t, "u123", got.ID)
}

// TestRetry_BackoffAndJitter
// Kiểm tra việc có log warn mỗi lần retry, và dùng logger thật để capture output
func TestRetry_BackoffAndJitter(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	logger := slog.New(handler)

	// maxRetries=3, base=100ms, max=800ms, jitter=50%
	policy := NewExponentialRetry(3, 100*time.Millisecond, 800*time.Millisecond, 0.5, logger)

	_ = policy.Do(func() error { return errors.New("fail") })

	logOutput := buf.String()

	assert.Contains(t, logOutput, "Retry attempt failed")
	// 3 lần retry → phải có 3 dòng warn (lần đầu không log retry)
	assert.Equal(t, 3, strings.Count(logOutput, "Retry attempt failed"))
}

// TestDefaultValues_WhenInvalidInput
// Kiểm tra cơ chế bảo vệ: khi truyền tham số không hợp lệ → dùng giá trị mặc định an toàn
func TestDefaultValues_WhenInvalidInput(t *testing.T) {
	policy := NewExponentialRetry(
		-1,                   // maxRetries âm → dùng default
		-50*time.Millisecond, // base delay âm → dùng default
		0,                    // max delay = 0 → dùng default
		1.5,                  // jitter > 1 → clamp về 0.1
		nil,
	)

	// Vì cùng package → có thể truy cập field private để assert
	assert.Equal(t, 3, policy.maxRetries)
	assert.Equal(t, 200*time.Millisecond, policy.baseDelay)
	assert.Equal(t, 5*time.Second, policy.maxDelay)
	assert.Equal(t, 0.1, policy.jitterFactor)

	// Kiểm tra hành vi: default maxRetries=3 → gọi 4 lần (1 + 3 retry)
	called := 0
	_ = policy.Do(func() error {
		called++
		return errors.New("fail")
	})
	assert.Equal(t, 4, called)
}

// TestConcurrentSafe_Rng
// Kiểm tra tính an toàn khi dùng concurrent (nhiều goroutine dùng chung policy)
// Đặc biệt là random cho jitter → phải thread-safe (thường dùng rand.New(rand.NewSource(...)) với mutex hoặc rand/randutil)
func TestConcurrentSafe_Rng(t *testing.T) {
	policy := NewExponentialRetry(5, 10*time.Millisecond, 100*time.Millisecond, 0.3, nil)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Mỗi goroutine gọi Do → trigger jitter random
			_ = policy.Do(func() error { return errors.New("test") })
		}()
	}
	wg.Wait()

	// Không panic → coi như pass (test chủ yếu để đảm bảo không race condition trên RNG)
	// Nếu RNG không an toàn → có thể panic hoặc kết quả không mong muốn khi chạy -race
}
