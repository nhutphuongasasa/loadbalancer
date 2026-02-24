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

// Cấu trúc chính để gom middleware và helpers
type Tracer struct {
	logger *slog.Logger
}

// Context keys
type ctxKey string

const (
	requestIDKey ctxKey = "requestID"
	traceCtxKey  ctxKey = "traceContext"
)

// Common headers
const (
	HeaderRequestID   = "X-Request-ID"
	HeaderTraceParent = "traceparent"
	HeaderTraceState  = "tracestate"
	HeaderAmazonTrace = "X-Amzn-Trace-Id"
)

// TraceContext lưu thông tin trace
type TraceContext struct {
	TraceID    string // 32 hex chars
	SpanID     string // 16 hex chars
	Flags      byte   // sampled bit: 0x01 = sampled
	TraceState string
}

// New tạo Tracer instance với logger được inject
func NewTracer(logger *slog.Logger) *Tracer {
	if logger == nil {
		logger = slog.Default() // fallback nếu nil
	}
	return &Tracer{
		logger: logger,
	}
}

// randHex tạo chuỗi hex ngẫu nhiên
func (t *Tracer) randHex(n int) string {
	b := make([]byte, n/2)
	_, _ = rand.Read(b) // ignore error vì crypto/rand hiếm fail
	return hex.EncodeToString(b)
}

// RequestIDMiddleware tạo / extract request ID
func (t *Tracer) RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(HeaderRequestID)
		if reqID == "" {
			reqID = r.Header.Get(HeaderAmazonTrace)
		}
		if reqID == "" {
			reqID = t.randHex(32)
		}

		ctx := context.WithValue(r.Context(), requestIDKey, reqID)
		w.Header().Set(HeaderRequestID, reqID)

		// Logger với request info
		reqLogger := t.logger.With(
			slog.String("request_id", reqID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote_addr", r.RemoteAddr),
		)

		reqLogger.Info("request started")

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TraceContextMiddleware xử lý W3C trace context
func (t *Tracer) TraceContextMiddleware(next http.Handler) http.Handler {
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
			tc.TraceID = t.randHex(32)
			tc.SpanID = t.randHex(16)
			tc.Flags = 0x01 // sampled
		} else {
			tc.SpanID = t.randHex(16) // new span cho hop này
		}

		tc.TraceState = r.Header.Get(HeaderTraceState)

		ctx := context.WithValue(r.Context(), traceCtxKey, tc)

		// Logger enrich trace info
		reqLogger := t.logger.With(
			slog.String("trace_id", tc.TraceID),
			slog.String("span_id", tc.SpanID),
			slog.Bool("sampled", tc.Flags&0x01 == 0x01),
		)

		reqLogger.Info("trace context extracted/created")

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// PropagateTraceHeaders inject headers khi forward request xuống backend
func (t *Tracer) PropagateTraceHeaders(ctx context.Context, req *http.Request) {
	tc, ok := ctx.Value(traceCtxKey).(TraceContext)
	if !ok || tc.TraceID == "" {
		return
	}

	traceparent := fmt.Sprintf("00-%s-%s-%02x", tc.TraceID, tc.SpanID, tc.Flags)
	req.Header.Set(HeaderTraceParent, traceparent)

	if tc.TraceState != "" {
		req.Header.Set(HeaderTraceState, tc.TraceState)
	}

	if reqID, ok := ctx.Value(requestIDKey).(string); ok && reqID != "" {
		req.Header.Set(HeaderRequestID, reqID)
	}
}

// Helpers để lấy giá trị từ context (không cần tracer instance)

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
