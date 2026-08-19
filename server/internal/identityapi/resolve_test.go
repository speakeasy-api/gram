package identityapi_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/identity"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// The whole point of the identity URN is that every surface links with the
// identifier it happens to hold. Whichever one that is, the caller must land
// on the same subject and the same canonical URN.
func TestResolve_ConvergesOnCanonicalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	byID, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "user:" + mockidp.MockUserID, ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	byEmail, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "email:" + mockidp.MockUserEmail, ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "user:"+mockidp.MockUserID, byID.CanonicalUrn)
	require.Equal(t, byID.CanonicalUrn, byEmail.CanonicalUrn)
	require.Equal(t, byID.UserIds, byEmail.UserIds)
	require.Equal(t, byID.Emails, byEmail.Emails)
}

func TestResolve_DirectoryUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	resolved, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "user:" + mockidp.MockUserID, ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "human", resolved.Kind)
	require.Equal(t, "Dev User", resolved.DisplayName)
	require.Equal(t, []string{mockidp.MockUserID}, resolved.UserIds)
	require.Contains(t, resolved.Emails, mockidp.MockUserEmail)
	require.NotNil(t, resolved.Directory)

	// Every address the person is known by is a candidate external user id,
	// because agents report whichever address the local tool was configured
	// with.
	require.Contains(t, resolved.ExternalUserIds, mockidp.MockUserEmail)
}

// An email that matches no directory row still has telemetry and cost, so it
// resolves to an unattributed identity rather than failing.
func TestResolve_UnknownEmailIsUnattributed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	resolved, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "email:nobody@example.com", ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "unattributed", resolved.Kind)
	require.Equal(t, "email:nobody@example.com", resolved.CanonicalUrn)
	require.Empty(t, resolved.UserIds)
	require.Equal(t, []string{"nobody@example.com"}, resolved.Emails)
}

// Agents commonly report the person's address as the external user id, so an
// address-shaped external URN resolves to the person behind it.
func TestResolve_ExternalUserID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	opaque, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "external:svc-7", ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "unattributed", opaque.Kind)
	require.Equal(t, "external:svc-7", opaque.CanonicalUrn)
	require.Equal(t, []string{"svc-7"}, opaque.ExternalUserIds)

	addressShaped, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "external:" + mockidp.MockUserEmail, ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "human", addressShaped.Kind)
	require.Equal(t, "user:"+mockidp.MockUserID, addressShaped.CanonicalUrn)
}

func TestResolve_APIKeySubject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	resolved, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: "apikey:33333333-3333-3333-3333-333333333333", ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)

	require.Equal(t, "apikey", resolved.Kind)
	require.Equal(t, "apikey:33333333-3333-3333-3333-333333333333", resolved.CanonicalUrn)
	require.Empty(t, resolved.UserIds)
}

func TestResolve_RejectsInvalidAndUnsupportedURNs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestIdentityService(t)

	tests := []struct {
		name string
		urn  string
		code oops.Code
	}{
		{name: "malformed", urn: "not-a-urn", code: oops.CodeBadRequest},
		{name: "role principal", urn: "role:organization:admin", code: oops.CodeBadRequest},
		{name: "agent not yet minted", urn: "agent:agt_01abc", code: oops.CodeNotImplemented},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ti.service.Resolve(ctx, &gen.ResolvePayload{Urn: tt.urn, ApikeyToken: nil, SessionToken: nil})
			require.Error(t, err)

			var shareable *oops.ShareableError
			require.ErrorAs(t, err, &shareable)
			require.Equal(t, tt.code, shareable.Code)
		})
	}
}
