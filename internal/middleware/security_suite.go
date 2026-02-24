package middleware

import (
	"net/http"
)

type SecuritySuite struct {
	limiter  IRateLimiter
	logger   ILogger
	stickier IStickier
	tracer   *Tracer
}

func NewSecuritySuit(
	limiter IRateLimiter,
	logger ILogger,
	stickier IStickier,
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
		handler = s.tracer.TraceContextMiddleware(handler)
		handler = s.tracer.RequestIDMiddleware(handler)
	}

	return handler
}

func (s *SecuritySuite) Limiter() IRateLimiter {
	return s.limiter
}

func (s *SecuritySuite) Logger() ILogger {
	return s.logger
}

func (s *SecuritySuite) Stickier() IStickier {
	return s.stickier
}

func (s *SecuritySuite) Tracer() *Tracer {
	return s.tracer
}
