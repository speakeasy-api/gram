package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	ecrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	extkeysrepo "github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	ipcrepo "github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	oerepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
)

const platformCredentialStaffEmail = "operator@example.com"

type platformCredentialFixture struct {
	svc      *Service
	conn     *pgxpool.Pool
	resolver *gcpauth.StubResolver
	orgID    string
}

// newPlatformCredentialService returns an admin service with a stub GCP
// identity and an organization for the org-scoped rows platform credentials
// must never reach. The staff user is not seeded: tests opt in with
// seedPlatformCredentialStaff.
func newPlatformCredentialService(t *testing.T) *platformCredentialFixture {
	t.Helper()

	_, svc, conn := newTestAdminService(t)
	resolver := gcpauth.NewStubResolver()
	svc.SetGCPIdentity(gcpauth.NewIdentity(resolver))

	orgID := "org_" + uuid.NewString()
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(conn).CreateOrganizationMetadataFixture(t.Context(), testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: "Platform Credential Org", Slug: "platform-credential-" + orgID[len(orgID)-8:], GramAccountType: "enterprise",
		FreeTrialStartedAt: conv.ToPGTimestamptz(now), FreeTrialEndsAt: conv.ToPGTimestamptz(now.Add(14 * 24 * time.Hour)),
	}))

	return &platformCredentialFixture{svc: svc, conn: conn, resolver: resolver, orgID: orgID}
}

// newPlatformCredentialAdmin is newPlatformCredentialService with the staff
// identity's email seeded as a live platform administrator.
func newPlatformCredentialAdmin(t *testing.T) (context.Context, *platformCredentialFixture) {
	t.Helper()

	f := newPlatformCredentialService(t)
	seedPlatformCredentialStaff(t, f.conn, platformCredentialStaffEmail, true)
	return withPlatformCredentialStaff(t.Context()), f
}

// seedPlatformCredentialStaff records a Speakeasy user for the staff email,
// with or without the platform-admin entitlement, and returns its id.
func seedPlatformCredentialStaff(t *testing.T, conn *pgxpool.Pool, email string, admin bool) string {
	t.Helper()

	id := "user_" + uuid.NewString()
	q := testrepo.New(conn)
	require.NoError(t, q.InsertUserFixture(t.Context(), testrepo.InsertUserFixtureParams{ID: id, Email: email, DisplayName: "Test Operator"}))
	require.NoError(t, q.SetUserPlatformAdminFixture(t.Context(), testrepo.SetUserPlatformAdminFixtureParams{Admin: admin, ID: id}))
	return id
}

func withPlatformCredentialStaff(ctx context.Context) context.Context {
	return contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "admin-session",
		Email:       platformCredentialStaffEmail,
		OIDCSubject: "sub-operator",
		Name:        "Test Operator",
		HD:          "example.com",
	})
}

// ownProjectServiceAccount builds a service account in the project the stub
// resolver reports as Speakeasy's own, where platform credentials are expected.
func ownProjectServiceAccount(name string) string {
	_, domain, _ := strings.Cut(gcpauth.StubResolverPrincipal, "@")
	return name + "@" + domain
}

func requirePlatformCredentialCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func createPayload(name string, impersonate *string) *gen.CreateGcpIamPlatformCredentialPayload {
	return &gen.CreateGcpIamPlatformCredentialPayload{
		AdminSessionToken: nil, Name: name, ImpersonateServiceAccount: impersonate,
		WifPoolID: nil, WifProviderID: nil, WifProjectNumber: nil,
	}
}

func updatePayload(id, name string, impersonate *string) *gen.UpdateGcpIamPlatformCredentialPayload {
	return &gen.UpdateGcpIamPlatformCredentialPayload{
		ID: id, AdminSessionToken: nil, Name: name, ImpersonateServiceAccount: impersonate,
		WifPoolID: nil, WifProviderID: nil, WifProjectNumber: nil,
	}
}

