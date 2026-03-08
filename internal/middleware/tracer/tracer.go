package tracer

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
	TraceID      string
	SpanID       string
	ParentSpanID string
	Flags        byte
	TraceState   string
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
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (t *Tracer) CombinedTracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tc := t.extractTraceContext(r)

		// Mỗi hop luôn tạo SpanID mới — lưu upstream SpanID vào ParentSpanID
		tc.ParentSpanID = tc.SpanID
		tc.SpanID = t.randomHex(8) // 8 bytes = 16 hex chars

		// FIX: inject TraceContext và RequestID vào context để downstream dùng được
		ctx := context.WithValue(r.Context(), traceCtxKey, tc)
		ctx = context.WithValue(ctx, requestIDKey, tc.TraceID)
		r = r.WithContext(ctx)

		// Propagate headers sang backend
		t.PropagateTraceHeaders(tc, r)

		t.logger.Info("tracing injected",
			slog.String("trace_id", tc.TraceID),
			slog.String("span_id", tc.SpanID),
			slog.String("parent_span_id", tc.ParentSpanID),
			slog.String("path", r.URL.Path),
			slog.Bool("sampled", tc.Flags&0x01 == 0x01),
		)

		// Set response header để client biết trace ID
		w.Header().Set(HeaderRequestID, tc.TraceID)

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
	if v, ok := ctx.Value(requestIDKey).(string); ok && v != "" {
		return v
	}

	return "unknown"
}

func TraceContextFromContext(ctx context.Context) (TraceContext, bool) {
	v, ok := ctx.Value(traceCtxKey).(TraceContext)
	return v, ok
}

func (t *Tracer) extractTraceContext(r *http.Request) TraceContext {
	var tc TraceContext
	tc.TraceState = r.Header.Get(HeaderTraceState)

	// 1. W3C traceparent: "00-<traceID>-<spanID>-<flags>"
	if tp := r.Header.Get(HeaderTraceParent); tp != "" {
		if parsed, ok := parseTraceParent(tp); ok {
			return parsed
		}
		t.logger.Warn("invalid traceparent header, ignoring", slog.String("traceparent", tp))
	}

	// 2. X-Request-ID
	if rid := strings.TrimSpace(r.Header.Get(HeaderRequestID)); rid != "" {
		tc.TraceID = rid
		tc.Flags = 0x01
		return tc
	}

	// 3. X-Amzn-Trace-Id: "Root=1-xxxxxxxx-yyyyyyyyyyyyyyyyyyyyyyyy"
	// FIX: không dùng thẳng làm TraceID vì format không phải hex thuần
	if amzn := r.Header.Get(HeaderAmazonTrace); amzn != "" {
		if extracted := extractAmznTraceID(amzn); extracted != "" {
			tc.TraceID = extracted
			tc.Flags = 0x01
			return tc
		}
	}

	// 4. Tự sinh trace mới
	tc.TraceID = t.randomHex(16) // 16 bytes = 32 hex chars
	tc.Flags = 0x01
	return tc
}

func parseTraceParent(tp string) (TraceContext, bool) {
	parts := strings.Split(tp, "-")
	if len(parts) != 4 {
		return TraceContext{}, false
	}

	version, traceID, spanID, flagsStr := parts[0], parts[1], parts[2], parts[3]

	// validate
	if version != "00" {
		return TraceContext{}, false
	}
	if len(traceID) != 32 || !isHex(traceID) {
		return TraceContext{}, false
	}
	if len(spanID) != 16 || !isHex(spanID) {
		return TraceContext{}, false
	}
	if len(flagsStr) != 2 || !isHex(flagsStr) {
		return TraceContext{}, false
	}

	var flags byte
	fmt.Sscanf(flagsStr, "%02x", &flags)

	return TraceContext{
		TraceID: traceID,
		SpanID:  spanID,
		Flags:   flags,
	}, true
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

func extractAmznTraceID(header string) string {
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "Root=") {
			root := strings.TrimPrefix(part, "Root=")
			// format: "1-xxxxxxxx-yyyyyyyyyyyyyyyyyyyyyyyy"
			segments := strings.SplitN(root, "-", 3)
			if len(segments) == 3 {
				combined := segments[1] + segments[2]
				combined = strings.ReplaceAll(combined, "-", "")
				if len(combined) >= 32 && isHex(combined[:32]) {
					return combined[:32]
				}
			}
		}
	}
	return ""
}
