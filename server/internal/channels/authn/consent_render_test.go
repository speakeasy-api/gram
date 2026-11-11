package authn

import (
	"bytes"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/consentpage"
)

func TestConsentTemplateRendering(t *testing.T) {
	// Exercise both text and attribute contexts with untrusted client and identity data.
	untrusted := `<script>alert("consent")</script>&'`
	data := map[string]any{"Styles": consentpage.Styles}
	for _, field := range []string{"Name", "ClientID", "Organization", "Project", "User", "Resource", "Challenge", "CSRF"} {
		data[field] = field + untrusted
	}
	var out bytes.Buffer
	if err := consentTemplate.Execute(&out, data); err != nil {
		t.Fatal(err)
	}
	page := out.String()
	if strings.Contains(page, "<script>") || strings.Contains(page, "ZgotmplZ") {
		t.Fatal("untrusted data was not safely rendered")
	}
	for _, field := range []string{"Name", "ClientID", "Organization", "Project", "User"} {
		if !strings.Contains(page, html.EscapeString(data[field].(string))) {
			t.Errorf("missing escaped %s", field)
		}
	}
	for _, field := range []string{"Resource", "Challenge", "CSRF"} {
		want := `type="hidden" name="` + strings.ToLower(field) + `" value="` + html.EscapeString(data[field].(string)) + `"`
		if !strings.Contains(page, want) {
			t.Errorf("missing escaped hidden field %s", field)
		}
	}
	for _, want := range []string{
		`<html lang="en">`, `<meta charset="UTF-8"`,
		`aria-labelledby="page-title"`, `id="page-title"`,
		`<dl `, `<dt class="text-eyebrow">Organization</dt>`,
		`<dt class="text-eyebrow">Project</dt>`, `<dt class="text-eyebrow">Signed in as</dt>`,
		`<form method="post" action="/channels/oauth/authorize"`,
		`type="submit" name="decision" value="allow"`,
		`type="submit" name="decision" value="deny"`,
		`>Allow</button>`, `>Deny</button>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing consent markup %q", want)
		}
	}
	var logo bytes.Buffer
	if err := consentTemplate.ExecuteTemplate(&logo, "speakeasyWordmark", nil); err != nil {
		t.Fatal(err)
	}
	if logo.Len() == 0 || !strings.Contains(page, logo.String()) {
		t.Error("missing shared Speakeasy wordmark")
	}
	if !strings.Contains(page, string(consentpage.Styles)) {
		t.Error("missing shared consent stylesheet")
	}
}

func TestConsentTemplateUtilitiesCompiled(t *testing.T) {
	// This page reuses existing utilities, so no generated stylesheet change is needed.
	escape := strings.NewReplacer(":", `\:`, "[", `\[`, "]", `\]`)
	for _, match := range regexp.MustCompile(`class="([^"]+)"`).FindAllStringSubmatch(consentHTML, -1) {
		for class := range strings.FieldsSeq(match[1]) {
			if !strings.Contains(string(consentpage.Styles), "."+escape.Replace(class)) {
				t.Errorf("class %q is missing from the shared stylesheet", class)
			}
		}
	}
}
