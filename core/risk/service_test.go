package risk

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type memoryBackend struct {
	config  Config
	events  []Event
	deny    bool
	failure error
}

func (b *memoryBackend) RiskConfig(context.Context) (Config, error) { return b.config, b.failure }
func (b *memoryBackend) RiskUserAllowed(context.Context, string) error {
	if b.deny {
		return ErrAccountRestricted
	}
	return nil
}
func (b *memoryBackend) RecordRiskHit(_ context.Context, e Event, _ int) (Decision, error) {
	b.events = append(b.events, e)
	return Decision{Action: e.Action, Level: e.Level, Count: int64(len(b.events))}, nil
}

func TestRiskLiteralBoundariesAndNegation(t *testing.T) {
	c := DefaultConfig()
	for _, tt := range []struct{ text, level string }{
		{"Visible text policy involves short readable words.", ""},
		{"A Nike shoe", "medium"}, {"A nikestore sign", ""},
		{"Without Nike branding", ""}, {"Avoid Nike, but include Adidas", "medium"},
		{"Do not include Nike branding", ""}, {"not without Nike branding", "medium"},
		{"Avoid too much Nike branding", "medium"},
		{"\u539f\u521b\u6e29\u99a8\u6d77\u62a5\uff0c\u907f\u514d\u4f4e\u6e05\u3001\u7578\u5f62\u624b\u6307\u3001\u8272\u60c5\u3001\u8840\u8165\u6050\u6016\u5143\u7d20\u3002", ""},
		{"\u907f\u514d\u4f4e\u6e05\uff0c\u4f46\u662f\u9700\u8981\u7a81\u51fa\u8840\u8165\u51b2\u51fb\u611f\u3002", "high"},
		{"\u4e0d\u8981\u907f\u514d\u8840\u8165", "high"},
		{"\u8840\u8165\u4e0d\u8981\u51fa\u73b0", ""},
	} {
		t.Run(tt.text, func(t *testing.T) {
			level, _ := FirstHit(c, tt.text)
			if level != tt.level {
				t.Fatalf("level %q, want %q", level, tt.level)
			}
		})
	}
	// Keywords are literal, including regex metacharacters.
	c.Keywords["high"] = []string{"a+b"}
	if l, _ := FirstHit(c, "a+b"); l != "high" {
		t.Fatal("literal keyword not matched")
	}
}

func TestRiskSourcesProviderRedactionAndFailClosed(t *testing.T) {
	b := &memoryBackend{config: DefaultConfig()}
	s := Service{Backend: b}
	ctx := context.Background()
	r := Request{UserID: "u1", Feature: "creative", Source: "ai_output", RequestKey: "fixture-key", Text: "Nike PRIVATE_PROMPT"}
	if _, err := s.Check(ctx, r); err != nil || len(b.events) != 0 {
		t.Fatal("generated prompt was screened")
	}
	r.Source = "user_input"
	if _, err := s.Check(ctx, r); !errors.Is(err, ErrBlocked) || len(b.events) != 1 {
		t.Fatal("user input was not screened")
	}
	if strings.Contains(b.events[0].Keyword, "PRIVATE_PROMPT") {
		t.Fatal("input persisted")
	}
	r.TaskID = "task1"
	r.Text = "PRIVATE_AUTH response policy PRIVATE_PROMPT"
	if _, err := s.RecordProviderError(ctx, r); err != nil {
		t.Fatal(err)
	}
	if e := b.events[1]; e.Keyword != "policy" || e.RequestKey != "provider:task1" || e.Source != "provider_error" {
		t.Fatal("provider log is not sanitized")
	}
	b.deny = true
	r.Source = "ai_output"
	if _, err := s.Check(ctx, r); !errors.Is(err, ErrAccountRestricted) {
		t.Fatal("source bypassed account restriction")
	}
	b.deny = false
	b.failure = errors.New("config unavailable")
	r.Source = "user_input"
	if _, err := s.Check(ctx, r); err == nil {
		t.Fatal("risk config failure allowed submission")
	}
}

func TestRiskConfigurationValidation(t *testing.T) {
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.WarnLevels = []string{"high"}
	if c.Validate() == nil {
		t.Fatal("overlapping levels accepted")
	}
	c = DefaultConfig()
	c.BanThreshold = 0
	if c.Validate() == nil {
		t.Fatal("invalid threshold accepted")
	}
	c = DefaultConfig()
	c.Keywords["high"] = []string{"a", " A "}
	if c.Validate() == nil {
		t.Fatal("duplicate keywords accepted")
	}
	c = DefaultConfig()
	c.WarnLevels = []string{}
	b := &memoryBackend{config: c}
	s := Service{Backend: b}
	if _, err := s.Check(context.Background(), Request{UserID: "u1", Source: "user_input", Feature: "image", RequestKey: "fixture-key", Text: "\u6027\u611f"}); err != nil || len(b.events) != 0 {
		t.Fatal("inactive level recorded")
	}
	c.BlockLevels = []string{"medium"}
	c.Keywords["high"] = []string{"inactive"}
	b.config = c
	if _, err := s.Check(context.Background(), Request{UserID: "u1", Source: "user_input", Feature: "image", RequestKey: "fixture-two", Text: "inactive Nike"}); !errors.Is(err, ErrBlocked) {
		t.Fatal("inactive higher severity concealed an active match")
	}
}
