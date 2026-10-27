package video

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

var singleShot = regexp.MustCompile("(?i)(\u4e00\u955c\u5230\u5e95|\u5355\u955c\u5934|\u8fde\u7eed\u955c\u5934|\u4e0d\u8981\u5207\u955c|\u4e0d\u5207\u955c|no\\s+cuts?|single\\s+shot|continuous\\s+shot)")
var storyboard = regexp.MustCompile("(?i)(\u5206\u955c|\u955c\u5934\\s*[\u4e00\u4e8c\u4e09\u56db\u4e94\u516d\u4e03\u516b\u4e5d\u5341\\d]+|\u5207\u955c|\u8f6c\u573a|\u591a\u573a\u666f|\u5e7f\u544a\u7247|\u5ba3\u4f20\u7247|\u5e26\u8d27|\\d+\\s*[-~\u81f3\u2013\u2014]\\s*\\d+\\s*(?:s|\u79d2)|shot\\s*\\d+|scene\\s*\\d+)")

const continuity = `Product continuity requirements: keep the exact same referenced product as the subject in every frame. Preserve its identity,
silhouette, geometry, proportions, materials, colors, logo, labels, printed text, small details, accessories and object count.
All product details must remain temporally coherent from the first frame to the last frame.
Avoid accidental text, misspellings, watermarks, QR codes, unrelated logos and unsupported product claims.`

const continuousMotion = "Use one continuous shot with physically plausible motion. No cuts, no scene replacement, no viewpoint jump and no object transformation."
const storyboardMotion = `Follow the planned shot order and timing in the script. Cuts and transitions are allowed only where the script requests them.
Across every planned shot, keep the same product identity and use coherent camera direction, lighting and physical motion.`

const agnesContinuity = `

Product continuity requirements: keep the exact same product as the only subject in every frame. Preserve its identity,
silhouette, geometry, proportions, materials, colors, logo, labels, printed text, small details, accessories and object count.
Use one continuous shot with physically plausible motion. No cuts, no scene replacement, no viewpoint jump and no object transformation.
All product details must remain temporally coherent from the first frame to the last frame.
`

const agnesNegativePrompt = "product morphing, identity change, shape change, geometry change, proportion change, color shift, " +
	"material change, logo change, label change, text change, misspelled text, extra text, duplicated product, " +
	"missing parts, extra parts, object replacement, object count change, accessory count change, packaging change, " +
	"background replacement, scene cut, shot transition, camera jump, sudden zoom, flicker, jitter, frame inconsistency, " +
	"temporal inconsistency, warping, melting, stretching, deformation, disassembly, floating parts, impossible motion, " +
	"blur, low detail, watermark, subtitles"

func compilePrompt(prompt string, provider Provider, referenceCount int) string {
	motion := continuousMotion
	if !singleShot.MatchString(prompt) && storyboard.MatchString(prompt) {
		motion = storyboardMotion
	}
	suffix := "\n\n" + continuity + "\n" + motion
	if referenceCount > 0 {
		names := make([]string, referenceCount)
		for i := range names {
			prefix := "\u56fe\u7247"
			if provider == HFSY {
				prefix = "@\u56fe"
			}
			names[i] = prefix + strconv.Itoa(i+1)
		}
		suffix += "\n\u53c2\u8003\u7d20\u6750\uff1a" + strings.Join(names, "\u3001") +
			" \u6309\u4e0a\u4f20\u987a\u5e8f\u5bf9\u5e94\uff1b\u628a\u5b83\u4eec\u4f5c\u4e3a\u5546\u54c1\u8eab\u4efd\u3001\u7ed3\u6784\u3001\u989c\u8272\u548c\u6750\u8d28\u7684\u4e8b\u5b9e\u6765\u6e90\u3002"
	}
	limit := 4000
	if provider == HFSY {
		limit = 5000
	}
	available := limit - len(utf16.Encode([]rune(suffix)))
	// Match Java's UTF-16 length budget without cutting a surrogate pair.
	used, end := 0, len(prompt)
	for i, r := range prompt {
		used++
		if r > 0xffff {
			used++
		}
		if used > available {
			end = i
			break
		}
	}
	return strings.TrimSpace(prompt[:end]) + suffix
}
