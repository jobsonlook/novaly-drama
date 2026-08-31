package main

import "testing"

func TestLocalListenAddress(t *testing.T) {
	for _, tc := range []struct{ name, host, want string }{
		{"native stays private", "", "127.0.0.1:8086"},
		{"container interface", "0.0.0.0", "0.0.0.0:8086"},
		{"ipv6 loopback", "::1", "[::1]:8086"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DOUBAO_LISTEN_HOST", tc.host)
			if got := localListenAddress("8086"); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
