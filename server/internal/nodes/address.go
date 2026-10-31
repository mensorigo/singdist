package nodes

import (
	"fmt"
	"net"
	"strings"
)

// ParseAddress splits a node address into scheme and host:port. A bare
// host:port means https (the agent serves TLS by default); an explicit
// http:// prefix selects plaintext (tests and dev agents with TLS off).
func ParseAddress(address string) (scheme, hostport string, err error) {
	scheme = "https"
	rest := strings.TrimSpace(address)
	if strings.HasPrefix(rest, "http://") {
		scheme = "http"
		rest = strings.TrimPrefix(rest, "http://")
	} else if strings.HasPrefix(rest, "https://") {
		rest = strings.TrimPrefix(rest, "https://")
	}
	rest = strings.TrimSuffix(rest, "/")
	host, port, err := net.SplitHostPort(rest)
	if err != nil {
		return "", "", fmt.Errorf("invalid address %q: %w", address, err)
	}
	if host == "" || port == "" {
		return "", "", fmt.Errorf("invalid address %q: host and port required", address)
	}
	return scheme, rest, nil
}

// configured public host, or the address host when none is set.
// configured public host, or the address host when none is set.
func PublicHostOf(publicHost, address string) string {
	if strings.TrimSpace(publicHost) != "" {
		return strings.TrimSpace(publicHost)
	}
	_, hostport, err := ParseAddress(address)
	if err != nil {
		return address
	}
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
}

// --- HTTP client ------------------------------------------------------------
