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
	t.Parallel()
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
			t.Parallel()
			clean, err := sanitizeSVG([]byte(s))
			require.NoError(t, err)
			again, err := sanitizeSVG(clean)
			require.NoError(t, err)
			require.Equal(t, clean, again)
			require.Contains(t, string(clean), `xmlns="http://www.w3.org/2000/svg"`)
			require.NotContains(t, string(clean), "<!--")
			for _, attribute := range []string{`width="3"`, `r="3"`, `fill="none"`, `stroke="currentColor"`, `opacity="0.5"`, `transform="translate(1,2) scale(2) rotate(30 1 2) skewX(3) skewY(4) matrix(1 0 0 1 0 0)"`} {
				if strings.Contains(s, attribute) {
					require.Contains(t, string(clean), attribute)
				}
			}
			var root struct {
				XMLName xml.Name `xml:"svg"`
			}
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
		t.Run(fmt.Sprintf("reject_%02d", i), func(t *testing.T) {
			t.Parallel()
			out, err := sanitizeSVG([]byte(s))
			require.Error(t, err)
			require.Nil(t, out)
		})
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

func TestSanitizeSVGClipping(t *testing.T) {
	t.Parallel()
	wrap := func(s string) string { return `<svg xmlns="http://www.w3.org/2000/svg">` + s + `</svg>` }
	definition := `<defs><clipPath id="clip0_12_34"><rect width="12" height="8" fill="white"/></clipPath></defs>`
	group := `<g clip-path="url(#clip0_12_34)"><path d="M0 0 L12 8"/></g>`
	for _, body := range []string{group + definition, definition + group, `<path d="M0 0 L12 8" clip-path="url(#clip0_12_34)"/>` + definition} {
		clean, err := sanitizeSVG([]byte(wrap(body)))
		require.NoError(t, err)
		require.Contains(t, string(clean), `clip-path="url(#clip0_12_34)"`)
		again, err := sanitizeSVG(clean)
		require.NoError(t, err)
		require.Equal(t, clean, again)
	}
	bad := []string{
		group, group + definition + definition,
		`<path id="clip0_12_34"/>` + group,
		`<clipPath id="clip0_12_34"><rect width="12"/></clipPath>` + group,
		`<g>` + definition + `</g>`, `<defs><path/></defs>`,
		`<defs><clipPath><rect/></clipPath></defs>`,
		`<defs><clipPath id="x"/></defs>`,
		`<defs><clipPath id="x"><g><rect/></g></clipPath></defs>`,
		`<defs><clipPath id="x"><rect><path/></rect></clipPath></defs>`,
		`<defs><clipPath id="x"><use href="#x"/></clipPath></defs>`,
		`<defs><clipPath id="x"><script/></clipPath></defs>`,
		`<defs><clipPath id="x" clip-path="url(#x)"><rect/></clipPath></defs>`,
		`<defs><clipPath id="x"><rect clip-path="url(#x)"/></clipPath></defs>`,
		`<defs><clipPath id="x"><rect clip-path="url(#y)"/></clipPath><clipPath id="y"><rect clip-path="url(#x)"/></clipPath></defs>`,
		`<defs clip-path="url(#clip0_12_34)">` + definition + `</defs>`,
	}
	for _, ref := range []string{"url(https://example.com/a.svg#x)", "url(//example.com/a#x)", "url(data:image/svg+xml,x)", "url(javascript:x)", "url(#missing)", "url('#clip0_12_34')", `u\72l(#clip0_12_34)`, `url(#clip0_12_\33 4)`, "url(/**/#clip0_12_34)", "url(%23clip0_12_34)", "URL(#clip0_12_34)"} {
		bad = append(bad, `<g clip-path="`+ref+`"/>`+definition)
	}
	for i, body := range bad {
		t.Run(fmt.Sprintf("reject_clip_%02d", i), func(t *testing.T) {
			t.Parallel()
			clean, err := sanitizeSVG([]byte(wrap(body)))
			require.Error(t, err)
			require.Nil(t, clean)
		})
	}
}

func TestCanonicalPlatformImageSVGGuidance(t *testing.T) {
	t.Parallel()
	_, _, err := canonicalPlatformImage([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>private-input</script></svg>`), "image/svg+xml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "static SVG")
	require.Contains(t, err.Error(), "local clip paths")
	require.NotContains(t, err.Error(), "private-input")
	require.Less(t, len(err.Error()), 400)
}

func TestSVGPathGrammarAndGeometryBudget(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"M0 0", "m.5-.5 1 2z", "M0 0H1V2L3 4C1 2 3 4 5 6S1 2 3 4Q1 2 3 4T5 6Z", "M0 0a2 3 45 0110-5", "M0 0A2 3 0 1 0 4 5 2 3 0 0 1 6 7"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			_, err := sanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="` + path + `"/></svg>`))
			require.NoError(t, err)
		})
	}
	for _, path := range []string{"M", "M0", "M0 0L", "M0 0L1", "M0 0C1 2 3 4 5", "M0 0Q1 2 3", "M0 0Z1 2", "M0 0A1 2 3 2 0 4 5", "M0 0A-1 2 3 0 0 4 5", "M0 0A1 2 3 0 0 4", "M,0 0", "M0,,0", "M0 0,", "M0 0,L1 2", "M0 0Z,"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			_, err := sanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><path d="` + path + `"/></svg>`))
			require.Error(t, err)
		})
	}
	for name, body := range map[string]string{
		"path coordinates": `<path d="M0 0` + strings.Repeat(` 1 1`, 10000) + `"/>`,
		"close commands":   `<path d="M0 0` + strings.Repeat(`Z`, 10000) + `"/>`,
		"points":           `<polyline points="` + strings.Repeat(`1 1 `, 10000) + `"/>`,
		"across paths":     strings.Repeat(`<path d="M0 0L1 1"/>`, 2000),
		"across shapes":    strings.Repeat(`<rect width="1" height="1"/>`, 10001),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := sanitizeSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg">` + body + `</svg>`))
			require.Error(t, err)
		})
	}
}

func TestSVGPathBudgetBoundary(t *testing.T) {
	t.Parallel()
	for _, size := range []int{2, 3, 4} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Parallel()
			budget := size
			require.Equal(t, size >= 3, validSVGPath("M0 0", &budget))
		})
	}
}
