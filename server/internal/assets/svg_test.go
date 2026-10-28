package assets

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestSanitizeSVG(t *testing.T) {
	wrap := func(s string) string {
		return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` + s + `</svg>`
	}
	for _, s := range []string{
		wrap(`<path fill="#8b5cf6" d="M0 0 L24 24"/>`),
		wrap(strings.Repeat(`<g>`, 63) + strings.Repeat(`</g>`, 63)),
		wrap(`<g fill="none" stroke="currentColor" stroke-width="0" fill-rule="evenodd" clip-rule="nonzero" fill-opacity="0" stroke-opacity="1" stroke-linecap="round" stroke-linejoin="bevel" stroke-miterlimit="1"><rect width="0" height="0" rx="0" /></g>`),
		`<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" width="24px" height="24"><!--drop--><title>A &amp; B</title><desc>Logo</desc></svg>`,
		wrap(`<g transform="translate(1,2) scale(2) rotate(30 1 2) skewX(3) skewY(4) matrix(1 0 0 1 0 0)" opacity="0.5"><rect x="-1" y="2" width="3" height="4" rx="1"/><circle cx="1" cy="2" r="3"/><ellipse rx="1" ry="2"/><line x1="0" y1="0" x2="1" y2="1"/><polygon points="0,0 1,1 2,0"/><polyline points="0 0 1 1"/></g>`),
	} {
		t.Run(s[:min(len(s), 60)], func(t *testing.T) {
			clean, err := sanitizeSVG([]byte(s))
			require.NoError(t, err)
			again, err := sanitizeSVG(clean)
			require.NoError(t, err)
			require.Equal(t, clean, again)
			require.Contains(t, string(clean), `xmlns="http://www.w3.org/2000/svg"`)
			require.NotContains(t, string(clean), "<!--")
			var root struct{ XMLName xml.Name }
			require.NoError(t, xml.Unmarshal(clean, &root))
			require.Equal(t, "http://www.w3.org/2000/svg", root.XMLName.Space)
		})
	}
	bad := []string{
		"", `<svg/>`, `<svg xmlns="evil"/>`, wrap(`<script/>`), wrap(`<foreignObject/>`), wrap(`<image href="https://example.com"/>`), wrap(`<use/>`), wrap(`<a/>`), wrap(`<animate/>`), wrap(`<text/>`), wrap(`<defs/>`),
		wrap(`<path onload="alert(1)"/>`), wrap(`<path style="fill:red"/>`), wrap(`<path class="x"/>`), wrap(`<path id="x"/>`), wrap(`<path href="javascript:x"/>`), wrap(`<path href="data:x"/>`), wrap(`<path fill="url(https://example.com)"/>`), wrap(`<path fill="var(--x)"/>`),
		wrap(`<path xmlns="evil"/>`), wrap(`<x:path xmlns:x="evil"/>`), wrap(`<path xmlns:x="http://www.w3.org/2000/svg" x:fill="black"/>`), wrap(`<path fill="black" fill="white"/>`), wrap(`<path bogus="1"/>`), wrap(`<path width="1"/>`),
		wrap(`<path d="M NaN 0"/>`), wrap(`<path d="M1e999 0"/>`), wrap(`<polygon points="1 2 3"/>`), wrap(`<circle r="-1"/>`), wrap(`<circle r="0"/>`), wrap(`<rect width="-1"/>`), wrap(`<g opacity="2"/>`), wrap(`<g transform="translate(1 2 3)"/>`), wrap(`<g transform="url(x)"/>`), wrap(`<g stroke-linecap="evil"/>`),
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 0 1"/>`, `<svg xmlns="http://www.w3.org/2000/svg" width="0"/>`,
		wrap(`text`), "\u00a0" + wrap(``), wrap(`<path xmlns="http://www.w3.org/2000/svg" xmlns="http://www.w3.org/2000/svg"/>`), wrap(`<path>text</path>`), wrap(`<svg/>`), wrap(`<path>`), wrap(`&unknown;`), wrap(`<?evil x?>`), `<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///etc/passwd">]>` + wrap(`&x;`),
		`<?xml version="1.1"?>` + wrap(``), `<?xml version="1.0" encoding="ISO-8859-1"?>` + wrap(``), wrap(``) + wrap(``), `x` + wrap(``), wrap(``) + `x`,
		wrap(strings.Repeat(`<g>`, 64) + strings.Repeat(`</g>`, 64)), wrap(strings.Repeat(`<path/>`, 50_000)), strings.Repeat(" ", MaxFileSizeImage+1), wrap(`<title>` + strings.Repeat(`>`, MaxFileSizeImage/2) + `</title>`),
	}
	for i, s := range bad {
		t.Run(fmt.Sprintf("reject_%02d", i), func(t *testing.T) { out, err := sanitizeSVG([]byte(s)); require.Error(t, err); require.Nil(t, out) })
	}
	clean, err := sanitizeSVG([]byte(wrap(`<path fill="#8b5cf6" d="M0 0 L24 24"/>`)))
	require.NoError(t, err)
	require.Contains(t, string(clean), `d="M0 0 L24 24"`)
}

func FuzzSanitizeSVG(f *testing.F) {
	for _, s := range []string{`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0 L1 1"/></svg>`, `<svg/>`, `<!DOCTYPE svg>`, `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := sanitizeSVG(data)
		if err != nil {
			if out != nil {
				t.Fatal("output on error")
			}
			return
		}
		if len(out) > MaxFileSizeImage {
			t.Fatal("unbounded output")
		}
		again, err := sanitizeSVG(out)
		if err != nil || !bytes.Equal(out, again) {
			t.Fatalf("unstable serialization: %v", err)
		}
	})
}
