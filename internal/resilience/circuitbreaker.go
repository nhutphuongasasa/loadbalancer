package resilience

import (
	"time"

	"log/slog"

	"github.com/sony/gobreaker"
)

type CircuitBreaker interface {
	Execute(fn func() (interface{}, error)) (interface{}, error)
	State() gobreaker.State
}

type sonyGoBreaker struct {
	cb     *gobreaker.CircuitBreaker
	logger *slog.Logger
}

func NewSonyGoBreaker(
	name string,
	maxConsecutiveFailures uint32,
	timeout time.Duration,
	interval time.Duration,
	logger *slog.Logger,
) *sonyGoBreaker {
	if logger == nil {
		logger = slog.Default()
	}

	settings := gobreaker.Settings{
		Name:     name,
		Timeout:  timeout,
		Interval: interval,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= maxConsecutiveFailures
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Info("Circuit breaker state changed",
				slog.String("name", name),
				slog.String("from", from.String()),
				slog.String("to", to.String()),
			)
		},
	}

	return &sonyGoBreaker{
		cb:     gobreaker.NewCircuitBreaker(settings),
		logger: logger,
	}
}

func (b *sonyGoBreaker) Execute(fn func() (interface{}, error)) (interface{}, error) {
	return b.cb.Execute(fn)
}

func (b *sonyGoBreaker) State() gobreaker.State {
	return b.cb.State()
}
