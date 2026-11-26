package util

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// HTTPPostProxiedOnce must make exactly one attempt: it exists for
// non-idempotent deliveries (Pushover) where a retry after a lost response
// would send the notification twice.
func TestHTTPPostProxiedOnceDoesNotRetry(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("test server does not support hijacking")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer srv.Close()

	oldTimeout := HTTPTimeout
	HTTPTimeout = 5
	t.Cleanup(func() { HTTPTimeout = oldTimeout })

	// "direct" keeps the test independent of any proxy environment.
	if _, err := HTTPPostProxiedOnce(srv.URL, false, "direct", []byte("{}"), ""); err == nil {
		t.Fatal("expected the closed connection to fail the request")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server saw %d attempts, want exactly 1", got)
	}
}
