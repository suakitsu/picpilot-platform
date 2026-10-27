package risk

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrBlocked           = errors.New("content was blocked before generation")
	ErrAccountRestricted = errors.New("account is restricted")
	ErrInvalidRequest    = errors.New("invalid risk request")
)

type Request struct {
	UserID, ProjectID, TaskID, Feature, RequestKey string
	Source                                         string // Only user_input is screened; composed system prompts are not.
	Text                                           string `json:"-"`
}
type Event struct{ UserID, ProjectID, TaskID, Feature, RequestKey, Level, Keyword, Source, Action string }
type Decision struct {
	Action     string `json:"action"`
	Level      string `json:"risk_level,omitempty"`
	Count      int64  `json:"count"`
	Restricted bool   `json:"restricted"`
	Duplicate  bool   `json:"duplicate"`
}
type Backend interface {
	RiskConfig(context.Context) (Config, error)
	RiskUserAllowed(context.Context, string) error
	RecordRiskHit(context.Context, Event, int) (Decision, error)
}
type Service struct{ Backend Backend }

// Check must run before credit reservation and before any network submission.
// RequestKey must be stable across retries of the same user action.
func (s Service) Check(ctx context.Context, r Request) (Decision, error) {
	d := Decision{Action: "allowed"}
	if s.Backend == nil || r.UserID == "" || len(r.Text) > 1<<20 {
		return d, ErrInvalidRequest
	}
	if err := s.Backend.RiskUserAllowed(ctx, r.UserID); err != nil {
		return d, err
	}
	if r.Source != "user_input" {
		return d, nil
	}
	if len(r.RequestKey) < 8 || len(r.RequestKey) > 128 {
		return d, ErrInvalidRequest
	}
	c, err := s.Backend.RiskConfig(ctx)
	if err != nil {
		return d, err
	}
	if err = c.Validate(); err != nil {
		return d, err
	}
	if !c.Enabled {
		return d, nil
	}
	level, kw := FirstHit(c, r.Text)
	action := ""
	if contains(c.BlockLevels, level) {
		action = "blocked"
	} else if contains(c.WarnLevels, level) {
		action = "warned"
	}
	if action == "" {
		return d, nil
	}
	d, err = s.Backend.RecordRiskHit(ctx, Event{UserID: r.UserID, ProjectID: r.ProjectID, TaskID: r.TaskID, Feature: r.Feature, RequestKey: r.RequestKey, Level: level, Keyword: kw, Source: "user_input", Action: action}, c.BanThreshold)
	if err != nil {
		return d, err
	}
	if d.Restricted {
		return d, ErrAccountRestricted
	}
	if action == "blocked" {
		return d, ErrBlocked
	}
	return d, nil
}

// RecordProviderError records only a fixed classification. Provider bodies and
// prompts are never passed to the persistence layer or returned in an error.
func (s Service) RecordProviderError(ctx context.Context, r Request) (Decision, error) {
	d := Decision{Action: "allowed"}
	if s.Backend == nil || r.UserID == "" || r.TaskID == "" || len(r.Text) > 1<<20 {
		return d, ErrInvalidRequest
	}
	c, err := s.Backend.RiskConfig(ctx)
	if err != nil {
		return d, err
	}
	if err = c.Validate(); err != nil {
		return d, err
	}
	if !c.Enabled {
		return d, nil
	}
	for _, word := range []string{"risk control", "moderation", "keyword", "policy", "sensitive"} {
		if strings.Contains(strings.ToLower(r.Text), word) {
			return s.Backend.RecordRiskHit(ctx, Event{UserID: r.UserID, ProjectID: r.ProjectID, TaskID: r.TaskID, Feature: r.Feature, RequestKey: "provider:" + r.TaskID, Level: "medium", Keyword: word, Source: "provider_error", Action: "recorded"}, c.BanThreshold)
		}
	}
	return d, nil
}

func FirstHit(c Config, raw string) (string, string) {
	text := Normalize(raw)
	for _, level := range []string{"high", "medium", "low"} {
		if !contains(c.BlockLevels, level) && !contains(c.WarnLevels, level) {
			continue
		}
		for _, kw := range c.Keywords[level] {
			if matches(text, Normalize(kw)) {
				return level, kw
			}
		}
	}
	return "", ""
}
func contains(items []string, s string) bool {
	for _, v := range items {
		if v == s {
			return true
		}
	}
	return false
}
func asciiWord(r rune) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' }
func matches(text, kw string) bool {
	if kw == "" {
		return false
	}
	bounded := true
	for _, r := range kw {
		if r >= 128 || !(unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || r == '-' || r == '_') {
			bounded = false
		}
	}
	for offset := 0; offset < len(text); {
		i := strings.Index(text[offset:], kw)
		if i < 0 {
			return false
		}
		i += offset
		end := i + len(kw)
		offset = end
		if bounded {
			if i > 0 {
				r, _ := utf8.DecodeLastRuneInString(text[:i])
				if asciiWord(r) {
					continue
				}
			}
			if end < len(text) {
				r, _ := utf8.DecodeRuneInString(text[end:])
				if asciiWord(r) {
					continue
				}
			}
		}
		if !negative(text, i, end) {
			return true
		}
	}
	return false
}

