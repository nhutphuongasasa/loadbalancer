package circuitBreaker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nhutphuongasasa/loadbalancer/internal/config"
	"github.com/sony/gobreaker"
)

type CircuitBreaker interface {
	Execute(fn func() (interface{}, error)) (interface{}, error)
	State() gobreaker.State
	IsOpen() bool
	IsHalfOpen() bool
	IsClosed() bool
}

type sonyGoBreaker struct {
	cb     *gobreaker.CircuitBreaker
	logger *slog.Logger
}

func NewSonyGoBreaker(name string, cfg *config.CircuitBreakerConfig, logger *slog.Logger) (*sonyGoBreaker, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg == nil {
		cfg = config.DefaultCircuitBreakerConfig()
	}

	if name == "" {
		return nil, fmt.Errorf("circuit breaker name must not be empty")
	}
	if cfg.MaxConsecutiveFailures == 0 {
		return nil, fmt.Errorf("MaxConsecutiveFailures must be > 0")
	}
	if cfg.Timeout <= 0 {
		return nil, fmt.Errorf("Timeout must be > 0")
	}

	interval := cfg.Interval
	if interval < 0 {
		interval = 0
	}

	settings := gobreaker.Settings{
		Name:     name,
		Timeout:  cfg.Timeout,
		Interval: interval,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cfg.MaxConsecutiveFailures
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			level := slog.LevelInfo
			if to == gobreaker.StateOpen {
				level = slog.LevelWarn
			}
			logger.Log(context.TODO(), level, "Circuit breaker state changed",
				slog.String("name", name),
				slog.String("from", from.String()),
				slog.String("to", to.String()),
			)
		},
	}

	return &sonyGoBreaker{
		cb:     gobreaker.NewCircuitBreaker(settings),
		logger: logger,
	}, nil
}

func MustNewSonyGoBreaker(name string, cfg *config.CircuitBreakerConfig, logger *slog.Logger) *sonyGoBreaker {
	b, err := NewSonyGoBreaker(name, cfg, logger)
	if err != nil {
		panic(fmt.Sprintf("circuit breaker init failed: %v", err))
	}
	return b
}

func (b *sonyGoBreaker) Execute(fn func() (interface{}, error)) (interface{}, error) {
	return b.cb.Execute(fn)
}

func (b *sonyGoBreaker) State() gobreaker.State {
	return b.cb.State()
}

func (b *sonyGoBreaker) IsOpen() bool {
	return b.cb.State() == gobreaker.StateOpen
}

func (b *sonyGoBreaker) IsHalfOpen() bool {
	return b.cb.State() == gobreaker.StateHalfOpen
}

func (b *sonyGoBreaker) IsClosed() bool {
	return b.cb.State() == gobreaker.StateClosed
}

func IsCircuitOpenError(err error) bool {
	return err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests
}

// defaultTimeout dùng trong test
var defaultTestConfig = &config.CircuitBreakerConfig{
	MaxConsecutiveFailures: 3,
	Timeout:                1 * time.Second,
	Interval:               10 * time.Second,
}