func createAmbientPlatformCredential(t *testing.T, ctx context.Context, f *platformCredentialFixture, name string) *gen.GcpIamCredential {
	t.Helper()

	cred, err := f.svc.CreateGcpIamPlatformCredential(ctx, createPayload(name, nil))
	require.NoError(t, err)
	return cred
}

// createOrgGcpCredential writes an organization-scoped credential straight
// through the repo, so a test can show the platform handlers never reach it.
func createOrgGcpCredential(t *testing.T, f *platformCredentialFixture, name string) uuid.UUID {
	t.Helper()

	q := ecrepo.New(f.conn)
	ec, err := q.CreateExternalCredential(t.Context(), ecrepo.CreateExternalCredentialParams{
		OrganizationID: conv.ToPGText(f.orgID),
		Provider:       "gcp_iam",
		Name:           name,
	})
	require.NoError(t, err)
	_, err = q.CreateGcpIamCredential(t.Context(), ecrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      ec.ID,
		ImpersonateServiceAccount: conv.ToPGText("customer@customer-project.iam.gserviceaccount.com"),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
		SkipProjectVerification:   false,
	})
	require.NoError(t, err)
	return ec.ID
}

func createIssuerKey(t *testing.T, f *platformCredentialFixture, credID uuid.UUID) (*jwksrepo.Queries, uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	backing, err := extkeysrepo.New(f.conn).CreateExternalKey(ctx, extkeysrepo.CreateExternalKeyParams{
		OrganizationID:               conv.ToPGText(f.orgID),
		ExternalCredentialID:         credID,
		Provider:                     "gcp_kms",
		Algorithm:                    "RS256",
		Name:                         "issuer",
		CustomerGrantReference:       pgtype.Text{String: "", Valid: false},
		IdentityProviderConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	jq := jwksrepo.New(f.conn)
	set, err := jq.CreateJsonWebKeySet(ctx, jwksrepo.CreateJsonWebKeySetParams{OrganizationID: f.orgID, ExternalKeyID: backing.ID, Name: "issuer", IdentityProviderConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	require.NoError(t, err)
	_, err = jq.CreateInternalSessionIssuer(ctx, jwksrepo.CreateInternalSessionIssuerParams{OrganizationID: f.orgID, JsonWebKeySetID: set.ID, Issuer: "https://app.example.com/oauth/idp/org/" + f.orgID})
	require.NoError(t, err)
	key, err := jq.CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{OrganizationID: f.orgID, JsonWebKeySetID: set.ID, ExternalKeyID: backing.ID, State: "active", Kid: "kid-1", PublicJwk: []byte(`{"kty":"EC"}`)})
	require.NoError(t, err)
	return jq, key.ID
}

// platformCredentialLogs collects the service's log output so a test can
// assert on the exemption grant, whose log line is its only record.
type platformCredentialLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *platformCredentialLogs) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n, err := c.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("capture log output: %w", err)
	}
	return n, nil
}

