// Package localmachine exposes the panel's own machine identity for the
// inbound form's machine dropdown: the local row shows hostname + (IP:port)
// next to the node rows' name (address), instead of a bare "本机".
package localmachine

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Machine is the panel host's identity as shown in the UI.
type Machine struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
}

// Handler serves the local-machine endpoint.
type Handler struct {
	addr string
}

// NewHandler creates the handler; addr is the panel's listen address
// (e.g. ":32434"), from which the displayed port is derived.
func NewHandler(addr string) *Handler { return &Handler{addr: addr} }

// RegisterProtected registers the endpoint behind the auth guard.
func (h *Handler) RegisterProtected(rg *gin.RouterGroup) {
	rg.GET("/local-machine", h.get)
}

func (h *Handler) get(c *gin.Context) {
	hostname, _ := os.Hostname()
	c.JSON(http.StatusOK, Machine{
		Hostname: hostname,
		IP:       outboundIP(),
		Port:     portOf(h.addr),
	})
}

// portOf extracts the port from a listen address such as ":32434" or
// "127.0.0.1:32434". It returns 0 when no port can be determined.
func portOf(addr string) int {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		if n, err := strconv.Atoi(port); err == nil {
			return n
		}
		return 0
	}
	if strings.HasPrefix(addr, ":") {
		if n, err := strconv.Atoi(strings.TrimPrefix(addr, ":")); err == nil {
			return n
		}
	}
	return 0
}

// outboundIP returns the host's primary outbound IP: the address the
// kernel would use to reach the internet. The UDP "dial" sends no
// packets — it only resolves the local address from the routing table.
// It falls back to the first non-loopback IPv4 on an up interface.
func outboundIP() string {
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		defer conn.Close()
		if udp, ok := conn.LocalAddr().(*net.UDPAddr); ok && udp.IP != nil {
			return udp.IP.String()
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}
