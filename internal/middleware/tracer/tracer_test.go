package tracer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================
// Helper
// ============================================================

func newTestTracer() *Tracer {
	return NewTracer(nil)
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// ============================================================
// randomHex
// ============================================================

func TestRandomHex_Length(t *testing.T) {
	tr := newTestTracer()
	cases := []struct {
		nBytes      int
		expectedLen int
	}{
		{8, 16},
		{16, 32},
		{4, 8},
	}
	for _, c := range cases {
		got := tr.randomHex(c.nBytes)
		if len(got) != c.expectedLen {
			t.Errorf("randomHex(%d): got len %d, want %d", c.nBytes, len(got), c.expectedLen)
		}
	}
}

func TestRandomHex_IsHex(t *testing.T) {
	tr := newTestTracer()
	got := tr.randomHex(16)
	if !isHex(got) {
		t.Errorf("randomHex output is not valid hex: %q", got)
	}
}

func TestRandomHex_Unique(t *testing.T) {
	tr := newTestTracer()
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id := tr.randomHex(16)
		if _, exists := seen[id]; exists {
			t.Fatal("randomHex generated duplicate value")
		}
		seen[id] = struct{}{}
	}
}

// ============================================================
// isHex
// ============================================================

func TestIsHex(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"0123456789abcdef", true},
		{"ABCDEF", true},
		{"00ff", true},
		{"", false},
		{"xyz", false},
		{"0g", false},
		{"12 34", false},
	}
	for _, c := range cases {
		if got := isHex(c.input); got != c.want {
			t.Errorf("isHex(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

// ============================================================
// parseTraceParent
// ============================================================

func TestParseTraceParent_Valid(t *testing.T) {
	tp := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	tc, ok := parseTraceParent(tp)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tc.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("wrong TraceID: %q", tc.TraceID)
	}
	if tc.SpanID != "00f067aa0ba902b7" {
		t.Errorf("wrong SpanID: %q", tc.SpanID)
	}
	if tc.Flags != 0x01 {
		t.Errorf("wrong Flags: %02x", tc.Flags)
	}
}

func TestParseTraceParent_WrongVersion(t *testing.T) {
	tp := "01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	_, ok := parseTraceParent(tp)
	if ok {
		t.Error("expected ok=false for non-00 version")
	}
}

func TestParseTraceParent_TooFewParts(t *testing.T) {
	_, ok := parseTraceParent("00-traceid-spanid")
	if ok {
		t.Error("expected ok=false for malformed traceparent")
	}
}

func TestParseTraceParent_InvalidTraceIDLength(t *testing.T) {
	// TraceID quá ngắn
	tp := "00-4bf92f35-00f067aa0ba902b7-01"
	_, ok := parseTraceParent(tp)
	if ok {
		t.Error("expected ok=false for short traceID")
	}
}

func TestParseTraceParent_NonHexTraceID(t *testing.T) {
	tp := "00-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx-00f067aa0ba902b7-01"
	_, ok := parseTraceParent(tp)
	if ok {
		t.Error("expected ok=false for non-hex traceID")
	}
}

func TestParseTraceParent_SampledFlagZero(t *testing.T) {
	tp := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"
	tc, ok := parseTraceParent(tp)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if tc.Flags != 0x00 {
		t.Errorf("expected Flags=0, got %02x", tc.Flags)
	}
}

// ============================================================
// extractAmznTraceID
// ============================================================

func TestExtractAmznTraceID_Valid(t *testing.T) {
	header := "Root=1-5e1b1c77-000000000000000000000001"
	got := extractAmznTraceID(header)
	if got == "" {
		t.Error("expected non-empty result")
	}
	if len(got) != 32 {
		t.Errorf("expected 32 chars, got %d: %q", len(got), got)
	}
	if !isHex(got) {
		t.Errorf("expected hex output, got %q", got)
	}
}

func TestExtractAmznTraceID_WithSelfField(t *testing.T) {
	// format với nhiều segment
	header := "Root=1-5e1b1c77-000000000000000000000001;Self=1-abc"
	got := extractAmznTraceID(header)
	if got == "" {
		t.Error("expected non-empty result when Self field present")
	}
}

func TestExtractAmznTraceID_Invalid(t *testing.T) {
	got := extractAmznTraceID("not-a-valid-header")
	if got != "" {
		t.Errorf("expected empty for invalid header, got %q", got)
	}
}

func TestExtractAmznTraceID_Empty(t *testing.T) {
	got := extractAmznTraceID("")
	if got != "" {
		t.Errorf("expected empty for empty header, got %q", got)
	}
}

// ============================================================
// PropagateTraceHeaders
// ============================================================

func TestPropagateTraceHeaders_SetsTraceparent(t *testing.T) {
	tr := newTestTracer()
	tc := TraceContext{
		TraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:  "00f067aa0ba902b7",
		Flags:   0x01,
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	tr.PropagateTraceHeaders(tc, r)

	got := r.Header.Get(HeaderTraceParent)
	expected := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestPropagateTraceHeaders_SetsRequestID(t *testing.T) {
	tr := newTestTracer()
	tc := TraceContext{TraceID: "abc123", SpanID: "span456", Flags: 0x01}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	tr.PropagateTraceHeaders(tc, r)

	if got := r.Header.Get(HeaderRequestID); got != "abc123" {
		t.Errorf("got %q, want abc123", got)
	}
}

func TestPropagateTraceHeaders_SetsTraceState(t *testing.T) {
	tr := newTestTracer()
	tc := TraceContext{TraceID: "abc", SpanID: "def", Flags: 0x01, TraceState: "vendor=value"}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	tr.PropagateTraceHeaders(tc, r)

	if got := r.Header.Get(HeaderTraceState); got != "vendor=value" {
		t.Errorf("got %q, want vendor=value", got)
	}
}

func TestPropagateTraceHeaders_NoTraceState_NotSet(t *testing.T) {
	tr := newTestTracer()
	tc := TraceContext{TraceID: "abc", SpanID: "def", Flags: 0x01}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	tr.PropagateTraceHeaders(tc, r)

	if got := r.Header.Get(HeaderTraceState); got != "" {
		t.Errorf("expected empty tracestate, got %q", got)
	}
}

// ============================================================
// CombinedTracingMiddleware — context injection
// ============================================================

func TestMiddleware_InjectsTraceContextIntoContext(t *testing.T) {
	tr := newTestTracer()
	var gotTC TraceContext
	var gotOk bool

	handler := tr.CombinedTracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTC, gotOk = TraceContextFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if !gotOk {
		t.Fatal("expected TraceContext in context")
	}
	if gotTC.TraceID == "" {
		t.Error("expected non-empty TraceID in context")
	}
	if gotTC.SpanID == "" {
		t.Error("expected non-empty SpanID in context")
	}
}

func TestMiddleware_InjectsRequestIDIntoContext(t *testing.T) {
	tr := newTestTracer()
	var gotID string

	handler := tr.CombinedTracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if gotID == "" || gotID == "unknown" {
		t.Errorf("expected valid request ID in context, got %q", gotID)
	}
}

func TestMiddleware_SetsResponseHeader(t *testing.T) {
	tr := newTestTracer()
	handler := tr.CombinedTracingMiddleware(okHandler())

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if got := w.Header().Get(HeaderRequestID); got == "" {
		t.Error("expected X-Request-ID in response header")
	}
}

func TestMiddleware_PreservesExistingTraceparent(t *testing.T) {
	tr := newTestTracer()
	incomingTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	incomingTP := "00-" + incomingTraceID + "-00f067aa0ba902b7-01"

	var gotTC TraceContext
	handler := tr.CombinedTracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTC, _ = TraceContextFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(HeaderTraceParent, incomingTP)
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if gotTC.TraceID != incomingTraceID {
		t.Errorf("expected TraceID=%q, got %q", incomingTraceID, gotTC.TraceID)
	}
}

func TestMiddleware_NewSpanIDPerRequest(t *testing.T) {
	// SpanID phải luôn mới, không giữ spanID của upstream
	tr := newTestTracer()
	upstreamSpanID := "00f067aa0ba902b7"
	incomingTP := "00-4bf92f3577b34da6a3ce929d0e0e4736-" + upstreamSpanID + "-01"

	var gotTC TraceContext
	handler := tr.CombinedTracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTC, _ = TraceContextFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(HeaderTraceParent, incomingTP)
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if gotTC.SpanID == upstreamSpanID {
		t.Error("SpanID should be new, not the upstream spanID")
	}
	if gotTC.ParentSpanID != upstreamSpanID {
		t.Errorf("ParentSpanID should be upstream spanID %q, got %q", upstreamSpanID, gotTC.ParentSpanID)
	}
}

func TestMiddleware_FallbackToRequestID(t *testing.T) {
	tr := newTestTracer()
	var gotTC TraceContext

	handler := tr.CombinedTracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTC, _ = TraceContextFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(HeaderRequestID, "my-custom-request-id")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if gotTC.TraceID != "my-custom-request-id" {
		t.Errorf("expected TraceID from X-Request-ID, got %q", gotTC.TraceID)
	}
}

func TestMiddleware_InvalidTraceparent_GeneratesNew(t *testing.T) {
	tr := newTestTracer()
	var gotTC TraceContext

	handler := tr.CombinedTracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTC, _ = TraceContextFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set(HeaderTraceParent, "invalid-header")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if gotTC.TraceID == "" {
		t.Error("expected new TraceID when traceparent is invalid")
	}
}

func TestMiddleware_PropagatesHeadersToBackend(t *testing.T) {
	tr := newTestTracer()

	handler := tr.CombinedTracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// request đến backend phải có traceparent
		tp := r.Header.Get(HeaderTraceParent)
		if tp == "" {
			t.Error("expected traceparent header propagated to backend")
		}
		if !strings.HasPrefix(tp, "00-") {
			t.Errorf("traceparent should start with '00-', got %q", tp)
		}
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	handler.ServeHTTP(httptest.NewRecorder(), r)
}

// ============================================================
// RequestIDFromContext / TraceContextFromContext
// ============================================================

func TestRequestIDFromContext_Empty(t *testing.T) {
	got := RequestIDFromContext(context.Background())
	if got != "unknown" {
		t.Errorf("expected 'unknown', got %q", got)
	}
}

func TestTraceContextFromContext_Empty(t *testing.T) {
	_, ok := TraceContextFromContext(context.Background())
	if ok {
		t.Error("expected ok=false for empty context")
	}
}
