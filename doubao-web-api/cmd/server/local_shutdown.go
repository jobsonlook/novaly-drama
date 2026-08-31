package main

import (
	"crypto/subtle"
	"net"
	"net/http"
)

func localShutdownHandler(next http.Handler, token string, stop func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/local-shutdown" {
			next.ServeHTTP(w, r)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if token == "" || err != nil || ip == nil || !ip.IsLoopback() || r.Method != http.MethodPost || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Novaly-Shutdown")), []byte(token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		stop()
	})
}
