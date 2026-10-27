package server

import (
	"net"
	"net/http"
	"net/netip"
)

func (s *Server) trustedProxy(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	for _, prefix := range s.Config.TrustedProxyCIDRs {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) secureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	values := r.Header.Values("X-Forwarded-Proto")
	return s.trustedProxy(r) && len(values) == 1 && values[0] == "https"
}

func (s *Server) clientAddress(r *http.Request) string {
	if s.trustedProxy(r) {
		values := r.Header.Values("X-Forwarded-For")
		if len(values) == 1 {
			if ip, err := netip.ParseAddr(values[0]); err == nil {
				return ip.Unmap().String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
