package middleware

import (
	"net/http"

	"github.com/nhutphuongasasa/loadbalancer/internal/middleware/logging"
	"github.com/nhutphuongasasa/loadbalancer/internal/middleware/rate_limit"
	"github.com/nhutphuongasasa/loadbalancer/internal/middleware/sticky"
)

type SecuritySuite struct {
	limiter  rate_limit.IRateLimiter
	logger   logging.ILogger
	stickier sticky.IStickier
	tracer   *Tracer
}

func NewSecuritySuit(
	limiter rate_limit.IRateLimiter,
	logger logging.ILogger,
	stickier sticky.IStickier,
	tracer *Tracer,
) *SecuritySuite {
	return &SecuritySuite{
		limiter:  limiter,
		logger:   logger,
		stickier: stickier,
		tracer:   tracer,
	}
}

func (s *SecuritySuite) Wrap(next http.Handler) http.Handler {
	handler := next

	if s.limiter != nil {
		handler = s.limiter.Middleware(handler)
	}

	if s.stickier != nil {
		handler = s.stickier.Middleware(handler)
	}

	if s.logger != nil {
		handler = s.logger.Middleware(handler)
	}

	if s.tracer != nil {
		handler = s.tracer.CombinedTracingMiddleware(handler)
	}

	return handler
}

func (s *SecuritySuite) Limiter() rate_limit.IRateLimiter {
	return s.limiter
}

func (s *SecuritySuite) Logger() logging.ILogger {
	return s.logger
}

func (s *SecuritySuite) Stickier() sticky.IStickier {
	return s.stickier
}

func (s *SecuritySuite) Tracer() *Tracer {
	return s.tracer
}
