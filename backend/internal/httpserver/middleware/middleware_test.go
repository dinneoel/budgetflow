package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowsUpToLimit(t *testing.T) {
	l := NewRateLimiter(3, time.Minute)
	for i := 1; i <= 3; i++ {
		if !l.Allow("k") {
			t.Fatalf("request %d denied, want allowed", i)
		}
	}
	if l.Allow("k") {
		t.Error("request over the limit allowed, want denied")
	}
	if !l.Allow("other-key") {
		t.Error("separate key denied, keys must be independent")
	}
}

func TestRateLimiterResetsAfterWindow(t *testing.T) {
	l := NewRateLimiter(1, 50*time.Millisecond)
	if !l.Allow("k") {
		t.Fatal("first request denied")
	}
	if l.Allow("k") {
		t.Fatal("second request in window allowed")
	}
	time.Sleep(60 * time.Millisecond)
	if !l.Allow("k") {
		t.Error("request after window expiry denied, want allowed")
	}
}

func TestRateLimitByIPMiddleware(t *testing.T) {
	handler := RateLimitByIP(NewRateLimiter(2, time.Minute))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	statusFor := func(addr string) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < 2; i++ {
		if got := statusFor("10.0.0.1:1234"); got != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200", i+1, got)
		}
	}
	if got := statusFor("10.0.0.1:9999"); got != http.StatusTooManyRequests {
		t.Errorf("over-limit request: status %d, want 429 (limit must key on IP, not port)", got)
	}
	if got := statusFor("10.0.0.2:1234"); got != http.StatusOK {
		t.Errorf("different IP: status %d, want 200", got)
	}
}

func TestRateLimiterPrunesExpiredEntries(t *testing.T) {
	l := NewRateLimiter(1, 10*time.Millisecond)
	for i := 0; i < 10_001; i++ {
		l.Allow(fmt.Sprintf("key-%d", i))
	}
	time.Sleep(20 * time.Millisecond)
	l.Allow("trigger-prune")
	l.mu.Lock()
	size := len(l.hits)
	l.mu.Unlock()
	if size > 10 {
		t.Errorf("limiter holds %d entries after expiry, expected pruning", size)
	}
}
