package server

import (
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/suakitsu/picpilot-platform/core/config"
)

func TestProxyHTTPSCannotBeSpoofed(t *testing.T) {
	s := &Server{Config: config.Config{TrustedProxyCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}}
	r := httptest.NewRequest("POST", "http://localhost/internal", nil)
	r.RemoteAddr = "198.51.100.1:4000"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-For", "192.0.2.1")
	if s.secureRequest(r) || s.clientAddress(r) != "198.51.100.1" {
		t.Fatal("untrusted forwarded headers accepted")
	}
	r.RemoteAddr = "127.0.0.1:4000"
	if !s.secureRequest(r) || s.clientAddress(r) != "192.0.2.1" {
		t.Fatal("trusted proxy rejected")
	}
	r.Header.Add("X-Forwarded-Proto", "http")
	if s.secureRequest(r) {
		t.Fatal("ambiguous proto headers accepted")
	}
	r.Header.Set("X-Forwarded-Proto", "https,http")
	if s.secureRequest(r) {
		t.Fatal("proto list accepted")
	}
	r.Header.Set("X-Forwarded-For", "192.0.2.1, 192.0.2.2")
	if s.clientAddress(r) != "127.0.0.1" {
		t.Fatal("untrusted client chain accepted")
	}
}
