package creative

import (
	"regexp"
	"strings"
)

var ignoreReferencesEN = regexp.MustCompile(`(?i)\b(?:do not|don't|never)\s+(?:use|reference|consider)\b.{0,30}\b(?:uploaded\s+)?(?:images?|photos?|references?|assets?)\b|\b(?:images?|photos?|assets?)\b.{0,30}\b(?:are not references|for display only|should be ignored)\b`)
var ignoreReferencesZH = regexp.MustCompile("(?:\u4e0d\u7528|\u4e0d\u9700\u8981|\u65e0\u9700|\u4e0d\u8981\u4f7f\u7528|\u4e0d\u4f7f\u7528|\u4e0d\u53c2\u8003|\u4e0d\u8981\u53c2\u8003|\u5ffd\u7565\u6240\u6709|\u5ffd\u7565\u5168\u90e8)[^\u3002\uff1b;\n]{0,12}(?:\u53c2\u8003)?(?:\u56fe|\u56fe\u7247|\u7167\u7247|\u7d20\u6750)")

func CleanPrompt(prompt string) string {
	const marker = "Intent and reference constraints:"
	i := strings.LastIndex(prompt, marker)
	if i < 0 {
		return strings.TrimSpace(prompt)
	}
	suffix := prompt[i:]
	for _, evidence := range []string{"Reference image is the source of truth.", "Keep the product from the reference image unchanged.", "Use the reference image as the source of truth for the main subject.", "Create an ecommerce commercial layout, but the referenced product remains the main subject", "Visible text is allowed only when explicitly provided by the user.", "Do not render readable text, logos, watermarks, labels, QR codes, or platform UI unless explicitly requested."} {
		if strings.Contains(suffix, evidence) {
			return strings.TrimSpace(prompt[:i])
		}
	}
	return strings.TrimSpace(prompt)
}
func IgnoreReferences(in ImageRequest) bool {
	text := CleanPrompt(in.Prompt + "\n" + in.Message)
	if in.ReferencePolicy == "none" || ignoreReferencesEN.MatchString(text) {
		return true
	}
	for _, match := range ignoreReferencesZH.FindAllStringIndex(text, -1) {
		suffix := text[match[1]:]
		partial := false
		for _, word := range []string{"\u4e2d", "\u4e0a", "\u91cc", "\u5185", "\u7684"} {
			if strings.HasPrefix(suffix, word) {
				partial = true
			}
		}
		if !partial {
			return true
		}
	}
	return false
}
func ImagePolicy(in ImageRequest) string {
	policy := "Follow the user's role for every specifically named asset. Explicit subject replacement overrides general identity preservation: replace only the designated subject, using the designated product reference, while preserving the requested composition. Do not preserve the old object when the user explicitly replaces it. Preserve existing packaging text, artwork, labels and logos unless the user requests changes. Add no new visible text unless explicitly requested, and then use the exact supplied wording."
	if IgnoreReferences(in) {
		return "Ignore uploaded images and execute the text request only. Add no new visible text unless explicitly requested."
	}
	if in.ReferencePolicy == "style_reference" {
		policy += " References supply style only; do not copy their product identity unless explicitly requested."
	} else {
		policy += " Preserve the designated product identity, geometry, materials and color, subject to explicit replacement instructions."
	}
	if in.TextMode == "no_text" {
		policy += " Add no new headline, slogan, promotional copy or watermark. Existing product/package labels are not new poster text."
	}
	return policy
}
