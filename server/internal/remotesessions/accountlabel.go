package remotesessions

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// AccountLabel is what a session is called on every surface: who holds the
// grant at the provider, and where.
type AccountLabel struct {
	// Name is the upstream display name, else the email; empty when no interface answered.
	Name string

	// Email is the upstream email when a display name is also known, so both are shown.
	Email string

	// Chips are provider context read from the enrichment document: a Notion
	// workspace, a Slack team, a GitHub login.
	Chips []string
}

// Identity is the name and email on one line, without the chips; provider text is folded onto one line.
func (l AccountLabel) Identity() string {
	parts := make([]string, 0, 2)
	if l.Name != "" {
		parts = append(parts, oneLine(l.Name))
	}
	if l.Email != "" {
		parts = append(parts, oneLine(l.Email))
	}
	return strings.Join(parts, " · ")
}

// Context is the chips, each folded onto one line.
func (l AccountLabel) Context() []string {
	chips := make([]string, 0, len(l.Chips))
	for _, chip := range l.Chips {
		chips = append(chips, oneLine(chip))
	}
	return chips
}

// String joins the identity and chips for a single-line surface.
func (l AccountLabel) String() string {
	parts := l.Context()
	if identity := l.Identity(); identity != "" {
		parts = append([]string{identity}, parts...)
	}
	return strings.Join(parts, " · ")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// RemoteSessionAccountLabel decides the account label from the identity columns and the enrichment document.
func RemoteSessionAccountLabel(email, displayName string, enrichment []byte) AccountLabel {
	label := AccountLabel{Name: conv.Default(displayName, email), Email: "", Chips: nil}
	if displayName != "" && email != "" {
		label.Email = email
	}
	if len(enrichment) == 0 {
		return label
	}
	var doc enrichmentDocument
	if err := json.Unmarshal(enrichment, &doc); err != nil {
		return label
	}
	extras := doc.TokenResponse
	if workspace := claimString(extras, "workspace_name"); workspace != "" {
		label.Chips = append(label.Chips, workspace)
	}
	if raw, ok := extras["team"]; ok {
		var team map[string]json.RawMessage
		if json.Unmarshal(raw, &team) == nil {
			if name := claimString(team, "name"); name != "" {
				label.Chips = append(label.Chips, name)
			}
		}
	}
	if login := claimString(extras, "login"); login != "" && login != label.Name && login != label.Email {
		label.Chips = append(label.Chips, login)
	}
	return label
}

// IntrospectedToken is what the introspection interface last said about the access token.
type IntrospectedToken struct {
	// Active is the RFC 7662 active member.
	Active bool

	// ExpiresAt is the exp member; zero when the provider reported none.
	ExpiresAt time.Time
}

// IntrospectedTokenFromEnrichment reads the stored introspection answer; false when none is stored.
func IntrospectedTokenFromEnrichment(enrichment []byte) (IntrospectedToken, bool) {
	var none IntrospectedToken
	if len(enrichment) == 0 {
		return none, false
	}
	var doc enrichmentDocument
	if err := json.Unmarshal(enrichment, &doc); err != nil || doc.Introspection == nil {
		return none, false
	}
	// Only a JSON boolean is a verdict; a null or missing active is unknown.
	var active *bool
	raw, ok := doc.Introspection["active"]
	if !ok || json.Unmarshal(raw, &active) != nil || active == nil {
		return none, false
	}
	token := IntrospectedToken{Active: *active, ExpiresAt: time.Time{}}
	var exp int64
	if raw, ok := doc.Introspection["exp"]; ok && json.Unmarshal(raw, &exp) == nil && exp > 0 {
		token.ExpiresAt = time.Unix(exp, 0)
	}
	return token, true
}