func (c *platformCredentialLogs) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Each handler maps its result field by field, so a round trip through all six
// proves the conversions as well as the dispatch.
func TestPlatformCredentials_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	target := ownProjectServiceAccount("signer")
	created, err := f.svc.CreateGcpIamPlatformCredential(ctx, createPayload("Platform signer", &target))
	require.NoError(t, err)
	require.Equal(t, "Platform signer", created.Name)
	require.Equal(t, "gcp_iam", created.Provider)
	require.Empty(t, created.OrganizationID, "a platform credential must not be scoped to an organization")
	require.Equal(t, &target, created.ImpersonateServiceAccount)
	require.NotEmpty(t, created.CreatedAt)

	listed, err := f.svc.ListPlatformExternalCredentials(ctx, &gen.ListPlatformExternalCredentialsPayload{AdminSessionToken: nil, Provider: nil})
	require.NoError(t, err)
	require.Len(t, listed.Credentials, 1)
	require.Equal(t, gen.ExternalCredentialSummary{
		ID: created.ID, OrganizationID: created.OrganizationID, Provider: "gcp_iam", Name: "Platform signer",
		CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt,
	}, *listed.Credentials[0])

	updated, err := f.svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(created.ID, "Renamed", &target))
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)

	got, err := f.svc.GetGcpIamPlatformCredential(ctx, &gen.GetGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, updated, got)

	verified, err := f.svc.VerifyGcpIamPlatformCredential(ctx, &gen.VerifyGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.True(t, verified.Verified)
	require.NotNil(t, verified.Principal)

	require.NoError(t, f.svc.DeleteGcpIamPlatformCredential(ctx, &gen.DeleteGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: created.ID}))

	_, err = f.svc.GetGcpIamPlatformCredential(ctx, &gen.GetGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: created.ID})
	requirePlatformCredentialCode(t, err, oops.CodeNotFound)

	listed, err = f.svc.ListPlatformExternalCredentials(ctx, &gen.ListPlatformExternalCredentialsPayload{AdminSessionToken: nil, Provider: nil})
	require.NoError(t, err)
	require.Empty(t, listed.Credentials)
}

// callAllPlatformCredentialMethods invokes every platform credential method
// and returns each one's error keyed by method name, so authorization tests
// cover a handler added later that forgets the gate.
func callAllPlatformCredentialMethods(ctx context.Context, svc *Service) map[string]error {
	id := uuid.NewString()
	_, createErr := svc.CreateGcpIamPlatformCredential(ctx, createPayload("x", nil))
	_, listErr := svc.ListPlatformExternalCredentials(ctx, &gen.ListPlatformExternalCredentialsPayload{AdminSessionToken: nil, Provider: nil})
	_, updateErr := svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(id, "x", nil))
	_, getErr := svc.GetGcpIamPlatformCredential(ctx, &gen.GetGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: id})
	_, verifyErr := svc.VerifyGcpIamPlatformCredential(ctx, &gen.VerifyGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: id})
	deleteErr := svc.DeleteGcpIamPlatformCredential(ctx, &gen.DeleteGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: id})
	return map[string]error{
		"create": createErr, "list": listErr, "update": updateErr,
		"get": getErr, "verify": verifyErr, "delete": deleteErr,
	}
}

func TestPlatformCredentials_UnavailableWithoutGCPIdentity(t *testing.T) {
	t.Parallel()

	svc := &Service{gcpIdentity: nil, logger: testenv.NewLogger(t)}
	for method, err := range callAllPlatformCredentialMethods(withPlatformCredentialStaff(t.Context()), svc) {
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr, method)
		require.Equal(t, oops.CodeUnavailable, oopsErr.Code, method)
	}
}

// Only the staff identity the admin transport verifies is accepted. A
// dashboard session, even a platform admin's, is not an auth method here, and
// neither is an admin context without a verified session.
func TestPlatformCredentials_RequireVerifiedStaffIdentity(t *testing.T) {
	t.Parallel()

	f := newPlatformCredentialService(t)
	userID := seedPlatformCredentialStaff(t, f.conn, platformCredentialStaffEmail, true)

	cases := map[string]context.Context{
		"no identity": t.Context(),
		"dashboard session": contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{
			UserID:    userID,
			Email:     new(platformCredentialStaffEmail),
			SessionID: new("session-1"),
			IsAdmin:   true,
		}, false),
		"unverified admin session": contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{
			SessionID: "", Email: platformCredentialStaffEmail, OIDCSubject: "sub-operator", Name: "Test Operator", HD: "example.com",
		}),
		"missing subject": contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{
			SessionID: "admin-session", Email: platformCredentialStaffEmail, OIDCSubject: "", Name: "Test Operator", HD: "example.com",
		}),
	}
	for name, ctx := range cases {
		for method, err := range callAllPlatformCredentialMethods(ctx, f.svc) {
			var oopsErr *oops.ShareableError
			require.ErrorAs(t, err, &oopsErr, "%s: %s", name, method)
			require.Equal(t, oops.CodeUnauthorized, oopsErr.Code, "%s: %s", name, method)
		}
	}
}

