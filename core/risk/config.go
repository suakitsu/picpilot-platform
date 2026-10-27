// Package risk evaluates user-authored input without any provider dependency.
package risk

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

type Config struct {
	Enabled      bool                `json:"enabled"`
	BanThreshold int                 `json:"ban_threshold"`
	BlockLevels  []string            `json:"block_levels"`
	WarnLevels   []string            `json:"warn_levels"`
	Keywords     map[string][]string `json:"keywords"`
}

const defaults = `{"enabled":true,"ban_threshold":5,"block_levels":["high","medium"],"warn_levels":["low"],"keywords":{"high":["\u8272\u60c5","\u6210\u4eba\u5185\u5bb9","\u88f8\u9732","\u5168\u88f8","\u88f8\u4f53","\u9732\u70b9","\u6027\u7231","\u6027\u4ea4","\u6027\u884c\u4e3a","\u63f4\u4ea4","\u5356\u6deb","\u672a\u6210\u5e74\u88f8\u9732","\u513f\u7ae5\u8272\u60c5","\u5e7c\u5973\u88f8","\u841d\u8389\u88f8","\u8840\u8165","\u80a2\u89e3","\u5c38\u4f53","\u81ea\u6b8b","\u81ea\u6740","\u8650\u5f85","\u6bd2\u54c1","\u5438\u6bd2","\u8d29\u6bd2","\u5927\u9ebb","\u51b0\u6bd2","\u6d77\u6d1b\u56e0","\u53ef\u5361\u56e0","\u67aa\u652f","\u624b\u67aa","\u6b65\u67aa","\u70b8\u5f39","\u7206\u70b8\u7269","\u5236\u7206","\u6740\u4eba","\u8eab\u4efd\u8bc1","\u62a4\u7167","\u94f6\u884c\u5361","\u4fe1\u7528\u5361","\u9a8c\u8bc1\u7801","\u5bc6\u7801","\u79c1\u94a5","\u7eb3\u7cb9","\u6050\u6016\u4e3b\u4e49","\u6781\u7aef\u7ec4\u7ec7"],"medium":["\u4eff\u724c","\u9ad8\u4eff","\u590d\u523b","\u539f\u5355","\u5c3e\u5355","\u8386\u7530","\u5c71\u5be8","\u5047\u5192","\u4fb5\u6743","logo\u590d\u523b","\u5546\u6807\u590d\u523b","\u5927\u724c\u540c\u6b3e","\u660e\u661f\u771f\u4eba","\u540d\u4eba\u8138","\u7f51\u7ea2\u771f\u4eba","\u6362\u8138","AI\u6362\u8138","deepfake","\u8fea\u58eb\u5c3c","\u6f2b\u5a01","\u5b9d\u53ef\u68a6","\u76ae\u5361\u4e18","Hello Kitty","\u5965\u7279\u66fc","Nike","Adidas","Gucci","LV","Chanel","Dior","Prada","Hermes","\u653f\u6cbb\u654f\u611f","\u9886\u5bfc\u4eba","\u5b97\u6559\u653b\u51fb","\u533b\u7597\u529f\u6548","\u6cbb\u6108","\u6839\u6cbb","\u51cf\u80a5\u795e\u836f","\u58ee\u9633"],"low":["\u6027\u611f","\u6cf3\u88c5","\u5185\u8863","\u6bd4\u57fa\u5c3c","\u513f\u7ae5","\u5c0f\u5b69","\u5b9d\u5b9d","\u5200","\u519b\u4e8b","\u6218\u4e89","\u54c1\u724c\u540c\u6b3e","\u5927\u724c\u98ce","\u5962\u4f88\u54c1\u98ce"]}}`

func DefaultConfig() Config { var c Config; _ = json.Unmarshal([]byte(defaults), &c); return c }

func (c Config) Validate() error {
	invalid := errors.New("invalid risk configuration")
	if c.BanThreshold < 1 || c.BanThreshold > 100 {
		return invalid
	}
	seen := map[string]bool{}
	for _, levels := range [][]string{c.BlockLevels, c.WarnLevels} {
		for _, level := range levels {
			if !ValidLevel(level) || seen[level] {
				return invalid
			}
			seen[level] = true
		}
	}
	if len(c.Keywords) != 3 {
		return invalid
	}
	for level, keywords := range c.Keywords {
		if !ValidLevel(level) || len(keywords) > 2000 {
			return invalid
		}
		unique := map[string]bool{}
		for _, kw := range keywords {
			key := Normalize(kw)
			if key == "" || utf8.RuneCountInString(kw) > 120 || strings.ContainsRune(kw, 0) || unique[key] {
				return invalid
			}
			unique[key] = true
		}
	}
	return nil
}

func ValidLevel(s string) bool  { return s == "high" || s == "medium" || s == "low" }
func Normalize(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }
