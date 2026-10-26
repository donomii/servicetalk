package main

import (
	"fmt"
	"net"
	"os"
)

// localHostname returns the advertised hostname for this machine.
// Checks SMT_HOST env var first, then auto-detects a non-loopback IPv4 address,
// and falls back to "localhost" for single-node operation.
func localHostname() string {
	if h := os.Getenv("SMT_HOST"); h != "" {
		return h
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String()
		}
	}
	return "localhost"
}

// selfURL returns the externally-reachable HTTP base URL for a server on this machine.
func selfURL(port int) string {
	return fmt.Sprintf("http://%s:%d", localHostname(), port)
}
