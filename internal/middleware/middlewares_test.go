package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestID(t *testing.T) {
	var capturedID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetReqID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// Case 1: Auto-generate request ID
	req1 := httptest.NewRequest("GET", "/test", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)

	if capturedID == "" {
		t.Fatal("expected non-empty request ID in context")
	}
	if !strings.HasPrefix(capturedID, "req_") {
		t.Errorf("expected request ID to start with 'req_', got %s", capturedID)
	}
	if respHeader := rr1.Header().Get("X-Request-ID"); respHeader != capturedID {
		t.Errorf("X-Request-ID response header %s != context %s", respHeader, capturedID)
	}

	// Case 2: Preserve incoming X-Request-ID
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-Request-ID", "custom-client-id-123")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if capturedID != "custom-client-id-123" {
		t.Errorf("expected 'custom-client-id-123', got %s", capturedID)
	}
	if rr2.Header().Get("X-Request-ID") != "custom-client-id-123" {
		t.Errorf("expected response header 'custom-client-id-123', got %s", rr2.Header().Get("X-Request-ID"))
	}
}

func TestRealIP(t *testing.T) {
	var capturedIP string
	handler := RealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedIP = r.RemoteAddr
		w.WriteHeader(http.StatusOK)
	}))

	// Case 1: X-Real-IP
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("X-Real-IP", "192.168.1.100")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if capturedIP != "192.168.1.100" {
		t.Errorf("expected X-Real-IP '192.168.1.100', got %s", capturedIP)
	}

	// Case 2: X-Forwarded-For
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if capturedIP != "203.0.113.195" {
		t.Errorf("expected X-Forwarded-For first IP '203.0.113.195', got %s", capturedIP)
	}
}

func TestRecoverer(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went critically wrong!")
	}))

	req := httptest.NewRequest("GET", "/panic", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 on panic recovery, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "internal-error") {
		t.Errorf("expected problem detail in body, got: %s", rr.Body.String())
	}
}

func TestTimeout(t *testing.T) {
	handler := Timeout(100 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			// Context timed out properly
			return
		}
	}))

	req := httptest.NewRequest("GET", "/slow", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
}

func TestLogger(t *testing.T) {
	handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/logged", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Errorf("expected status 202, got %d", rr.Code)
	}
}