type marker struct {
	value    string
	distance int
}

var negations = []marker{{"do not include", 120}, {"must not include", 120}, {"should not include", 120}, {"should not show", 120}, {"must not show", 120}, {"\u4e0d\u8981\u51fa\u73b0", 96}, {"\u4e0d\u5f97\u51fa\u73b0", 96}, {"\u4e0d\u80fd\u51fa\u73b0", 96}, {"\u4e0d\u53ef\u51fa\u73b0", 96}, {"\u7981\u6b62\u51fa\u73b0", 96}, {"\u4e25\u7981\u51fa\u73b0", 96}, {"\u4e0d\u8981\u5305\u542b", 96}, {"\u4e0d\u5f97\u5305\u542b", 96}, {"\u4e0d\u80fd\u5305\u542b", 96}, {"\u4e0d\u53ef\u5305\u542b", 96}, {"\u4e0d\u5e94\u5305\u542b", 96}, {"\u907f\u514d", 96}, {"\u89c4\u907f", 96}, {"\u675c\u7edd", 96}, {"\u6392\u9664", 96}, {"\u53bb\u9664", 96}, {"\u79fb\u9664", 96}, {"\u4e0d\u8981", 96}, {"\u4e0d\u5f97", 96}, {"\u7981\u6b62", 96}, {"\u4e25\u7981", 96}, {"without", 120}, {"avoid", 120}, {"exclude", 120}, {"no ", 80}, {"\u4e0d\u542b", 24}, {"\u65e0\u4efb\u4f55", 16}, {"\u65e0", 4}}

func negative(text string, start, end int) bool {
	after := strings.TrimSpace(text[end:])
	if len([]rune(after)) > 32 {
		after = string([]rune(after)[:32])
	}
	for _, m := range []string{"\u4e0d\u8981\u51fa\u73b0", "\u4e0d\u5f97\u51fa\u73b0", "\u4e0d\u80fd\u51fa\u73b0", "\u4e0d\u53ef\u51fa\u73b0", "\u7981\u6b62\u51fa\u73b0", "\u4e25\u7981\u51fa\u73b0", "\u4e0d\u5e94\u51fa\u73b0", "\u4e0d\u8981\u5305\u542b", "\u4e0d\u5f97\u5305\u542b", "\u9700\u907f\u514d", "\u5e94\u907f\u514d", "must not appear", "should not appear", "do not include", "must not include"} {
		if strings.HasPrefix(after, m) {
			return true
		}
	}
	prefix := text[:start]
	cut := 0
	for i, r := range prefix {
		if strings.ContainsRune("\r\n\u3002\uff01\uff1f!?;\uff1b", r) {
			cut = i + utf8.RuneLen(r)
		}
	}
	for _, b := range []string{"\u4f46\u662f", "\u4e0d\u8fc7", "\u7136\u800c", "\u53cd\u800c", "\u5176\u5b9e", "\u5b9e\u9645", "\u5374", "\u4f46", "but", "however", "instead", "yet"} {
		if i := strings.LastIndex(prefix, b); i >= 0 && i+len(b) > cut {
			cut = i + len(b)
		}
	}
	prefix = prefix[cut:]
	best := -1
	length := 0
	for _, m := range negations {
		if i := strings.LastIndex(prefix, m.value); i >= 0 && utf8.RuneCountInString(prefix[i+len(m.value):]) <= m.distance && (i > best || i == best && len(m.value) > length) {
			best = i
			length = len(m.value)
		}
	}
	if best < 0 {
		return false
	}
	before := prefix[:best]
	for _, v := range []string{"\u4e0d", "\u65e0\u9700", "\u4e0d\u7528", "\u4e0d\u8981", "not "} {
		if strings.HasSuffix(before, v) {
			return false
		}
	}
	between := prefix[best+length:]
	for _, v := range []string{"\u751f\u6210", "\u52a0\u5165", "\u5305\u542b", "\u5448\u73b0", "\u5c55\u793a", "\u8868\u73b0", "\u9700\u8981", "\u60f3\u8981", "\u5236\u4f5c", "\u753b\u51fa", "\u8425\u9020", "\u4f7f\u7528", "\u4fdd\u7559", "\u7a81\u51fa", "\u589e\u5f3a", "\u5e26\u6709", "generate", "include", "show", "depict", "add", "keep", "emphasize", "with", "\u592a", "\u8fc7\u4e8e", "\u8fc7\u5ea6", "\u975e\u5e38", "\u7a0d\u5fae", "\u4e00\u70b9", "\u5c11\u91cf", "\u8f7b\u5fae", "\u9002\u5ea6", "too", "slightly", "some"} {
		if strings.Contains(between, v) {
			return false
		}
	}
	for _, v := range []string{"\u8981\u7a81\u51fa", "\u9700\u7a81\u51fa", "\u8981\u52a0\u5165", "\u9700\u52a0\u5165", "\u8981\u5c55\u793a", "\u9700\u5c55\u793a", "\u8981\u5448\u73b0", "\u9700\u5448\u73b0", "\u8981\u589e\u5f3a", "\u9700\u589e\u5f3a", "should be shown", "must be shown", "include it", "show it", "emphasize it"} {
		if strings.Contains(after, v) {
			return false
		}
	}
	return true
}
