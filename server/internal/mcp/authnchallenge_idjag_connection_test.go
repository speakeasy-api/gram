package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// idJAGForm is an ID-JAG token request.
func idJAGForm(clientID, assertion string, resources ...string) url.Values {
	form := url.Values{
		"grant_type": {oauthwire.GrantTypeJWTBearer},
		"client_id":  {clientID},
		"assertion":  {assertion},
	}
	if len(resources) > 0 {
		form["resource"] = resources
	}
	return form
}

// postIDJAGForm posts form through the error handler the token route mounts, so
// answers the handler returns and answers it writes reach the client the same way.
func postIDJAGForm(t *testing.T, ctx context.Context, ti *testInstance, slug string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", slug)
	w := httptest.NewRecorder()
	oops.OAuthErrHandle(testenv.NewLogger(t), ti.service.HandleToken).ServeHTTP(w, req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx)))
	return w
}

func requireTokenResponse(t *testing.T, w *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	require.JSONEq(t, body, w.Body.String())
}

// requireOneIDJAGRow asserts the request wrote exactly one final inbound_token row and returns it.
func requireOneIDJAGRow(t *testing.T, rec *mcpconnectionattemptstest.Recording, what mcpconnectionattempts.Happened, secrets ...string) mcpconnectionattempts.Row {
	t.Helper()
	require.Len(t, rec.Rows(), 1, "%+v", rec.Rows())
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, what)
	require.Equal(t, oauthwire.GrantTypeJWTBearer, row.GrantType)
	rec.RequireNoUnclassifiedExit()
	requireNoSecretsInRows(t, rec.Rows(), secrets...)
	return row
}

// requireNoSecretsInRows asserts no row field holds an assertion or a claim from it.
func requireNoSecretsInRows(t *testing.T, rows []mcpconnectionattempts.Row, secrets ...string) {
	t.Helper()
	for _, row := range rows {
		dump := fmt.Sprintf("%+v", row)
		for _, secret := range append(secrets, mockidp.MockUserEmail, "external-user") {
			require.NotContains(t, dump, secret)
		}
	}
}

func requireRefusedBy(t *testing.T, row mcpconnectionattempts.Row, who mcpconnectionattempts.WhoFixes, detail string) {
	t.Helper()
	meaning, ok := mcpconnectionattempts.MeaningOf(row.Reason)
	require.True(t, ok)
	require.Equal(t, string(meaning.Outcome), row.Outcome)
	require.Equal(t, string(who), row.WhoFixes)
	require.Equal(t, detail, row.SubReason)
}

func TestIDJAGExchangeRecordsSignIn(t *testing.T) {
	t.Parallel()

	ctx, f := newIDJAGOutageFixture(t, true)
	rec := mcpconnectionattemptstest.New(t)
	f.ti.service.SetConnectionRecorder(rec.Recorder)
	jti := uuid.NewString()
	assertion := f.signer.sign(t, idJAGOutageIssuer, f.resource, f.resource, f.clientID, mockidp.MockUserEmail, jti)

	w := postIDJAGForm(t, ctx, f.ti, f.slug, idJAGForm(f.clientID, assertion))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	row := requireOneIDJAGRow(t, rec, mcpconnectionattempts.ResultSignedIn, assertion, jti, decodeSharedTokenResponse(t, w).AccessToken)
	require.Equal(t, string(mcpconnectionattempts.OutcomeConnected), row.Outcome)
	require.Empty(t, row.Reason)
	require.Equal(t, urn.NewUserSubject(mockidp.MockUserID).String(), row.SubjectURN)
	require.Equal(t, mcpconnectionattempts.AppOf(codexCIMDClientID, mcpconnectionattempts.ClientIDAdmitted).String(), row.AppKey)
	require.Equal(t, f.issuerID.String(), row.UserSessionIssuerID)
	require.NotEmpty(t, row.UserSessionClientID)
	require.NotEmpty(t, row.UserSessionID)
	require.NotEmpty(t, row.ServerID)
	require.Empty(t, row.ProviderHost)
}

