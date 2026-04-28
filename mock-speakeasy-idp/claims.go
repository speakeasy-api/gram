package mockidp

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// userSession holds the Speakeasy-format user and org data derived from OIDC claims.
type userSession struct {
	User          user           `json:"user"`
	Organizations []organization `json:"organizations"`
}

// deterministicUUID generates a stable UUID-like string from an input.
func deterministicUUID(input string) string {
	h := sha256.Sum256([]byte(input))
	hex := fmt.Sprintf("%x", h)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex[0:8], hex[8:12], hex[12:16], hex[16:20], hex[20:32],
	)
}

func deriveOrgs(claims *OidcClaims) []organization {
	now := time.Now().UTC().Format(time.RFC3339)

	// WorkOS org claims — derive a UUID for the Speakeasy org ID (mirroring
	// production where Speakeasy org IDs are UUIDs, not WorkOS org_... IDs).
	// The WorkOS org ID is preserved as SSOConnectionID.
	if claims.OrgID != "" && claims.OrgName != "" {
		workosOrgID := claims.OrgID
		return []organization{
			{
				ID:                 deterministicUUID("org:" + workosOrgID),
				Name:               claims.OrgName,
				Slug:               slugify(claims.OrgName),
				CreatedAt:          now,
				UpdatedAt:          now,
				AccountType:        "free",
				SSOConnectionID:    &workosOrgID,
				WorkOSID:           &workosOrgID,
				UserWorkspaceSlugs: []string{},
			},
		}
	}

	// Groups claim → one org per group
	if len(claims.Groups) > 0 {
		orgs := make([]organization, 0, len(claims.Groups))
		for _, g := range claims.Groups {
			orgs = append(orgs, organization{
				ID:                 deterministicUUID("group:" + g),
				Name:               g,
				Slug:               slugify(g),
				CreatedAt:          now,
				UpdatedAt:          now,
				AccountType:        "free",
				WorkOSID:           nil,
				UserWorkspaceSlugs: []string{},
			})
		}
		return orgs
	}

	// Fallback: derive org from email domain
	email := claims.Email
	if email == "" {
		email = claims.Sub
	}
	domain := "unknown"
	if _, after, ok := strings.Cut(email, "@"); ok {
		domain = after
	}
	return []organization{
		{
			ID:                 deterministicUUID("domain:" + domain),
			Name:               domain,
			Slug:               slugify(domain),
			CreatedAt:          now,
			UpdatedAt:          now,
			AccountType:        "free",
			WorkOSID:           nil,
			UserWorkspaceSlugs: []string{},
		},
	}
}

// mapClaimsToSession converts OIDC claims into a Speakeasy-compatible session.
func mapClaimsToSession(claims *OidcClaims) *userSession {
	now := time.Now().UTC().Format(time.RFC3339)

	email := claims.Email
	if email == "" {
		email = claims.Sub
	}

	displayName := claims.Name
	if displayName == "" {
		displayName = email
	}

	var photoURL *string
	if claims.Picture != "" {
		photoURL = &claims.Picture
	}

	u := user{
		ID:          deterministicUUID("user:" + claims.Sub),
		Email:       email,
		DisplayName: displayName,
		PhotoURL:    photoURL,
		Admin:       false,
		CreatedAt:   now,
		UpdatedAt:   now,
		Whitelisted: true,
	}

	return &userSession{
		User:          u,
		Organizations: deriveOrgs(claims),
	}
}
