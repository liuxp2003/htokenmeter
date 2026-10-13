package common

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"strings"
)

// GenKey returns a random 48-char hex key for gateway tokens.
func GenKey(n int) string {
	if n <= 0 {
		n = 24
	}
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RandomPassword returns a readable random password for first-boot admin.
func RandomPassword() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ClientIP extracts the real client IP, trusting only direct connection
// (routers sit at the edge; no reverse proxy by default).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// IsIPAllowed checks ip against a comma-separated CIDR / plain-IP list.
func IsIPAllowed(ip, list string) bool {
	if strings.TrimSpace(list) == "" {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			if net.ParseIP(item) == nil {
				continue
			}
			item += "/32"
			if parsed.To4() == nil {
				item = "/128"
			}
		}
		if _, network, err := net.ParseCIDR(item); err == nil && network.Contains(parsed) {
			return true
		}
	}
	return false
}

// ReadBodyAll reads r with a size cap.
func ReadBodyAll(r io.Reader, maxBytes int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxBytes))
}
