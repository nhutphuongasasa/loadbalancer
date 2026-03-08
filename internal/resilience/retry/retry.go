package retry

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"log/slog"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
)

type RetryPolicy interface {
	Do(fn func() error) error

	DoAny(fn func() (any, error)) (any, error)
}

type exponentialRetry struct {
	maxRetries   int
	baseDelay    time.Duration
	maxDelay     time.Duration
	jitterFactor float64
	logger       *slog.Logger
	rng          *rand.Rand
}

func NewExponentialRetry(
	retryCfg *config.RetryConfig,
	logger *slog.Logger,
) *exponentialRetry {

	if retryCfg.MaxRetries < 1 {
		retryCfg.MaxRetries = 3
	}
	if retryCfg.BaseDelay <= 0 {
		retryCfg.BaseDelay = 200 * time.Millisecond
	}
	if retryCfg.MaxDelay <= 0 {
		retryCfg.MaxDelay = 5 * time.Second
	}
	if retryCfg.JitterFactor < 0 || retryCfg.JitterFactor > 1 {
		retryCfg.JitterFactor = 0.1
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Sử dụng source riêng để tránh race condition khi dùng rand.Float64()
	src := rand.NewSource(time.Now().UnixNano())
	rng := rand.New(src)

	return &exponentialRetry{
		maxRetries:   retryCfg.MaxRetries,
		baseDelay:    retryCfg.BaseDelay,
		maxDelay:     retryCfg.MaxDelay,
		jitterFactor: retryCfg.JitterFactor,
		logger:       logger,
		rng:          rng,
	}
}

// Do thực hiện retry cho hàm chỉ trả về error
func (r *exponentialRetry) Do(fn func() error) error {
	_, err := r.DoAny(func() (any, error) {
		return nil, fn()
	})
	return err
}

// DoAny thực hiện retry và trả về kết quả dạng any
func (r *exponentialRetry) DoAny(fn func() (any, error)) (any, error) {
	var lastErr error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}

		lastErr = err

		if attempt == r.maxRetries {
			break
		}

		// Tính exponential backoff
		delay := r.baseDelay * time.Duration(1<<attempt) //  base * 2^attempt
		if delay > r.maxDelay {
			delay = r.maxDelay
		}

		// Thêm jitter (random trong khoảng [0, delay * jitterFactor])
		jitter := time.Duration(r.rng.Float64() * float64(delay) * r.jitterFactor)
		totalDelay := delay + jitter

		r.logger.Warn("Retry attempt failed, backing off",
			slog.Int("attempt", attempt+1),
			slog.Duration("delay", delay),
			slog.Duration("jitter", jitter),
			slog.Duration("total_delay", totalDelay),
			slog.String("error", err.Error()),
		)

		select {
		case <-time.After(totalDelay):
			// tiếp tục vòng lặp
		case <-context.Background().Done():
			// context bị hủy (hiếm xảy ra nếu không truyền context từ request)
			return nil, lastErr
		}
	}

	return nil, lastErr
}

func DoWithResult[T any](policy RetryPolicy, fn func() (T, error)) (T, error) {
	var zero T

	v, err := policy.DoAny(func() (any, error) {
		return fn()
	})
	if err != nil {
		return zero, err
	}

	if result, ok := v.(T); ok {
		return result, nil
	}

	// Type assertion thất bại → đây là lỗi lập trình
	return zero, errors.New("type assertion failed in DoWithResult: unexpected type")
}