// Signing in to the admin app is not enough: the staff email must belong to a
// live platform administrator, for reads and writes alike.
func TestPlatformCredentials_RequirePlatformAdmin(t *testing.T) {
	t.Parallel()

	requireForbidden := func(t *testing.T, svc *Service) {
		t.Helper()
		for method, err := range callAllPlatformCredentialMethods(withPlatformCredentialStaff(t.Context()), svc) {
			var oopsErr *oops.ShareableError
			require.ErrorAs(t, err, &oopsErr, method)
			require.Equal(t, oops.CodeForbidden, oopsErr.Code, method)
		}
	}

	t.Run("no Speakeasy user", func(t *testing.T) {
		t.Parallel()
		requireForbidden(t, newPlatformCredentialService(t).svc)
	})

	t.Run("user without the entitlement", func(t *testing.T) {
		t.Parallel()
		f := newPlatformCredentialService(t)
		seedPlatformCredentialStaff(t, f.conn, platformCredentialStaffEmail, false)
		requireForbidden(t, f.svc)
	})

	t.Run("deleted administrator", func(t *testing.T) {
		t.Parallel()
		f := newPlatformCredentialService(t)
		id := seedPlatformCredentialStaff(t, f.conn, platformCredentialStaffEmail, true)
		require.NoError(t, testrepo.New(f.conn).ForceSoftDeleteUser(t.Context(), id))
		requireForbidden(t, f.svc)
	})

	t.Run("email matched case-insensitively", func(t *testing.T) {
		t.Parallel()
		f := newPlatformCredentialService(t)
		seedPlatformCredentialStaff(t, f.conn, "Operator@Example.com", true)
		createAmbientPlatformCredential(t, withPlatformCredentialStaff(t.Context()), f, "x")
	})
}

func TestCreateGcpIamPlatformCredential_Ambient(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	cred := createAmbientPlatformCredential(t, ctx, f, "platform-ambient")
	require.Equal(t, "gcp_iam", cred.Provider)
	require.Equal(t, "platform-ambient", cred.Name)
	require.Empty(t, cred.OrganizationID)
	require.Nil(t, cred.ImpersonateServiceAccount, "ambient credential has no impersonation target")
	require.Nil(t, cred.WifPoolID)
	_, err := uuid.Parse(cred.ID)
	require.NoError(t, err)
}

func TestCreateGcpIamPlatformCredential_RejectsInvalidForms(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	cases := map[string]*gen.CreateGcpIamPlatformCredentialPayload{
		"blank name": createPayload("   ", nil),
		"partial wif triple": {
			AdminSessionToken: nil, Name: "partial-wif", ImpersonateServiceAccount: nil,
			WifPoolID: new("pool"), WifProviderID: nil, WifProjectNumber: nil,
		},
		"impersonation target is not a service account":     createPayload("user-target", new("operator@example.com")),
		"impersonation target is a default compute account": createPayload("compute-target", new("123456789-compute@developer.gserviceaccount.com")),
		"non-numeric wif project number": {
			AdminSessionToken: nil, Name: "wif", ImpersonateServiceAccount: nil,
			WifPoolID: new("pool"), WifProviderID: new("provider"), WifProjectNumber: new("abc"),
		},
	}
	for name, payload := range cases {
		_, err := f.svc.CreateGcpIamPlatformCredential(ctx, payload)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr, name)
		require.Equal(t, oops.CodeBadRequest, oopsErr.Code, name)
	}
}

