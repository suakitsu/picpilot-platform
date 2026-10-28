package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/suakitsu/picpilot-platform/core/config"
)

func TestRecoveryPasswordInputIsBounded(t *testing.T) {
	for _, raw := range []string{"short", strings.Repeat("x", 73), "fixture-password\nsecond line", strings.Repeat("x", 2048), "fixture\x00password"} {
		if _, err := readRecoveryPassword(strings.NewReader(raw)); err == nil {
			t.Fatal("unsafe password accepted")
		}
	}
	if password, err := readRecoveryPassword(strings.NewReader(" fixture-password \r\n")); err != nil || password != " fixture-password " {
		t.Fatal("password modified", err)
	}
}

func TestVersionAndRecoveryFlagsDoNotInitializeData(t *testing.T) {
	var out, errOut bytes.Buffer
	if status := RunWithInput(config.Personal, []string{"--version"}, nil, &out, &errOut); status != 0 || !strings.Contains(out.String(), "personal") {
		t.Fatal("version failed")
	}
	out.Reset()
	errOut.Reset()
	if status := RunWithInput(config.Platform, []string{"--reset-owner-password-stdin"}, strings.NewReader("fixture-password"), &out, &errOut); status != 2 {
		t.Fatal("recovery without exact account accepted")
	}
	if strings.Contains(errOut.String(), "fixture-password") {
		t.Fatal("password leaked")
	}
}
