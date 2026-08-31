package main

import (
	"net"
	"os"
)

func localListenAddress(port string) string {
	host := os.Getenv("DOUBAO_LISTEN_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