func TestCreateGcpIamPlatformCredential_ExemptsOwnProjectSigner(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	cred, err := f.svc.CreateGcpIamPlatformCredential(ctx, createPayload("identity-provider-signer", conv.PtrEmpty(provisiontest.SigningServiceAccount())))
	require.NoError(t, err)

	row, err := ecrepo.New(f.conn).GetGcpIamCredential(ctx, ecrepo.GetGcpIamCredentialParams{ID: uuid.MustParse(cred.ID), OrganizationID: pgtype.Text{String: "", Valid: false}})
	require.NoError(t, err)
	require.True(t, row.GcpIamCredential.SkipProjectVerification, "a signer in Speakeasy's own project must pass the own-project screen")
}

func TestUpdateGcpIamPlatformCredential_RenameDoesNotRelogExemption(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)
	logs := &platformCredentialLogs{mu: sync.Mutex{}, buf: bytes.Buffer{}}
	f.svc.logger = slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{AddSource: false, Level: slog.LevelInfo, ReplaceAttr: nil}))

	const grant = "exempted a gcp iam credential from own-project screening"
	signer := provisiontest.SigningServiceAccount()

	cred, err := f.svc.CreateGcpIamPlatformCredential(ctx, createPayload("identity-provider-signer", conv.PtrEmpty(signer)))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(logs.String(), grant))

	_, err = f.svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(cred.ID, "identity-provider-signer-renamed", conv.PtrEmpty(signer)))
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(logs.String(), grant), "a rename that keeps the target is not a new grant")

	_, err = f.svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(cred.ID, "identity-provider-signer-renamed", conv.PtrEmpty("other-"+signer)))
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(logs.String(), grant), "a new own-project target is a new grant")
}

func TestGetGcpIamPlatformCredential_Errors(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)
	orgCredID := createOrgGcpCredential(t, f, "org-scoped")

	_, err := f.svc.GetGcpIamPlatformCredential(ctx, &gen.GetGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: uuid.NewString()})
	requirePlatformCredentialCode(t, err, oops.CodeNotFound)

	_, err = f.svc.GetGcpIamPlatformCredential(ctx, &gen.GetGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: "not-a-uuid"})
	requirePlatformCredentialCode(t, err, oops.CodeBadRequest)

	// The platform query pins organization_id IS NULL AND project_id IS NULL.
	_, err = f.svc.GetGcpIamPlatformCredential(ctx, &gen.GetGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: orgCredID.String()})
	requirePlatformCredentialCode(t, err, oops.CodeNotFound)
}

func TestListPlatformExternalCredentials_FiltersAndExcludesOrgCredentials(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	a := createAmbientPlatformCredential(t, ctx, f, "platform-a")
	b := createAmbientPlatformCredential(t, ctx, f, "platform-b")
	createOrgGcpCredential(t, f, "org-scoped")

	ids := func(provider *string) []string {
		result, err := f.svc.ListPlatformExternalCredentials(ctx, &gen.ListPlatformExternalCredentialsPayload{AdminSessionToken: nil, Provider: provider})
		require.NoError(t, err)
		out := make([]string, 0, len(result.Credentials))
		for _, c := range result.Credentials {
			require.Empty(t, c.OrganizationID, "platform list must only contain platform-scoped rows")
			out = append(out, c.ID)
		}
		return out
	}

	require.ElementsMatch(t, []string{a.ID, b.ID}, ids(nil))
	require.ElementsMatch(t, []string{a.ID, b.ID}, ids(new("gcp_iam")))
	require.Empty(t, ids(new("aws_iam")))
}

func TestUpdateGcpIamPlatformCredential_ReplacesConfig(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	created := createAmbientPlatformCredential(t, ctx, f, "platform-update")
	target := "gram@gram-platform.iam.gserviceaccount.com"

	updated, err := f.svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(created.ID, "platform-update-renamed", &target))
	require.NoError(t, err)
	require.Equal(t, created.ID, updated.ID)
	require.Equal(t, "platform-update-renamed", updated.Name)
	require.Equal(t, &target, updated.ImpersonateServiceAccount)

	got, err := f.svc.GetGcpIamPlatformCredential(ctx, &gen.GetGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, updated, got)
}

