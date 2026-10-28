package commerce

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/suakitsu/picpilot-platform/core/creative"
)

func TestCatalogDefaultsSelectionAndValidation(t *testing.T) {
	c := DefaultCatalog()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(c.Platforms) != 8 || len(c.Slots) != 16 || len(c.Templates) != 52 {
		t.Fatal("legacy presets missing")
	}
	_, templates, e, err := c.Select(EstimateRequest{Platform: "tiktok_shop", Resolution: "2k", SelectedSlots: []string{"main_image", "main_image", "not-a-slot", "sku_display"}}, false)
	if err != nil || len(templates) != 2 || e.CreditsCost != 48 || e.Resolution != "2K" {
		t.Fatal("selection/estimate mismatch", err, e)
	}
	_, _, e, err = c.Select(EstimateRequest{Platform: "tiktok_shop"}, true)
	if err != nil || e.CreditsCost != 0 || e.ImageCount != 7 {
		t.Fatal("personal estimate", err)
	}
	c.Templates[1].ProviderParams["api_key"] = "fixture-only"
	if c.Validate() == nil {
		t.Fatal("credentials/unknown provider parameters accepted")
	}
}

func TestProductVisibleCopyAndLegacyAliases(t *testing.T) {
	var in GenerateRequest
	if err := Decode([]byte(`{"platform":"jd","productInfo":{"productName":"Fixture","sellingPoints":["1. Good","2) Good","1.5L","- Clear"]},"selectedSlots":["main_image"],"referenceImageIds":["asset_fixture"]}`), &in); err != nil {
		t.Fatal(err)
	}
	if err := in.Product.Normalize(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(in.Product.SellingPoints, "|"); got != "Good|1.5L|Clear" {
		t.Fatal(got)
	}
	if Decode([]byte(`{"product_info":{},"productInfo":{}}`), &in) == nil {
		t.Fatal("ambiguous aliases accepted")
	}
	if Decode([]byte(`{"platform":"jd","unknown":true}`), &in) == nil {
		t.Fatal("unknown input silently discarded")
	}
}

func TestCommercePlanPreservesGalleryAndVisibleTextPolicy(t *testing.T) {
	c := DefaultCatalog()
	product := Product{ProductName: "Fixture", PriceText: "19.50", SellingPoints: []string{"1. Durable", "2. Confirmed"}, Country: "US", Language: "en-US"}
	for _, platform := range []string{"tiktok_shop", "pinduoduo"} {
		p, templates, e, err := c.Select(EstimateRequest{Platform: platform}, false)
		if err != nil {
			t.Fatal(err)
		}
		// Runtime safety guidance must survive a legacy customized template.
		templates[0].PromptTemplate = "Fixture custom template {{product_name}}"
		images, err := c.Build(p, templates, e, GenerateRequest{Product: product}, []string{"asset_owned"})
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range images {
			if !strings.Contains(im.Payload.Image.Prompt, "Runtime platform suite quality guidance") || !strings.Contains(im.Payload.Image.Prompt, "same single front/back artwork count and placement") {
				t.Fatal("missing runtime safeguards")
			}
			plan := im.Payload.Snapshot["visibleTextPlan"].(map[string]any)
			layer := plan["text_layer"].(map[string]any)
			if (platform == "tiktok_shop" || im.Slot != "main_image" && im.Slot != "promo") && layer["price_text"] != "" {
				t.Fatal("price leaked into unrelated slot")
			}
			if platform == "tiktok_shop" && (im.Slot == "main_image" || im.Slot == "model_image" || im.Slot == "scenario" || im.Slot == "sku_display") && (layer["title"] != "" || layer["copy_density"] != "none") {
				t.Fatal("gallery became text poster")
			}
			if len(im.Payload.Image.ReferenceAssetIDs) != 1 {
				t.Fatal("references lost")
			}
		}
	}
}

func TestTranslationOwnedSourcePlanAndSizes(t *testing.T) {
	in := TranslationRequest{TargetLanguage: "MS-my", Resolution: "2k", Style: "natural"}
	language, err := in.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	im, err := TranslationImage(in, language, "asset_source", "portrait.png", 720, 1280, 2, 24)
	if err != nil {
		t.Fatal(err)
	}
	if im.Payload.Image.Size != "1152x2048" || len(im.Payload.Image.ReferenceAssetIDs) != 1 || im.Payload.Image.ReferenceAssetIDs[0] != "asset_source" {
		t.Fatal("wrong source or aspect")
	}
	if im.Payload.Snapshot["sourceImageIndex"] != 2 || im.Payload.Snapshot["sourceWidth"] != 720 {
		t.Fatal("source metadata lost")
	}
	for _, text := range []string{"Malaysian Malay", "Only replace existing text pixels", "visually identical", "proper names", "never guess", "original aspect ratio"} {
		if !strings.Contains(im.Payload.Image.Prompt, text) {
			t.Fatal("missing translation policy", text)
		}
	}
	if len(Languages()) != 14 {
		t.Fatal("language list incomplete")
	}
	for _, size := range sizes {
		for i, r := range []string{"1K", "2K", "4K"} {
			got, err := Size(size[0], r)
			if err != nil || got != size[i] {
				t.Fatal("size mapping", got, err)
			}
		}
	}
	var roundtrip Payload
	if json.Unmarshal(creative.JSON(im.Payload), &roundtrip) != nil || roundtrip.Validate("image") != nil {
		t.Fatal("payload not common runner compatible")
	}
}

func TestCommerceVisiblePlanSlotSpecificCopyAndOptions(t *testing.T) {
	c := DefaultCatalog()
	product := Product{ProductName: "Fixture", Material: "Steel", Specs: "500 ml", Audience: "Commuters", Scenario: "Daily travel", SellingPoints: []string{"1. Easy to carry", "2. Verified feature"}}
	p, templates, e, err := c.Select(EstimateRequest{Platform: "jd", SelectedSlots: []string{"white_background", "detail", "specification"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	images, err := c.Build(p, templates, e, GenerateRequest{Product: product, OverlayOptions: map[string]any{"color": "#123456"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, im := range images {
		plan := im.Payload.Snapshot["visibleTextPlan"].(map[string]any)
		layer := plan["text_layer"].(map[string]any)
		if layer["color"] != "#123456" || layer["safe_margin_percent"] != 8 {
			t.Fatal("visible style or edge policy ignored")
		}
		switch im.Slot {
		case "white_background":
			if plan["enabled"] != false || layer["title"] != "" || layer["subtitle"] != "" || len(layer["selling_points"].([]string)) != 0 {
				t.Fatal("white image has text")
			}
		case "detail":
			if layer["title"] != "Steel" || layer["subtitle"] != "500 ml" || layer["layout"] != "macro_detail_labels" {
				t.Fatal("detail uses generic product headline")
			}
		case "specification":
			if layer["title"] != "500 ml" || layer["layout"] != "spec_grid_or_measure_lines" {
				t.Fatal("specification plan lost supplied measurements")
			}
		}
	}
}
