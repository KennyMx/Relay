package api

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Host validation blocks DNS rebinding even when the gateway is loopback-only.
// Origin and Fetch Metadata validation reject cross-origin browser callers.
// CLI callers without Origin continue to use explicit bearer authentication.
func (s *Server) acceptBrowserRequest(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	allowed := s.AllowedHosts
	if len(allowed) == 0 {
		allowed = []string{"localhost", "127.0.0.1", "::1"}
	}
	trusted := false
	for _, entry := range allowed {
		if strings.EqualFold(host, strings.TrimSpace(entry)) {
			trusted = true
			break
		}
	}
	if !trusted || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
			u.Path != "" || u.RawQuery != "" || u.Fragment != "" || !strings.EqualFold(u.Host, r.Host) {
			return false
		}
	}
	return true
}
