package creative

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLegacyAliasesAndConflictingFields(t *testing.T) {
	var in CreateProject
	if err := DecodeAliases([]byte(`{"productName":"Cup","includeAplus":true,"aplus_size":"970x600","title_keywords":["cup"]}`), &in); err != nil || in.ProductName != "Cup" || in.IncludeAplus == nil || !*in.IncludeAplus {
		t.Fatal("legacy contract rejected", err)
	}
	for _, body := range []string{`{"productName":"A","product_name":"B"}`, `{"product_name":"A","credits":99}`, `null`, `{"product_name":"A"} {}`} {
		if DecodeAliases([]byte(body), &in) == nil {
			t.Fatal("ambiguous or unknown input accepted")
		}
	}
}
func TestListingValidationAndFactBoundaries(t *testing.T) {
	facts := Facts("Cup", "", "US", "en-US", map[string]any{"material": "steel", "dimensions": ""}, map[string]any{"color_hints": []string{"green"}, "material": "confirmed steel"}, true)
	if facts["material"] != "confirmed steel" || len(facts["color_hints"].([]string)) != 0 {
		t.Fatal("facts/color precedence lost")
	}
	valid := `{"title":"Cup","selling_point":["One","Two","Three","Four","Five"],"product_description":{"Material":"steel"}}`
	copy, err := ListingCopy(valid, facts)
	if err != nil || len(copy["image_briefs"].([]any)) != 7 {
		t.Fatal("copy schema rejected", err)
	}
	for _, value := range []string{`not JSON`, `{}`, `{"title":"x","bullets":["one"],"description":"x"}`, strings.Replace(valid, "Cup", strings.Repeat("a", MaxField+1), 1)} {
		if _, err := ListingCopy(value, facts); err == nil {
			t.Fatal("invalid copy accepted")
		}
	}
	prompt, err := ImagePrompt("dimensions", ImageRequest{Prompt: "Keep the full product", Size: "970x600"}, facts, nil, nil)
	if err != nil || !strings.Contains(prompt, "970x600") || !strings.Contains(prompt, "no center cropping") || !strings.Contains(prompt, "only confirmed dimensions") {
		t.Fatal("exact canvas or fact constraints missing")
	}
}
func TestStructuredVisionAndFieldCeilings(t *testing.T) {
	input := Text("Only known facts", map[string]string{"product": "Fixture"}, true)
	AddVision(&input, []string{"asset_b", "asset_a", "asset_c", "asset_d", "asset_e"})
	raw := string(JSON(input))
	if strings.Contains(raw, "base64") || strings.Count(raw, "picpilot-asset:") != 4 || strings.Index(raw, "asset_b") > strings.Index(raw, "asset_a") {
		t.Fatal("vision placeholders or ordering incorrect")
	}
	if ValidateFields(map[string]any{"a": []any{map[string]string{"long": strings.Repeat("x", MaxField)}}}) != nil {
		t.Fatal("limit boundary rejected")
	}
	if ValidateFields(map[string]string{"long": strings.Repeat("x", MaxField+1)}) == nil {
		t.Fatal("oversize field accepted")
	}
}

func TestReferencePolicyAndLegacyPromptPollution(t *testing.T) {
	polluted := "Replace the subject in reference image 1 with image 2.\nIntent and reference constraints:\nReference image is the source of truth. Do not replace the product."
	prompt, err := ImagePrompt("creative", ImageRequest{Prompt: polluted, Size: "1024x1024", Intent: "reference_replace", ReferencePolicy: "preserve", TextMode: "no_text"}, nil, nil, nil)
	if err != nil || strings.Contains(prompt, "Intent and reference constraints:") || strings.Contains(prompt, "Do not replace the product") || !strings.Contains(prompt, "Explicit subject replacement overrides") {
		t.Fatal("replacement request contradicted by stale policy", err)
	}
	if !IgnoreReferences(ImageRequest{Prompt: "Do not use uploaded images; draw from text only."}) || !IgnoreReferences(ImageRequest{ReferencePolicy: "none"}) {
		t.Fatal("text-only reference policy ignored")
	}
	prompt, err = ImagePrompt("creative", ImageRequest{Prompt: "Keep existing package text", TextMode: "no_text"}, nil, nil, nil)
	if err != nil || !strings.Contains(prompt, "Existing product/package labels are not new poster text") {
		t.Fatal("existing label preservation lost")
	}
}
func TestAssistantNavigationAndContentOnly(t *testing.T) {
	base := map[string]any{"reply": "Here is the prompt.", "intent": "image", "action": "open_image_agent", "action_href": "https://evil.invalid", "action_label": "Open", "suggested_prompt": "A cup", "confidence": 9}
	for _, latest := range []string{"Only the prompt, do not open anything", "\u76f4\u63a5\u7ed9\u6211\u63d0\u793a\u8bcd\uff0c\u4e0d\u8981\u6253\u5f00"} {
		out, err := AssistantResult(string(JSON(base)), latest, "configured")
		if err != nil || out["action"] != "none" || out["action_href"] != "" || out["confidence"] != float64(1) {
			t.Fatal("direct content navigates", out, err)
		}
	}
	out, err := AssistantResult(string(JSON(base)), "Open image agent", "configured")
	if err != nil || out["action_href"] != "/image-agent" || out["suggested_prompt"] != "" {
		t.Fatal("pure navigation polluted", out, err)
	}
	if _, err := AssistantResult(`{"reply":""}`, "hello", ""); err == nil {
		t.Fatal("fake success accepted")
	}
	out, err = AssistantResult(string(JSON(base)), "Open image agent for a cup", "configured")
	if err != nil || !strings.HasPrefix(out["action_href"].(string), "/image-agent?prompt=") {
		t.Fatal("unsafe or missing local handoff")
	}
	if _, err = json.Marshal(out); err != nil {
		t.Fatal(err)
	}
}
