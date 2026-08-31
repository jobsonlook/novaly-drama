package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalShutdownAuthorization(t *testing.T) {
	for _, tc := range []struct {
		method, remote, token, configured string
		want                              int
	}{
		{"POST", "127.0.0.1:1234", "secret", "secret", 202},
		{"POST", "127.0.0.1:1234", "wrong", "secret", 403},
		{"POST", "203.0.113.2:1234", "secret", "secret", 403},
		{"GET", "127.0.0.1:1234", "secret", "secret", 403},
		{"POST", "127.0.0.1:1234", "", "", 403},
	} {
		stopped := false
		h := localShutdownHandler(http.NotFoundHandler(), tc.configured, func() { stopped = true })
		req := httptest.NewRequest(tc.method, "http://localhost/internal/local-shutdown", nil)
		req.RemoteAddr = tc.remote
		req.Header.Set("X-Novaly-Shutdown", tc.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.want || stopped != (tc.want == 202) {
			t.Fatalf("%+v: status=%d stopped=%v", tc, w.Code, stopped)
		}
	}
}