func TestUpdateGcpIamPlatformCredential_Errors(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	created := createAmbientPlatformCredential(t, ctx, f, "platform-update")
	orgCredID := createOrgGcpCredential(t, f, "org-scoped")

	_, err := f.svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(created.ID, "   ", nil))
	requirePlatformCredentialCode(t, err, oops.CodeBadRequest)

	_, err = f.svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(created.ID, "user-target", new("operator@example.com")))
	requirePlatformCredentialCode(t, err, oops.CodeBadRequest)

	_, err = f.svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(uuid.NewString(), "nope", nil))
	requirePlatformCredentialCode(t, err, oops.CodeNotFound)

	_, err = f.svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(orgCredID.String(), "hijack", nil))
	requirePlatformCredentialCode(t, err, oops.CodeNotFound)
}

func TestVerifyGcpIamPlatformCredential_Outcomes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		resolve      func(context.Context, gcpauth.Credential) (gcpauth.Principal, error)
		verified     bool
		principal    *string
		source       *string
		detail       bool
		detailSubstr string
	}{
		{
			name: "resolves",
			resolve: func(context.Context, gcpauth.Credential) (gcpauth.Principal, error) {
				return gcpauth.Principal{Email: "gram@gram-platform.iam.gserviceaccount.com", Source: gcpauth.SourceMetadataServer}, nil
			},
			verified: true, principal: new("gram@gram-platform.iam.gserviceaccount.com"), source: new("metadata_server"), detail: false, detailSubstr: "",
		},
		{
			// e.g. user-backed local ADC: still verified, with a note.
			name: "resolves without email",
			resolve: func(context.Context, gcpauth.Credential) (gcpauth.Principal, error) {
				return gcpauth.Principal{Email: "", Source: gcpauth.SourceADC}, nil
			},
			verified: true, principal: nil, source: new("application_default_credentials"), detail: true, detailSubstr: "did not report a principal email",
		},
		{
			name: "unsupported mode",
			resolve: func(context.Context, gcpauth.Credential) (gcpauth.Principal, error) {
				return gcpauth.Principal{}, gcpauth.ErrUnsupportedMode
			},
			verified: false, principal: nil, source: nil, detail: true, detailSubstr: "ambient",
		},
		{
			name: "resolve failure",
			resolve: func(context.Context, gcpauth.Credential) (gcpauth.Principal, error) {
				return gcpauth.Principal{}, errors.New("metadata server unreachable")
			},
			verified: false, principal: nil, source: nil, detail: true, detailSubstr: "unreachable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newPlatformCredentialAdmin(t)
			cred := createAmbientPlatformCredential(t, ctx, f, "platform-verify")
			f.resolver.SetResolve(tc.resolve)

			result, err := f.svc.VerifyGcpIamPlatformCredential(ctx, &gen.VerifyGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: cred.ID})
			require.NoError(t, err, "an unresolvable probe is a reportable outcome, not a request error")
			require.Equal(t, tc.verified, result.Verified)
			require.Equal(t, tc.principal, result.Principal)
			require.Equal(t, tc.source, result.IdentitySource)
			if !tc.detail {
				require.Nil(t, result.Detail)
				return
			}
			require.NotNil(t, result.Detail)
			require.Contains(t, *result.Detail, tc.detailSubstr)
		})
	}
}

func TestVerifyGcpIamPlatformCredential_NotFound(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	_, err := f.svc.VerifyGcpIamPlatformCredential(ctx, &gen.VerifyGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: uuid.NewString()})
	requirePlatformCredentialCode(t, err, oops.CodeNotFound)
}

