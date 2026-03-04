package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

type Tracer struct {
	logger *slog.Logger
}

type ctxKey string

const (
	requestIDKey ctxKey = "requestID"
	traceCtxKey  ctxKey = "traceContext"
)

const (
	HeaderRequestID   = "X-Request-ID"
	HeaderTraceParent = "traceparent"
	HeaderTraceState  = "tracestate"
	HeaderAmazonTrace = "X-Amzn-Trace-Id"
)

type TraceContext struct {
	TraceID    string
	SpanID     string
	Flags      byte
	TraceState string
}

func NewTracer(logger *slog.Logger) *Tracer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Tracer{
		logger: logger,
	}
}

/*
*Tao chuoi ngau nhien de lam trace ID
 */
func (t *Tracer) randomHex(n int) string {
	b := make([]byte, n/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (t *Tracer) CombinedTracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var tc TraceContext

		tp := r.Header.Get(HeaderTraceParent)
		if tp != "" {
			parts := strings.Split(tp, "-")
			if len(parts) == 4 && parts[0] == "00" {
				tc.TraceID = parts[1]
				tc.SpanID = parts[2]
				fmt.Sscanf(parts[3], "%x", &tc.Flags)
			}
		}

		if tc.TraceID == "" {
			tc.TraceID = r.Header.Get(HeaderRequestID)
			if tc.TraceID == "" {
				tc.TraceID = r.Header.Get(HeaderAmazonTrace)
			}
		}

		if tc.TraceID == "" {
			tc.TraceID = t.randomHex(32)
			tc.Flags = 0x01
		}

		tc.SpanID = t.randomHex(16)
		tc.TraceState = r.Header.Get(HeaderTraceState)

		t.PropagateTraceHeaders(tc, r)

		t.logger.Info("tracing injected",
			slog.String("trace_id", tc.TraceID),
			slog.String("path", r.URL.Path),
		)

		next.ServeHTTP(w, r)
	})
}

func (t *Tracer) PropagateTraceHeaders(tc TraceContext, req *http.Request) {
	traceparent := fmt.Sprintf("00-%s-%s-%02x", tc.TraceID, tc.SpanID, tc.Flags)
	req.Header.Set(HeaderTraceParent, traceparent)

	if tc.TraceState != "" {
		req.Header.Set(HeaderTraceState, tc.TraceState)
	}

	req.Header.Set(HeaderRequestID, tc.TraceID)
}

func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return "unknown"
}

func TraceContextFromContext(ctx context.Context) TraceContext {
	if v, ok := ctx.Value(traceCtxKey).(TraceContext); ok {
		return v
	}
	return TraceContext{}
}