// Every refusal after the client is checked writes its code once, and the answer is what it was before rows existed.
func TestIDJAGExchangeRecordsRefusals(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		granted bool
		form    func(f idJAGOutageFixture, assertion string) url.Values
		sign    func(t *testing.T, f idJAGOutageFixture, jti string) string
		status  int
		body    string
		what    mcpconnectionattempts.Code
		who     mcpconnectionattempts.WhoFixes
		detail  string
		subject bool
	}{
		"missing assertion": {
			granted: true,
			form:    func(f idJAGOutageFixture, _ string) url.Values { return idJAGForm(f.clientID, "") },
			status:  http.StatusBadRequest, body: `{"error":"invalid_request","error_description":"assertion is required"}`,
			what: mcpconnectionattempts.CodeInvalidRequest, who: mcpconnectionattempts.WhoApp,
		},
		"resource names another server": {
			granted: true,
			form: func(f idJAGOutageFixture, assertion string) url.Values {
				return idJAGForm(f.clientID, assertion, f.resource, "https://other.example.test/mcp")
			},
			status: http.StatusBadRequest,
			what:   mcpconnectionattempts.CodeResourceMismatch, who: mcpconnectionattempts.WhoApp,
		},
		"no resource anywhere": {
			granted: true,
			sign: func(t *testing.T, f idJAGOutageFixture, jti string) string {
				t.Helper()
				return f.signer.signWithoutResource(t, idJAGOutageIssuer, f.resource, f.clientID, mockidp.MockUserEmail, jti)
			},
			status: http.StatusBadRequest, body: `{"error":"invalid_target","error_description":"resource is required: name the MCP server this authorization is for"}`,
			what: mcpconnectionattempts.CodeInvalidRequest, who: mcpconnectionattempts.WhoApp, detail: "assertion_resource_not_indicated",
		},
		"assertion for another audience": {
			granted: true,
			sign: func(t *testing.T, f idJAGOutageFixture, jti string) string {
				t.Helper()
				return f.signer.sign(t, idJAGOutageIssuer, "https://elsewhere.example.test", f.resource, f.clientID, mockidp.MockUserEmail, jti)
			},
			status: http.StatusBadRequest, body: `{"error":"invalid_grant","error_description":"assertion is invalid"}`,
			what: mcpconnectionattempts.CodeAssertionInvalid, who: mcpconnectionattempts.WhoApp, detail: "assertion_audience_mismatch",
		},
		"assertion from an untrusted issuer": {
			granted: true,
			sign: func(t *testing.T, f idJAGOutageFixture, jti string) string {
				t.Helper()
				return f.signer.sign(t, "https://other-identity.example.test", f.resource, f.resource, f.clientID, mockidp.MockUserEmail, jti)
			},
			status: http.StatusBadRequest, body: `{"error":"invalid_grant","error_description":"assertion is invalid"}`,
			what: mcpconnectionattempts.CodeIssuerNotTrusted, who: mcpconnectionattempts.WhoAdmin, detail: "assertion_issuer_mismatch",
		},
		"person not in the directory": {
			granted: true,
			sign: func(t *testing.T, f idJAGOutageFixture, jti string) string {
				t.Helper()
				return f.signer.sign(t, idJAGOutageIssuer, f.resource, f.resource, f.clientID, "nobody-"+jti+"@example.test", jti)
			},
			status: http.StatusBadRequest, body: `{"error":"invalid_grant","error_description":"assertion is invalid"}`,
			what: mcpconnectionattempts.CodeSubjectNotProvisioned, who: mcpconnectionattempts.WhoAdmin, detail: "subject_not_provisioned",
		},
		"person without access": {
			status: http.StatusBadRequest, body: `{"error":"invalid_grant","error_description":"assertion is invalid"}`,
			what: mcpconnectionattempts.CodeNoServerAccess, who: mcpconnectionattempts.WhoAdmin, detail: "subject_not_authorized", subject: true,
		},
		"person without access to the requested resource": {
			sign: func(t *testing.T, f idJAGOutageFixture, jti string) string {
				t.Helper()
				return f.signer.signWithoutResource(t, idJAGOutageIssuer, f.resource, f.clientID, mockidp.MockUserEmail, jti)
			},
			form: func(f idJAGOutageFixture, assertion string) url.Values {
				return idJAGForm(f.clientID, assertion, f.resource)
			},
			status: http.StatusBadRequest, body: `{"error":"invalid_target","error_description":"resource is not available to this user"}`,
			what: mcpconnectionattempts.CodeNoServerAccess, who: mcpconnectionattempts.WhoAdmin, detail: "resource_not_available", subject: true,
		},
		"upstream choice the organization does not take": {
			granted: true,
			form: func(f idJAGOutageFixture, assertion string) url.Values {
				form := idJAGForm(f.clientID, assertion)
				form.Set("upstream_auth_client", uuid.NewString()+":none")
				return form
			},
			status: http.StatusBadRequest, body: `{"error":"invalid_request","error_description":"upstream_auth_client is not accepted for this organization yet"}`,
			what: mcpconnectionattempts.CodeUpstreamCredentialChoice, who: mcpconnectionattempts.WhoApp, detail: "choice_not_accepted", subject: true,
		},
		"malformed upstream choice": {
			granted: true,
			form: func(f idJAGOutageFixture, assertion string) url.Values {
				form := idJAGForm(f.clientID, assertion)
				form.Set("upstream_auth_client", "not-a-choice")
				return form
			},
			status: http.StatusBadRequest, body: `{"error":"invalid_request","error_description":"upstream_auth_client must be <mcp_server_id>:<auth_client_id> or <mcp_server_id>:none"}`,
			what: mcpconnectionattempts.CodeInvalidRequest, who: mcpconnectionattempts.WhoApp, detail: "upstream_auth_client", subject: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newIDJAGOutageFixture(t, tc.granted)
			if !tc.granted {
				ctx = authztest.WithExactGrants(t, ctx)
			}
			rec := mcpconnectionattemptstest.New(t)
			f.ti.service.SetConnectionRecorder(rec.Recorder)
			jti := uuid.NewString()
			assertion := f.signer.sign(t, idJAGOutageIssuer, f.resource, f.resource, f.clientID, mockidp.MockUserEmail, jti)
			if tc.sign != nil {
				assertion = tc.sign(t, f, jti)
			}
			form := idJAGForm(f.clientID, assertion)
			if tc.form != nil {
				form = tc.form(f, assertion)
			}

			w := postIDJAGForm(t, ctx, f.ti, f.slug, form)
			if tc.body == "" {
				require.Equal(t, tc.status, w.Code, w.Body.String())
				require.JSONEq(t, `{"error":"invalid_target","error_description":"resource does not identify this MCP server (expected \"`+f.resource+`\")"}`, w.Body.String())
			} else {
				requireTokenResponse(t, w, tc.status, tc.body)
			}

			row := requireOneIDJAGRow(t, rec, tc.what, assertion, jti)
			requireRefusedBy(t, row, tc.who, tc.detail)
			require.Equal(t, f.issuerID.String(), row.UserSessionIssuerID)
			if tc.subject {
				require.Equal(t, urn.NewUserSubject(mockidp.MockUserID).String(), row.SubjectURN)
			} else {
				require.Empty(t, row.SubjectURN)
			}
		})
	}
}