func TestDeleteGcpIamPlatformCredential_MissingAndOrgScopedAreNoOps(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)
	orgCredID := createOrgGcpCredential(t, f, "org-scoped")

	require.NoError(t, f.svc.DeleteGcpIamPlatformCredential(ctx, &gen.DeleteGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: uuid.NewString()}))
	require.NoError(t, f.svc.DeleteGcpIamPlatformCredential(ctx, &gen.DeleteGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: orgCredID.String()}))

	_, err := ecrepo.New(f.conn).GetGcpIamCredential(ctx, ecrepo.GetGcpIamCredentialParams{ID: orgCredID, OrganizationID: conv.ToPGText(f.orgID)})
	require.NoError(t, err, "the org credential must survive a platform delete")
}

// A platform credential that managed signing keys still sign through cannot
// be deleted or re-pointed.
func TestGcpIamPlatformCredential_RefusesMutationWhileManagedKeysLive(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	cred := createAmbientPlatformCredential(t, ctx, f, "platform-managed")
	credID := uuid.MustParse(cred.ID)

	conn, err := ipcrepo.New(f.conn).CreateIdentityProviderConnection(ctx, ipcrepo.CreateIdentityProviderConnectionParams{OrganizationID: f.orgID, Provider: "okta"})
	require.NoError(t, err)
	key, err := extkeysrepo.New(f.conn).CreateExternalKey(ctx, extkeysrepo.CreateExternalKeyParams{
		OrganizationID:               conv.ToPGText(f.orgID),
		ExternalCredentialID:         credID,
		Provider:                     "gcp_kms",
		Algorithm:                    "RS256",
		Name:                         "managed",
		CustomerGrantReference:       pgtype.Text{String: "", Valid: false},
		IdentityProviderConnectionID: conv.ToNullUUID(conn.ID),
	})
	require.NoError(t, err)

	remove := func() error {
		return f.svc.DeleteGcpIamPlatformCredential(ctx, &gen.DeleteGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: cred.ID})
	}

	requirePlatformCredentialCode(t, remove(), oops.CodeConflict)
	_, err = f.svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(cred.ID, "renamed", nil))
	requirePlatformCredentialCode(t, err, oops.CodeConflict)

	_, err = extkeysrepo.New(f.conn).SoftDeleteExternalKey(ctx, extkeysrepo.SoftDeleteExternalKeyParams{ID: key.ID, OrganizationID: conv.ToPGText(f.orgID), Provider: "gcp_kms"})
	require.NoError(t, err)
	require.NoError(t, remove())
}

// Issuer keys sign as the credential's account and their cleanup removes the
// grant from it: the account can't change while any issuer key holds a grant,
// and the credential can't be deleted while a live issuer key signs through it.
func TestGcpIamPlatformCredential_GuardsIssuerKeyGrants(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	signer := provisiontest.SigningServiceAccount()
	cred, err := f.svc.CreateGcpIamPlatformCredential(ctx, createPayload("platform-issuer", conv.PtrEmpty(signer)))
	require.NoError(t, err)
	jq, keyID := createIssuerKey(t, f, uuid.MustParse(cred.ID))

	update := func(name, account string) error {
		_, err := f.svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(cred.ID, name, conv.PtrEmpty(account)))
		return err
	}
	remove := func() error {
		return f.svc.DeleteGcpIamPlatformCredential(ctx, &gen.DeleteGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: cred.ID})
	}

	require.NoError(t, update("renamed", signer), "a rename keeps the account")
	require.NoError(t, update("renamed", strings.ToUpper(signer)), "the account is stored lowercased, so a case-only change is no change")
	requirePlatformCredentialCode(t, update("renamed", "other-"+signer), oops.CodeConflict)
	_, err = f.svc.UpdateGcpIamPlatformCredential(ctx, &gen.UpdateGcpIamPlatformCredentialPayload{
		ID: cred.ID, AdminSessionToken: nil, Name: "renamed", ImpersonateServiceAccount: conv.PtrEmpty(signer),
		WifPoolID: conv.PtrEmpty("pool"), WifProviderID: conv.PtrEmpty("provider"), WifProjectNumber: conv.PtrEmpty("123456"),
	})
	requirePlatformCredentialCode(t, err, oops.CodeConflict)
	requirePlatformCredentialCode(t, remove(), oops.CodeConflict)

	_, err = jq.RevokeJsonWebKey(ctx, jwksrepo.RevokeJsonWebKeyParams{ID: keyID, OrganizationID: f.orgID})
	require.NoError(t, err)
	// A revoked key still awaiting cleanup keeps its grant.
	requirePlatformCredentialCode(t, update("renamed", "other-"+signer), oops.CodeConflict)

	require.NoError(t, jq.MarkJsonWebKeyKMSDisabled(ctx, jwksrepo.MarkJsonWebKeyKMSDisabledParams{ID: keyID, OrganizationID: f.orgID}))
	require.NoError(t, update("renamed", "other-"+signer))
}

