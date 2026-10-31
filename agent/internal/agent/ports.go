package agent

import (
	"fmt"
	"os"
	"strings"
)

// Listening is the set of locally bound ports, read from /proc
// (state observation only — no connection is opened).
type Listening struct {
	TCP map[int]bool
	UDP map[int]bool
}

// ListeningPorts reads /proc/net/tcp{,6} (LISTEN state) and
// /proc/net/udp{,6} (any bound socket). An unreadable table is
// skipped silently.
// ListeningPorts reads /proc/net/tcp{,6} (LISTEN state) and
// /proc/net/udp{,6} (any bound socket). An unreadable table is
// skipped silently.
func ListeningPorts() Listening {
	out := Listening{TCP: map[int]bool{}, UDP: map[int]bool{}}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[0] == "sl" || fields[3] != "0A" {
				continue
			}
			parts := strings.Split(fields[1], ":")
			if len(parts) != 2 {
				continue
			}
			var port int
			if _, err := fmt.Sscanf(parts[1], "%x", &port); err == nil {
				out.TCP[port] = true
			}
		}
	}
	for _, path := range []string{"/proc/net/udp", "/proc/net/udp6"} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[0] == "sl" {
				continue
			}
			parts := strings.Split(fields[1], ":")
			if len(parts) != 2 {
				continue
			}
			var port int
			if _, err := fmt.Sscanf(parts[1], "%x", &port); err == nil {
				out.UDP[port] = true
			}
		}
	}
	return out
}
