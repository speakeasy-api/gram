package remotesessions

import (
	"encoding/json"
	"net/mail"
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

	// Caveat qualifies an identity taken from a stored interface answer rather
	// than the recorded identity columns; empty when the identity is recorded.
	Caveat string
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

// tokenResponseOwner reads the Notion token-response owner (owner.user.name, owner.user.person.email).
// A workspace-level owner carries no user and yields nothing.
func tokenResponseOwner(extras map[string]json.RawMessage) (name, email string) {
	raw, ok := extras["owner"]
	if !ok {
		return "", ""
	}
	var owner struct {
		User struct {
			Name   string `json:"name"`
			Person struct {
				Email string `json:"email"`
			} `json:"person"`
		} `json:"user"`
	}
	if json.Unmarshal(raw, &owner) != nil {
		return "", ""
	}
	return strings.TrimSpace(owner.User.Name), strings.TrimSpace(owner.User.Person.Email)
}

// idTokenRejectedCaveat qualifies an identity shown from userinfo after the exchange rejected the grant's ID token.
const idTokenRejectedCaveat = "Unconfirmed. The provider reported this account, but its identity proof didn't verify, so it isn't recorded for this connection."

// subjectMismatchCaveat qualifies a recorded identity that a later provider answer contradicted.
const subjectMismatchCaveat = "Out of date. The provider now reports a different account for this connection than the one recorded here. Reconnect to update it."

// interfaceReasonSubjectMismatch is the fixed reason an enricher records when an answer names another account.
const interfaceReasonSubjectMismatch = "subject mismatch"

// RemoteSessionAccountLabel decides the account label from the identity columns and the enrichment document.
// A JWT-sourced name is shown only alongside an email claim or an email-shaped subject.
// Without a recorded identity, a stored userinfo answer under a rejected ID token is shown with a caveat,
// else a Notion-style token-response owner names the account.
func RemoteSessionAccountLabel(email, displayName, subject, source string, enrichment []byte) AccountLabel {
	if source == IdentitySourceJWTAccessToken && email == "" {
		if parsed, err := mail.ParseAddress(subject); err == nil && parsed.Address == subject {
			email = subject
		} else {
			// Hide the identity, not independently supplied provider context.
			displayName = ""
		}
	}
	var doc enrichmentDocument
	if len(enrichment) > 0 {
		var parsed enrichmentDocument
		if json.Unmarshal(enrichment, &parsed) == nil {
			doc = parsed
		}
	}
	extras := doc.TokenResponse
	caveat := ""
	if displayName == "" && email == "" && doc.Interfaces[IdentitySourceIDToken].Status == interfaceStatusRejected {
		displayName, email = displayNameFromClaims(doc.Userinfo), claimString(doc.Userinfo, "email")
		if displayName != "" || email != "" {
			caveat = idTokenRejectedCaveat
		}
	}
	if displayName == "" && email == "" {
		displayName, email = tokenResponseOwner(extras)
	} else if caveat == "" {
		// Only live answers about the account contradict it; a JWT for another subject is rejected as a token, not a restatement.
		for _, name := range []string{IdentitySourceUserinfo, IdentitySourceIntrospection} {
			if doc.Interfaces[name].Reason == interfaceReasonSubjectMismatch {
				caveat = subjectMismatchCaveat
				break
			}
		}
	}
	label := AccountLabel{Name: conv.Default(displayName, email), Email: "", Chips: nil, Caveat: caveat}
	if displayName != "" && email != "" {
		label.Email = email
	}
	if extras == nil {
		return label
	}
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
