package oinmanifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// RenderJSON serialises the manifest with stable key order and indentation so
// two exports of the same catalog are byte-identical.
func RenderJSON(manifest Manifest) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	return buf.Bytes(), nil
}

// RenderMarkdown produces the human-readable review copy. Every value that
// came from the catalog or configuration passes through the cell neutraliser
// because it is upstream-controlled text.
func RenderMarkdown(manifest Manifest) []byte {
	var b strings.Builder
	b.WriteString("# Speakeasy OIN Cross App Access manifest\n\n")
	fmt.Fprintf(&b, "Generated at %s (manifest version %d).\n\n", manifest.GeneratedAt.UTC().Format(time.RFC3339), manifest.ManifestVersion)
	b.WriteString("Nothing here is customer data: this is the Speakeasy platform catalog as Okta's questionnaire wants it.\n\n")

	b.WriteString("## Requesting app\n\n")
	b.WriteString("| Field | Value |\n|---|---|\n")
	app := manifest.RequestingApp
	writeRow(&b, "Listing name", app.Name)
	writeRow(&b, "Org domain", app.OrgDomain)
	writeRow(&b, "SSO mode", app.SSOMode)
	writeRow(&b, "Role", app.Role)
	writeRow(&b, "Redirect URI", app.RedirectURI)
	writeRow(&b, "Subject token type", app.SubjectTokenType)
	writeRow(&b, "Sends resource parameter", fmt.Sprintf("%t", app.SendsResourceParameter))
	b.WriteString("\n")

	b.WriteString("## Trust requirements\n\n")
	for _, requirement := range manifest.TrustRequirements {
		b.WriteString("- " + escapeCell(requirement) + "\n")
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## Resource registrations\n\n%d registrations: %d ready, %d not ready.\n\n", manifest.Summary.Registrations, manifest.Summary.Ready, manifest.Summary.Blocked)

	ready := make([]Registration, 0, len(manifest.ResourceRegistrations))
	blocked := make([]Registration, 0, len(manifest.ResourceRegistrations))
	for _, registration := range manifest.ResourceRegistrations {
		if registration.Ready() {
			ready = append(ready, registration)
		} else {
			blocked = append(blocked, registration)
		}
	}

	b.WriteString("### Ready\n\n")
	if len(ready) == 0 {
		b.WriteString("No registration is ready.\n\n")
	} else {
		writeRegistrationHeader(&b, false)
		for _, registration := range ready {
			writeRegistrationRow(&b, registration, false)
		}
		b.WriteString("\n")
	}

	if len(blocked) > 0 {
		b.WriteString("### Not ready\n\n")
		writeRegistrationHeader(&b, true)
		for _, registration := range blocked {
			writeRegistrationRow(&b, registration, true)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Evidence\n\n")
	b.WriteString("Conformance evidence is not collected yet; every registration reports `unavailable` until the ID-JAG diagnostic ships.\n")
	return []byte(b.String())
}

func writeRegistrationHeader(b *strings.Builder, withBlockers bool) {
	b.WriteString("| Resource | AS issuer | Resource identifier | XAA audience | Client ID | Scopes | Registration | Auth method")
	if withBlockers {
		b.WriteString(" | Blockers")
	}
	b.WriteString(" |\n|---|---|---|---|---|---|---|---")
	if withBlockers {
		b.WriteString("|---")
	}
	b.WriteString("|\n")
}

func writeRegistrationRow(b *strings.Builder, registration Registration, withBlockers bool) {
	cells := []string{
		registration.ResourceName,
		registration.ResourceASIssuer,
		registration.ResourceIdentifier,
		ptrOr(registration.XAAAudience, "unknown"),
		ptrOr(registration.ClientID, "none"),
		strings.Join(registration.Scopes, ", "),
		registration.Registration,
		ptrOr(registration.TokenEndpointAuthMethod, "unset"),
	}
	if withBlockers {
		cells = append(cells, strings.Join(registration.Blockers, "; "))
	}
	b.WriteString("|")
	for _, cell := range cells {
		b.WriteString(" " + escapeCell(cell) + " |")
	}
	b.WriteString("\n")
}

func writeRow(b *strings.Builder, field, value string) {
	fmt.Fprintf(b, "| %s | %s |\n", escapeCell(field), escapeCell(value))
}

func ptrOr(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

// escapeCell neutralises a value for a Markdown table cell: backslashes are
// escaped first so a stored `\|` cannot unescape the pipe that follows, pipes
// and newlines would break the table, raw HTML is entity-escaped so a viewer
// never renders upstream markup, control characters are dropped, and a leading
// Markdown structural character is prefixed with a backslash so the cell
// cannot open a heading, list, quote, or code fence.
func escapeCell(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	for _, r := range value {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			out.WriteByte(' ')
		case r == '\\':
			out.WriteString(`\\`)
		case r == '|':
			out.WriteString(`\|`)
		case r == '&':
			out.WriteString("&amp;")
		case r == '<':
			out.WriteString("&lt;")
		case r == '>':
			out.WriteString("&gt;")
		case unicode.IsControl(r):
			continue
		default:
			out.WriteRune(r)
		}
	}
	escaped := strings.TrimSpace(out.String())
	if escaped == "" {
		return ""
	}
	if strings.ContainsRune("#-*+`~=", rune(escaped[0])) {
		escaped = `\` + escaped
	}
	return escaped
}
