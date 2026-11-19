package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/TheAmgadX/Spill-Social-Media-Platform/internal/views"
)

// RequestID is a middleware that injects a request ID into the context of each
// request. If an X-Request-ID header exists on the incoming request, it is used;
// otherwise, a new unique request ID is generated. The request ID is also set
// on the response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			var b [12]byte
			_, _ = rand.Read(b[:])
			reqID = "req_" + hex.EncodeToString(b[:])
		}

		w.Header().Set("X-Request-ID", reqID)
		ctx := views.WithReqID(r.Context(), reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetReqID returns the request ID from the context if present.
func GetReqID(ctx context.Context) string {
	return views.GetReqID(ctx)
}

// RealIP is a middleware that sets RemoteAddr to the client's real IP,
// inspecting X-Real-IP and X-Forwarded-For headers.
func RealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			r.RemoteAddr = realIP
		} else if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				r.RemoteAddr = strings.TrimSpace(parts[0])
			}
		}
		next.ServeHTTP(w, r)
	})
}

// statusWriter wraps http.ResponseWriter to capture status code and bytes written.
type statusWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (w *statusWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += n
	return n, err
}

// Logger is a middleware that logs the HTTP method, path, status, duration, and request ID.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}

		next.ServeHTTP(sw, r)

		status := sw.statusCode
		if status == 0 {
			status = http.StatusOK
		}

		reqID := GetReqID(r.Context())
		log.Printf("[HTTP] %s %s - %d (%s) req_id=%s",
			r.Method, r.URL.Path, status, time.Since(start), reqID)
	})
}

// Recoverer is a middleware that recovers from panics, logs the stack trace,
// and returns an HTTP 500 Problem Detail response.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rvr := recover(); rvr != nil {
				log.Printf("[PANIC] %v\n%s", rvr, debug.Stack())
				views.WriteProblem(w, r, http.StatusInternalServerError, "An unexpected server error occurred.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Timeout is a middleware that sets a context timeout for request handling.
func Timeout(duration time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
