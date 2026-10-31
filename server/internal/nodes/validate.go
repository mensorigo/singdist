package nodes

import (
	"math"
	"net"
	"strconv"
	"strings"
)

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return "", errValidation("name must be 1-64 characters")
	}
	return name, nil
}

// truncateRunes caps a derived name at n runes without appending an
// ellipsis (unlike truncate, which is for error snippets).
// truncateRunes caps a derived name at n runes without appending an
// ellipsis (unlike truncate, which is for error snippets).
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func validateAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", errValidation("address is required")
	}
	_, hostport, err := ParseAddress(address)
	if err != nil {
		return "", errValidation(err.Error())
	}
	_, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", errValidation("address must be host:port")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", errValidation("address port must be 1-65535")
	}
	return address, nil
}
func validateToken(token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errValidation("token is required")
	}
	if len(token) > 512 {
		return "", errValidation("token is too long")
	}
	return token, nil
}
func validateTLSMode(mode string) (string, error) {
	if mode == "" {
		return "skip-verify", nil
	}
	if mode != "skip-verify" {
		return "", errValidation("tlsMode not supported yet (only skip-verify in v1)")
	}
	return mode, nil
}
func validateRatio(ratio *float64) (float64, error) {
	if ratio == nil {
		return 1.0, nil
	}
	if *ratio <= 0 || math.IsNaN(*ratio) || math.IsInf(*ratio, 0) {
		return 0, errValidation("usageRatio must be greater than 0")
	}
	return *ratio, nil
}