// Only revoked issuer keys reference the credential: deleting it is allowed,
// since their cleanup reads the deleted credential's account.
func TestDeleteGcpIamPlatformCredential_AllowsOnlyRevokedIssuerKeys(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	cred := createAmbientPlatformCredential(t, ctx, f, "platform-issuer-revoked")
	jq, keyID := createIssuerKey(t, f, uuid.MustParse(cred.ID))
	_, err := jq.RevokeJsonWebKey(ctx, jwksrepo.RevokeJsonWebKeyParams{ID: keyID, OrganizationID: f.orgID})
	require.NoError(t, err)

	require.NoError(t, f.svc.DeleteGcpIamPlatformCredential(ctx, &gen.DeleteGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: cred.ID}))
}

// Speakeasy-managed organization encryption keys pin the platform credential's
// id and impersonated account: its identity cannot change and it cannot be
// deleted while one still reaches KMS through it. Renames stay allowed, and a
// key the cleanup sweep has disabled no longer counts.
func TestGcpIamPlatformCredential_GuardsManagedEncryptionKeys(t *testing.T) {
	t.Parallel()
	ctx, f := newPlatformCredentialAdmin(t)

	account := ownProjectServiceAccount("org-encryption")
	cred, err := f.svc.CreateGcpIamPlatformCredential(ctx, createPayload("platform-encryption", conv.PtrEmpty(account)))
	require.NoError(t, err)

	q := oerepo.New(f.conn)
	key, err := q.CreateEncryptionExternalKey(ctx, oerepo.CreateEncryptionExternalKeyParams{
		OrganizationID:       conv.ToPGText(f.orgID),
		ExternalCredentialID: uuid.MustParse(cred.ID),
		Algorithm:            "GOOGLE_SYMMETRIC_ENCRYPTION",
		Name:                 "managed-kek",
	})
	require.NoError(t, err)
	require.NoError(t, q.CreateManagedEncryptionKey(ctx, oerepo.CreateManagedEncryptionKeyParams{ExternalKeyID: key.ID, OrganizationID: f.orgID}))

	update := func(name, target string) error {
		_, err := f.svc.UpdateGcpIamPlatformCredential(ctx, updatePayload(cred.ID, name, conv.PtrEmpty(target)))
		return err
	}
	remove := func() error {
		return f.svc.DeleteGcpIamPlatformCredential(ctx, &gen.DeleteGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: cred.ID})
	}

	require.NoError(t, update("renamed", account), "a rename keeps the identity")
	requirePlatformCredentialCode(t, update("renamed", ownProjectServiceAccount("other")), oops.CodeConflict)
	requirePlatformCredentialCode(t, remove(), oops.CodeConflict)

	require.NoError(t, q.MarkManagedEncryptionKeyDisabled(ctx, oerepo.MarkManagedEncryptionKeyDisabledParams{ExternalKeyID: key.ID, OrganizationID: f.orgID}))
	require.NoError(t, update("renamed", ownProjectServiceAccount("other")))
	require.NoError(t, remove(), "a disabled key no longer blocks the delete")
}
