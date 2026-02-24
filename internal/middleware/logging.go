package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"time"
)

type ILogger interface {
	Middleware(next http.Handler) http.Handler
}

type logManager struct {
	logger *slog.Logger
}

/*
* nhan cau hinh logger va su dung neu khong co thi dung mac dinh
 */
func NewLogger(l *slog.Logger) ILogger {
	if l == nil {
		l = slog.Default()
	}

	return &logManager{
		logger: l,
	}
}

/*
*thuc hien ghi log
 */
func (l *logManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		interceptor := &responseWriterInterceptor{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // default
		}

		//trueyn writer cua minhtu custom
		next.ServeHTTP(interceptor, r)
		latency := time.Since(start)

		remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			remoteIP = r.RemoteAddr
		}

		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote_ip", remoteIP),
			slog.Int("status", interceptor.statusCode),
			slog.Int64("latency_us", latency.Microseconds()),
			slog.Int64("resp_bytes", interceptor.bytesWritten),
		}

		if r.URL.RawQuery != "" {
			attrs = append(attrs, slog.String("query", r.URL.RawQuery))
		}

		logLevel := slog.LevelInfo
		if interceptor.statusCode >= 500 {
			logLevel = slog.LevelError
		} else if interceptor.statusCode >= 400 {
			logLevel = slog.LevelWarn
		}

		l.logger.LogAttrs(
			r.Context(),
			logLevel,
			"HTTP request completed",
			attrs...,
		)
	})
}

type responseWriterInterceptor struct {
	http.ResponseWriter //khong duo cphep dat name vi neu nhu dat name thi gia tri gan vao cac ham implement se la cua field do chu khong phai cua struct khien cho tai phai implment head
	statusCode          int
	bytesWritten        int64 // diem so luong byte di qua moi lan request
	writtenHeader       bool  //kiem tra xem co
}

/*
* kiem tra va set statusCode va header cho logging
 */
func (w *responseWriterInterceptor) WriteHeader(code int) {
	if !w.writtenHeader {
		w.statusCode = code
		w.writtenHeader = true
		w.ResponseWriter.WriteHeader(code)
	}
}

/*
* truyen du lieu  va tinh toan dung luong
 */
func (w *responseWriterInterceptor) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += int64(n)
	return n, err
}
