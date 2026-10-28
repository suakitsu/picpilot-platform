package server

import (
	"testing"

	"github.com/suakitsu/picpilot-platform/core/config"
)

func TestCaptchaAllowsConfiguredAdminHostNotSiblingOrSuffix(t *testing.T) {
	s := &Server{Config: config.Config{PublicBaseURL: "https://app.example.invalid", AllowedOrigins: []string{"https://admin.example.invalid:8443"}}}
	for _, hostname := range []string{"app.example.invalid", "admin.example.invalid", "ADMIN.EXAMPLE.INVALID"} {
		if !s.captchaHostnameAllowed(hostname) {
			t.Fatal("configured origin rejected")
		}
	}
	for _, hostname := range []string{"", "example.invalid", "app.example.invalid.attacker.invalid", "other.example.invalid", "https://admin.example.invalid"} {
		if s.captchaHostnameAllowed(hostname) {
			t.Fatal("unconfigured origin accepted")
		}
	}
}