// The organization's identity provider being down is the provider's to fix, and the
// assertion stays unspent.
func TestIDJAGExchangeRecordsKeyOutage(t *testing.T) {
	t.Parallel()

	ctx, f := newIDJAGOutageFixture(t, true)
	rec := mcpconnectionattemptstest.New(t)
	f.ti.service.SetConnectionRecorder(rec.Recorder)
	f.down.Store(true)
	jti := uuid.NewString()
	assertion := f.signer.sign(t, idJAGOutageIssuer, f.resource, f.resource, f.clientID, mockidp.MockUserEmail, jti)

	w := postIDJAGForm(t, ctx, f.ti, f.slug, idJAGForm(f.clientID, assertion))
	requireIDJAGOutage(t, w)
	require.JSONEq(t, `{"error":"temporarily_unavailable","error_description":"the service is temporarily unavailable"}`, w.Body.String())

	row := requireOneIDJAGRow(t, rec, mcpconnectionattempts.CodeIDPTransientFailure, assertion, jti)
	requireRefusedBy(t, row, mcpconnectionattempts.WhoProvider, "assertion_key_unresolvable")
	require.Empty(t, row.ProviderHost)
}

// A trust link that is missing for the endpoint's organization is the admin's to add.
func TestIDJAGExchangeRecordsMissingTrust(t *testing.T) {
	t.Parallel()

	ctx, ti, assertionSigner, jwksURL := newIDJAGTestService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(ctx, ti.logger, toolset.McpSlug.String, "mcp")
	require.NoError(t, err)

	otherOrganizationID := "org_" + uuid.NewString()
	require.NoError(t, orgsrepo.New(ti.conn).CreateOrganizationMetadata(ctx, orgsrepo.CreateOrganizationMetadataParams{
		ID: otherOrganizationID, Name: "Other organization", Slug: "other-" + uuid.NewString(),
	}))
	const upstreamIssuer = "https://other-identity.example.test"
	otherIssuer := createTrustedIDJAGIssuer(t, ctx, ti, otherOrganizationID, upstreamIssuer, jwksURL)
	client := seedCIMDClient(t, ctx, ti, otherIssuer.ID, codexCIMDClientID)
	endpoint.UserSessionIssuerID = otherIssuer.ID
	resource, err := endpoint.RootURL(ti.serverURL.String())
	require.NoError(t, err)
	jti := uuid.NewString()
	assertion := assertionSigner.sign(t, upstreamIssuer, resource, resource, client.ClientID, mockidp.MockUserEmail, jti)
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/token", strings.NewReader(idJAGForm(client.ClientID, assertion).Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeToken(w, req.WithContext(ctx), endpoint))
	requireTokenResponse(t, w, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"assertion is invalid"}`)

	row := requireOneIDJAGRow(t, rec, mcpconnectionattempts.CodeIssuerNotTrusted, assertion, jti)
	requireRefusedBy(t, row, mcpconnectionattempts.WhoAdmin, "trusted_issuer_unavailable")
	require.Equal(t, authCtx.ActiveOrganizationID, row.OrganizationID)
}

// Client refusals on the ID-JAG grant write the rows an authorization code's do; a
// client that never resolved writes none.
func TestIDJAGExchangeRecordsClientRefusals(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mode      admission.Mode
		clientID  func(f idJAGClientFixture) string
		failFetch bool
		block     string
		status    int
		body      string
		what      mcpconnectionattempts.Code
		app       func(f idJAGClientFixture) string
	}{
		"presets refuses an unlisted document": {
			mode: admission.ModePresets, clientID: func(f idJAGClientFixture) string { return f.ds.clientID },
			status: http.StatusBadRequest, body: `{"error":"invalid_client","error_description":"this client ID metadata document URL is not permitted by the server's client policy; the server operator must allow it"}`,
			what: mcpconnectionattempts.CodeCIMDAdmissionDenied,
			app: func(f idJAGClientFixture) string {
				return mcpconnectionattempts.AppOf(f.ds.clientID, mcpconnectionattempts.ClientIDNotAdmitted).String()
			},
		},
		"document fetch fails": {
			mode: admission.ModeOpen, clientID: func(f idJAGClientFixture) string { return f.ds.clientID }, failFetch: true,
			status: http.StatusServiceUnavailable, body: `{"error":"temporarily_unavailable","error_description":"failed to fetch client metadata document"}`,
			what: mcpconnectionattempts.CodeCIMDFetchFailed,
			app: func(f idJAGClientFixture) string {
				return mcpconnectionattempts.AppOf(f.ds.clientID, mcpconnectionattempts.ClientIDNotAdmitted).String()
			},
		},
		"blocked AI tool": {
			mode: admission.ModeOpen, clientID: func(idJAGClientFixture) string { return codexCIMDClientID }, block: "codex",
			status: http.StatusBadRequest,
			what:   mcpconnectionattempts.CodeAIToolBlocked,
			app: func(idJAGClientFixture) string {
				return mcpconnectionattempts.AppOf(codexCIMDClientID, mcpconnectionattempts.ClientIDAdmitted).String()
			},
		},
		"client without a metadata document": {
			mode: admission.ModeOpen, clientID: func(f idJAGClientFixture) string { return f.dcrClientID },
			status: http.StatusBadRequest, body: `{"error":"unauthorized_client","error_description":"this client is not allowed to use the ID-JAG grant"}`,
			what: mcpconnectionattempts.CodeClientNotCIMD,
			app:  func(idJAGClientFixture) string { return mcpconnectionattempts.AppDCR.String() },
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newIDJAGClientFixture(t)
			if tc.failFetch {
				f.ds.set(t, func(ds *cimdDocServer) { ds.status = http.StatusInternalServerError })
			}
			setOrganizationIssuerAdmissionMode(t, f.ctx, f.ti, f.issuer, tc.mode)
			if tc.block != "" {
				blockTarget(t, f.ctx, f.ti, f.issuer.OrganizationID.String, tc.block)
			}
			clientID := tc.clientID(f)
			jti := uuid.NewString()
			assertion := f.signer.sign(t, "https://identity.example.test", f.resource, f.resource, clientID, mockidp.MockUserEmail, jti)

			w := postIDJAGForm(t, f.ctx, f.ti, f.slug, idJAGForm(clientID, assertion))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.body != "" {
				require.JSONEq(t, tc.body, w.Body.String())
			}

			row := requireOneIDJAGRow(t, f.rec, tc.what, assertion, jti)
			require.Equal(t, tc.app(f), row.AppKey)
			require.Empty(t, row.SubjectURN)
		})
	}
}

func TestIDJAGExchangeUnresolvedClientWritesNoRow(t *testing.T) {
	t.Parallel()

	f := newIDJAGClientFixture(t)
	w := postIDJAGForm(t, f.ctx, f.ti, f.slug, idJAGForm("unknown-client", "unused.assertion.value"))
	requireTokenResponse(t, w, http.StatusBadRequest, `{"error":"invalid_client","error_description":"client authentication failed"}`)

	form := idJAGForm(codexCIMDClientID, "unused.assertion.value")
	form.Set("client_secret", "one")
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+f.slug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(codexCIMDClientID, "two")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", f.slug)
	w = httptest.NewRecorder()
	oops.OAuthErrHandle(testenv.NewLogger(t), f.ti.service.HandleToken).ServeHTTP(w, req.WithContext(context.WithValue(f.ctx, chi.RouteCtxKey, routeCtx)))
	requireTokenResponse(t, w, http.StatusBadRequest, `{"error":"invalid_request","error_description":"invalid client authentication request"}`)

	f.rec.RequireNothing()
}

// idJAGClientFixture is a toolset on a trusted ID-JAG issuer with a client
// metadata document server and a DCR client, recording connection rows.
type idJAGClientFixture struct {
	ctx         context.Context //nolint:containedctx // test fixture
	ti          *testInstance
	signer      idJAGTestAssertion
	ds          *cimdDocServer
	issuer      usersessionsrepo.UserSessionIssuer
	slug        string
	resource    string
	dcrClientID string
	rec         *mcpconnectionattemptstest.Recording
}

func newIDJAGClientFixture(t *testing.T) idJAGClientFixture {
	t.Helper()
	ctx, ti, signer, ds, issuer, slug, resource := newIDJAGAdmissionFixture(t)
	seedCIMDClient(t, ctx, ti, issuer.ID, codexCIMDClientID)
	dcr := createDCRClient(t, ctx, ti, issuer.ID)
	rec := mcpconnectionattemptstest.New(t)
	ti.service.SetConnectionRecorder(rec.Recorder)
	return idJAGClientFixture{ctx: ctx, ti: ti, signer: signer, ds: ds, issuer: issuer, slug: slug, resource: resource, dcrClientID: dcr.ClientID, rec: rec}
}

// A client looping on one bad assertion is held to a few rows, and the rows still count every request.
func TestIDJAGExchangeRepeatedRefusalIsRateLimited(t *testing.T) {
	t.Parallel()

	ctx, f := newIDJAGOutageFixture(t, true)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	recorder := mcpconnectionattempts.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, mcpconnectionattemptstest.LogsOn)
	f.ti.service.SetConnectionRecorder(recorder)

	const requests = 12
	assertion := f.signer.sign(t, idJAGOutageIssuer, "https://elsewhere.example.test", f.resource, f.clientID, mockidp.MockUserEmail, uuid.NewString())
	for range requests {
		w := postIDJAGForm(t, ctx, f.ti, f.slug, idJAGForm(f.clientID, assertion))
		requireTokenResponse(t, w, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"assertion is invalid"}`)
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	require.NoError(t, recorder.Shutdown(shutdownCtx))

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, qerr := conn.Query(ctx, `SELECT repeats FROM mcp_connection_events WHERE user_session_issuer_id = $1 AND reason = $2 AND is_final`, f.issuerID.String(), mcpconnectionattempts.CodeAssertionInvalid.String())
		if !assert.NoError(c, qerr) {
			return
		}
		defer o11y.NoLogDefer(rows.Close)
		written, events := 0, 0
		for rows.Next() {
			var repeats uint32
			assert.NoError(c, rows.Scan(&repeats))
			written++
			events += 1 + int(repeats)
		}
		assert.Less(c, written, requests, "repeats fold into fewer rows")
		assert.Equal(c, requests, events, "every request is still counted")
	}, 30*time.Second, 200*time.Millisecond)
}
