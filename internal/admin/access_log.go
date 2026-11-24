package admin

import (
	"log/slog"
	"net/http"
	"time"
)

// AccessLogMiddleware logs all admin endpoint access for audit completeness.
func AccessLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(rw, r)
		slog.Info("admin access",
			"component", "audit",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"remote", r.RemoteAddr,
			"latency_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush proxies to the underlying Flusher for streaming responses.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for Hijack/Push detection.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
