package logging

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================
// Helper
// ============================================================

// logBuffer bắt log output để assert trong test
func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	return logger, buf
}

func handlerWithStatus(code int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
	})
}

func handlerWithBody(code int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	})
}

// ============================================================
// NewLogger
// ============================================================

func TestNewLogger_NilUsesDefault(t *testing.T) {
	l := NewLogger(nil)
	if l == nil {
		t.Fatal("expected non-nil ILogger")
	}
}

func TestNewLogger_CustomLogger(t *testing.T) {
	logger, _ := newTestLogger()
	l := NewLogger(logger)
	if l == nil {
		t.Fatal("expected non-nil ILogger")
	}
}

// ============================================================
// responseWriterInterceptor
// ============================================================

func TestInterceptor_DefaultStatus200(t *testing.T) {
	w := httptest.NewRecorder()
	interceptor := &responseWriterInterceptor{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
	// Không gọi WriteHeader — statusCode phải vẫn là 200
	if interceptor.statusCode != http.StatusOK {
		t.Errorf("got %d, want 200", interceptor.statusCode)
	}
}

func TestInterceptor_WriteHeader_SetsStatus(t *testing.T) {
	w := httptest.NewRecorder()
	interceptor := &responseWriterInterceptor{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
	interceptor.WriteHeader(http.StatusCreated)
	if interceptor.statusCode != http.StatusCreated {
		t.Errorf("got %d, want 201", interceptor.statusCode)
	}
}

func TestInterceptor_WriteHeader_Idempotent(t *testing.T) {
	// Gọi WriteHeader 2 lần — chỉ lần đầu có hiệu lực
	w := httptest.NewRecorder()
	interceptor := &responseWriterInterceptor{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
	interceptor.WriteHeader(http.StatusCreated)
	interceptor.WriteHeader(http.StatusBadRequest) // phải bị bỏ qua
	if interceptor.statusCode != http.StatusCreated {
		t.Errorf("second WriteHeader should be ignored, got %d", interceptor.statusCode)
	}
}

func TestInterceptor_Write_SetsWrittenHeader(t *testing.T) {
	// FIX: Write phải tự gọi WriteHeader(200) nếu chưa được gọi
	w := httptest.NewRecorder()
	interceptor := &responseWriterInterceptor{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
	_, _ = interceptor.Write([]byte("hello"))
	if !interceptor.writtenHeader {
		t.Error("writtenHeader should be true after Write")
	}
}

func TestInterceptor_Write_CountsBytes(t *testing.T) {
	w := httptest.NewRecorder()
	interceptor := &responseWriterInterceptor{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
	_, _ = interceptor.Write([]byte("hello"))
	_, _ = interceptor.Write([]byte(" world"))
	if interceptor.bytesWritten != 11 {
		t.Errorf("got %d bytes, want 11", interceptor.bytesWritten)
	}
}

func TestInterceptor_Write_AfterWriteHeader_NoOverride(t *testing.T) {
	// Nếu đã WriteHeader(201) rồi mới Write — status phải giữ 201
	w := httptest.NewRecorder()
	interceptor := &responseWriterInterceptor{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
	interceptor.WriteHeader(http.StatusCreated)
	_, _ = interceptor.Write([]byte("body"))
	if interceptor.statusCode != http.StatusCreated {
		t.Errorf("Write should not override status, got %d", interceptor.statusCode)
	}
}

// ============================================================
// Middleware — log level theo status code
// ============================================================

func TestMiddleware_LogsInfo_For2xx(t *testing.T) {
	logger, buf := newTestLogger()
	m := NewLogger(logger)

	r := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.RemoteAddr = "1.2.3.4:80"
	w := httptest.NewRecorder()
	m.Middleware(handlerWithStatus(http.StatusOK)).ServeHTTP(w, r)

	if !strings.Contains(buf.String(), "INFO") {
		t.Errorf("expected INFO log, got: %s", buf.String())
	}
}

func TestMiddleware_LogsWarn_For4xx(t *testing.T) {
	logger, buf := newTestLogger()
	m := NewLogger(logger)

	r := httptest.NewRequest(http.MethodGet, "/missing", nil)
	r.RemoteAddr = "1.2.3.4:80"
	w := httptest.NewRecorder()
	m.Middleware(handlerWithStatus(http.StatusNotFound)).ServeHTTP(w, r)

	if !strings.Contains(buf.String(), "WARN") {
		t.Errorf("expected WARN log, got: %s", buf.String())
	}
}

func TestMiddleware_LogsError_For5xx(t *testing.T) {
	logger, buf := newTestLogger()
	m := NewLogger(logger)

	r := httptest.NewRequest(http.MethodGet, "/crash", nil)
	r.RemoteAddr = "1.2.3.4:80"
	w := httptest.NewRecorder()
	m.Middleware(handlerWithStatus(http.StatusInternalServerError)).ServeHTTP(w, r)

	if !strings.Contains(buf.String(), "ERROR") {
		t.Errorf("expected ERROR log, got: %s", buf.String())
	}
}

// ============================================================
// Middleware — log fields
// ============================================================

func TestMiddleware_LogsMethod(t *testing.T) {
	logger, buf := newTestLogger()
	m := NewLogger(logger)

	r := httptest.NewRequest(http.MethodPost, "/api", nil)
	r.RemoteAddr = "1.2.3.4:80"
	m.Middleware(handlerWithStatus(200)).ServeHTTP(httptest.NewRecorder(), r)

	if !strings.Contains(buf.String(), "POST") {
		t.Errorf("expected method POST in log, got: %s", buf.String())
	}
}

func TestMiddleware_LogsPath(t *testing.T) {
	logger, buf := newTestLogger()
	m := NewLogger(logger)

	r := httptest.NewRequest(http.MethodGet, "/some/path", nil)
	r.RemoteAddr = "1.2.3.4:80"
	m.Middleware(handlerWithStatus(200)).ServeHTTP(httptest.NewRecorder(), r)

	if !strings.Contains(buf.String(), "/some/path") {
		t.Errorf("expected path in log, got: %s", buf.String())
	}
}

func TestMiddleware_LogsQuery_WhenPresent(t *testing.T) {
	logger, buf := newTestLogger()
	m := NewLogger(logger)

	r := httptest.NewRequest(http.MethodGet, "/search?q=hello", nil)
	r.RemoteAddr = "1.2.3.4:80"
	m.Middleware(handlerWithStatus(200)).ServeHTTP(httptest.NewRecorder(), r)

	if !strings.Contains(buf.String(), "q=hello") {
		t.Errorf("expected query in log, got: %s", buf.String())
	}
}

func TestMiddleware_NoQuery_WhenAbsent(t *testing.T) {
	logger, buf := newTestLogger()
	m := NewLogger(logger)

	r := httptest.NewRequest(http.MethodGet, "/hello", nil)
	r.RemoteAddr = "1.2.3.4:80"
	m.Middleware(handlerWithStatus(200)).ServeHTTP(httptest.NewRecorder(), r)

	if strings.Contains(buf.String(), "query=") {
		t.Errorf("should not log query field when absent, got: %s", buf.String())
	}
}

func TestMiddleware_LogsRemoteIP(t *testing.T) {
	logger, buf := newTestLogger()
	m := NewLogger(logger)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "5.6.7.8:1234"
	m.Middleware(handlerWithStatus(200)).ServeHTTP(httptest.NewRecorder(), r)

	if !strings.Contains(buf.String(), "5.6.7.8") {
		t.Errorf("expected remote IP in log, got: %s", buf.String())
	}
}

func TestMiddleware_LogsRespBytes(t *testing.T) {
	logger, buf := newTestLogger()
	m := NewLogger(logger)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:80"
	m.Middleware(handlerWithBody(200, "hello world")).ServeHTTP(httptest.NewRecorder(), r)

	// "resp_bytes=11"
	if !strings.Contains(buf.String(), "resp_bytes=11") {
		t.Errorf("expected resp_bytes=11 in log, got: %s", buf.String())
	}
}

func TestMiddleware_RemoteAddr_NoPort(t *testing.T) {
	// RemoteAddr không có port — không được panic
	logger, _ := newTestLogger()
	m := NewLogger(logger)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4" // không có port
	w := httptest.NewRecorder()

	// Không panic là pass
	m.Middleware(handlerWithStatus(200)).ServeHTTP(w, r)
}
