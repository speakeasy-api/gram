package mockidp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeterministicUUID(t *testing.T) {
	// Same input → same output
	a := deterministicUUID("user:abc")
	b := deterministicUUID("user:abc")
	assert.Equal(t, a, b)

	// Different input → different output
	c := deterministicUUID("user:xyz")
	assert.NotEqual(t, a, c)

	// Has UUID-like format (8-4-4-4-12 hex)
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, a)
}

func TestDeriveOrgs_WorkOSClaims(t *testing.T) {
	claims := &OidcClaims{
		Sub:     "user1",
		Email:   "test@example.com",
		OrgID:   "org_123",
		OrgName: "My Company",
	}
	orgs := deriveOrgs(claims)
	require.Len(t, orgs, 1)
	assert.Equal(t, "My Company", orgs[0].Name)
	assert.Equal(t, "my-company", orgs[0].Slug)
	assert.Equal(t, deterministicUUID("org:org_123"), orgs[0].ID)
	assert.Equal(t, "free", orgs[0].AccountType)
}

func TestDeriveOrgs_GroupsClaims(t *testing.T) {
	claims := &OidcClaims{
		Sub:    "user1",
		Email:  "test@example.com",
		Groups: []string{"Engineering", "Platform Team"},
	}
	orgs := deriveOrgs(claims)
	require.Len(t, orgs, 2)
	assert.Equal(t, "Engineering", orgs[0].Name)
	assert.Equal(t, "engineering", orgs[0].Slug)
	assert.Equal(t, "Platform Team", orgs[1].Name)
	assert.Equal(t, "platform-team", orgs[1].Slug)
}

func TestDeriveOrgs_FallbackToDomain(t *testing.T) {
	claims := &OidcClaims{
		Sub:   "user1",
		Email: "alice@acme.com",
	}
	orgs := deriveOrgs(claims)
	require.Len(t, orgs, 1)
	assert.Equal(t, "acme.com", orgs[0].Name)
	assert.Equal(t, deterministicUUID("domain:acme.com"), orgs[0].ID)
}

func TestDeriveOrgs_FallbackNoEmail(t *testing.T) {
	claims := &OidcClaims{
		Sub: "user1",
	}
	orgs := deriveOrgs(claims)
	require.Len(t, orgs, 1)
	assert.Equal(t, "unknown", orgs[0].Name)
}

func TestMapClaimsToSession(t *testing.T) {
	claims := &OidcClaims{
		Sub:     "workos_user_abc",
		Email:   "alice@example.com",
		Name:    "Alice Smith",
		Picture: "https://example.com/photo.jpg",
		OrgID:   "org_1",
		OrgName: "Test Org",
	}
	sess := mapClaimsToSession(claims)

	assert.Equal(t, deterministicUUID("user:workos_user_abc"), sess.User.ID)
	assert.Equal(t, "alice@example.com", sess.User.Email)
	assert.Equal(t, "Alice Smith", sess.User.DisplayName)
	require.NotNil(t, sess.User.PhotoURL)
	assert.Equal(t, "https://example.com/photo.jpg", *sess.User.PhotoURL)
	assert.False(t, sess.User.Admin)
	assert.True(t, sess.User.Whitelisted)

	require.Len(t, sess.Organizations, 1)
	assert.Equal(t, "Test Org", sess.Organizations[0].Name)
}

func TestMapClaimsToSession_Fallbacks(t *testing.T) {
	t.Run("no email uses sub", func(t *testing.T) {
		sess := mapClaimsToSession(&OidcClaims{Sub: "user123"})
		assert.Equal(t, "user123", sess.User.Email)
		assert.Equal(t, "user123", sess.User.DisplayName)
	})

	t.Run("no name uses email", func(t *testing.T) {
		sess := mapClaimsToSession(&OidcClaims{Sub: "u1", Email: "bob@test.com"})
		assert.Equal(t, "bob@test.com", sess.User.DisplayName)
	})

	t.Run("no picture is nil", func(t *testing.T) {
		sess := mapClaimsToSession(&OidcClaims{Sub: "u1"})
		assert.Nil(t, sess.User.PhotoURL)
	})
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"My Company", "my-company"},
		{"hello world 123", "hello-world-123"},
		{"  Leading Trailing  ", "leading-trailing"},
		{"UPPERCASE", "uppercase"},
		{"special!@#chars", "special-chars"},
		{"multiple---dashes", "multiple-dashes"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, slugify(tt.input))
		})
	}
}
