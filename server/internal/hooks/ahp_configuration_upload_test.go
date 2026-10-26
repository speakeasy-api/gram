package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	ahp "github.com/agenthooksprotocol/go-sdk"
	"github.com/agenthooksprotocol/go-sdk/capability"
	ahpclient "github.com/agenthooksprotocol/go-sdk/client"
	"github.com/agenthooksprotocol/go-sdk/content"
	"github.com/agenthooksprotocol/go-sdk/event"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	domainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/netingress"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

func TestAHPConfigurationNativeUpload(t *testing.T) {
	t.Parallel()
	for _, header := range []string{"Authorization", "Gram-Key"} {
		t.Run(header, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			auth := hookAuthContext(t, ctx)
			ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
			// Hooks fail open by default; record the explicit fail-closed choice.
			failOpenFlag := featuresrepo.EnableFeatureParams{OrganizationID: auth.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureHooksFailOpen)}
			_, err := featuresrepo.New(ti.conn).EnableFeature(ctx, failOpenFlag)
			require.NoError(t, err)
			_, err = featuresrepo.New(ti.conn).DeleteFeature(ctx, featuresrepo.DeleteFeatureParams{OrganizationID: failOpenFlag.OrganizationID, FeatureName: failOpenFlag.FeatureName})
			require.NoError(t, err)
			handler, err := ti.service.ahpHandler()
			require.NoError(t, err)
			uploadHandler := ti.service.ahpUploadHandler()
			value := "example"
			if header == "Authorization" {
				value = "Bearer example"
			}
			var uploadCalls, eventCalls, discoveryCalls int
			var delivered ahp.UserMessageInboundEvent
			loseUpload := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, value, r.Header.Get(header))
				assert.Equal(t, "chosen", r.Header.Get("Gram-Project"))
				// Even an attacker-controlled request host cannot influence discovery.
				r.Host = "attacker.invalid"
				r.Header.Set("X-Forwarded-Host", "attacker.invalid")
				if r.URL.Path == ahpUploadPath {
					uploadCalls++
					uploadHandler.ServeHTTP(w, r)
					return
				}
				raw, readErr := io.ReadAll(r.Body)
				assert.NoError(t, readErr)
				r.Body = io.NopCloser(bytes.NewReader(raw))
				var envelope struct {
					Method string `json:"method"`
					Params struct {
						Event json.RawMessage `json:"event"`
					} `json:"params"`
				}
				assert.NoError(t, json.Unmarshal(raw, &envelope))
				if envelope.Method == "hooks/configuration" {
					discoveryCalls++
				}
				if envelope.Method == "hooks/intercept" {
					eventCalls++
					assert.NoError(t, json.Unmarshal(envelope.Params.Event, &delivered))
					if loseUpload {
						ref := delivered.Message.Messages[0].Parts[0].AttachmentBodyPart.Value.Body.Ref
						assert.NoError(t, ti.service.cache.Delete(ctx, ahpUploadCacheKey(*auth.ProjectID, strings.TrimPrefix(ref, ahpUploadRefPrefix))))
					}
				}
				handler.ServeHTTP(w, r)
			}))
			defer srv.Close()
			base, err := url.Parse(srv.URL)
			require.NoError(t, err)
			ti.service.serverURL = base
			ti.service.orgHosts = orghost.New(orghost.Config{ServerURL: base})
			registrationJSON, err := json.Marshal(map[string]any{"protocolVersion": "draft", "hooks": []any{map[string]any{
				"id": "ai.speakeasy.ahp", "transport": map[string]any{"type": "http", "url": srv.URL + "/ahp", "headers": map[string]string{header: value, "Gram-Project": "chosen"}},
			}}})
			require.NoError(t, err)
			registration := ahp.ParseRegistration(registrationJSON)
			require.True(t, registration.OK, "%v", registration.Diagnostics)
			options := ahpclient.Options{
				Source: "urn:test:discovery", Harness: ahp.Harness{Name: "test", Version: "1"},
				Events: capability.Host{
					UserMessageInbound: capability.UserMessageInbound{Deny: true},
					ToolBefore:         capability.ToolBefore{Deny: true},
					SessionStart:       capability.SessionStart{Observe: true},
					SessionEnd:         capability.SessionEnd{Observe: true},
					TurnEnd:            capability.TurnEnd{Observe: true},
					ModelResponseAfter: capability.ModelResponseAfter{Observe: true},
					ToolAfter:          capability.ToolAfter{Observe: true},
				},
				MaxContentBytes: 2 * ahpMaxUploadBytes,
				Content:         ahpclient.ContentOptions{AllowLoopbackHTTP: true, AuthorizeContent: func(context.Context, ahpclient.ContentAuthorization) (bool, error) { return true, nil }},
			}
			// Upload-bearing discovery is not implicitly authorized.
			for _, approval := range []string{"missing", "rejected", "error"} {
				unapprovedOptions := options
				if approval != "missing" {
					unapprovedOptions.AuthorizeConfiguration = func(context.Context, *ahp.Configuration) (bool, error) {
						if approval == "error" {
							return false, errors.New("approval unavailable")
						}
						return false, nil
					}
				}
				unapproved, err := ahpclient.New(registration.Value, unapprovedOptions)
				require.NoError(t, err)
				for _, name := range []event.Type{event.UserMessageInbound, event.ToolBefore, event.SessionStart, event.SessionEnd, event.TurnEnd, event.ModelResponseAfter, event.ToolAfter} {
					require.False(t, unapproved.Subscribes(name), "%s approval must skip the entire backend: %s", approval, name)
				}
				require.NoError(t, unapproved.Close())
			}
			options.AuthorizeConfiguration = func(_ context.Context, config *ahp.Configuration) (bool, error) {
				raw, marshalErr := json.Marshal(config)
				require.NoError(t, marshalErr)
				require.NotContains(t, string(raw), value)
				require.NotContains(t, string(raw), "attacker.invalid")
				var fields struct {
					Subscriptions []struct {
						Upload struct {
							Endpoint  string          `json:"endpoint"`
							MaxBytes  int             `json:"maxBytes"`
							TimeoutMs int             `json:"timeoutMs"`
							Auth      json.RawMessage `json:"auth"`
						} `json:"upload"`
					} `json:"subscriptions"`
				}
				require.NoError(t, json.Unmarshal(raw, &fields))
				require.Len(t, fields.Subscriptions, 2)
				for _, sub := range fields.Subscriptions {
					require.Equal(t, srv.URL+ahpUploadPath, sub.Upload.Endpoint)
					require.Equal(t, ahpMaxUploadBytes, sub.Upload.MaxBytes)
					require.Equal(t, 10000, sub.Upload.TimeoutMs)
					require.Empty(t, sub.Upload.Auth)
				}
				return true, nil
			}
			client, err := ahpclient.New(registration.Value, options)
			require.NoError(t, err)
			defer func() { require.NoError(t, client.Close()) }()
			require.True(t, client.Subscribes(event.UserMessageInbound))
			// Existing explicit registrations bypass discovery and its authorization hook.
			var explicit map[string]any
			require.NoError(t, json.Unmarshal(registrationJSON, &explicit))
			configJSON, err := json.Marshal(ahpConfiguration(false, srv.URL+ahpUploadPath))
			require.NoError(t, err)
			var config map[string]any
			require.NoError(t, json.Unmarshal(configJSON, &config))
			hooks, ok := explicit["hooks"].([]any)
			require.True(t, ok)
			backend, ok := hooks[0].(map[string]any)
			require.True(t, ok)
			backend["subscriptions"] = config["subscriptions"]
			explicitJSON, err := json.Marshal(explicit)
			require.NoError(t, err)
			explicitRegistration := ahp.ParseRegistration(explicitJSON)
			require.True(t, explicitRegistration.OK, "%v", explicitRegistration.Diagnostics)
			explicitOptions := options
			explicitOptions.AuthorizeConfiguration = nil
			before := discoveryCalls
			explicitClient, err := ahpclient.New(explicitRegistration.Value, explicitOptions)
			require.NoError(t, err)
			require.True(t, explicitClient.Subscribes(event.UserMessageInbound))
			require.NoError(t, explicitClient.Close())
			require.Equal(t, before, discoveryCalls)
			dispatch := func(raw []byte) *ahpclient.Result {
				input := ahpTestInbound()
				delete(input, "source")
				delete(input, "type")
				delete(input, "harness")
				input["message"] = map[string]any{"sender": "user", "channel": "cli", "messages": []any{map[string]any{
					"id": "message", "role": "user", "parts": []any{map[string]any{"id": "notes", "kind": "attachment", "mediaType": "application/xml", "selection": "metadata"}},
				}}}
				result, dispatchErr := client.Dispatch(ctx, event.UserMessageInbound, input, ahpclient.WithContentSource("/message/messages/0/parts/0", content.NewAttachment(raw)))
				require.NoError(t, dispatchErr)
				t.Cleanup(func() { require.NoError(t, result.Close()) })
				return result
			}
			result := dispatch([]byte("discovered attachment evidence"))
			require.Empty(t, result.Diagnostics)
			require.Equal(t, 1, uploadCalls)
			require.Equal(t, 1, eventCalls)
			attachments, err := ti.service.ahpPromptAttachments(ctx, &delivered)
			require.NoError(t, err)
			require.Len(t, attachments, 1)
			require.Equal(t, "discovered attachment evidence", attachments[0].Content)
			oversized := dispatch([]byte(strings.Repeat("x", ahpMaxUploadBytes+1)))
			require.NotEmpty(t, oversized.Diagnostics)
			require.Equal(t, 1, uploadCalls, "advertised size limit rejects before upload")
			require.Equal(t, 1, eventCalls, "fail-closed content failure must not dispatch")
			// Simulate expiry/eviction after a valid receipt but before event handling.
			loseUpload = true
			missing := dispatch([]byte("staged evidence must not disappear silently"))
			require.NotEmpty(t, missing.Diagnostics)
			require.Empty(t, missing.AcceptedResponses)
			require.Equal(t, ahp.InterceptRequestParamsStatePermissionDeny, missing.Permission, "fail-closed must deny execution after evidence loss")
			require.True(t, missing.Diagnostics[0].FailClosed)
			// Fail-open remains an explicit organization policy, not a successful scan.
			_, err = featuresrepo.New(ti.conn).EnableFeature(ctx, featuresrepo.EnableFeatureParams{OrganizationID: auth.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureHooksFailOpen)})
			require.NoError(t, err)
			require.NoError(t, client.Close())
			client, err = ahpclient.New(registration.Value, options)
			require.NoError(t, err)
			missing = dispatch([]byte("fail-open evidence loss remains diagnostic"))
			require.NotEmpty(t, missing.Diagnostics)
			require.Empty(t, missing.AcceptedResponses)
			require.NotEqual(t, ahp.InterceptRequestParamsStatePermissionDeny, missing.Permission)
			require.False(t, missing.Interrupted)
			require.False(t, missing.Diagnostics[0].FailClosed)
		})
	}
}

func TestAHPConfigurationUploadRequestOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		surface requestorigin.Surface
		base    string
	}{
		{"platform", requestorigin.SurfacePlatform, "https://alternate.example.test"},
		{"custom domain", requestorigin.SurfaceCustomDomain, "https://custom.example.test"},
		{"private origin context", requestorigin.SurfacePrivateNetwork, "https://private.example.test"},
		{"configured fallback", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			auth := hookAuthContext(t, ctx)
			ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
			// The stored org default must not move uploads away from the request origin.
			useTestOrgHosts(t, ctx, ti, pgtype.Text{String: testPlatformBaseURL, Valid: true})
			handler, err := ti.service.ahpHandler()
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodPost, "https://forged.invalid/ahp", strings.NewReader(`{"jsonrpc":"2.0","id":"discovery","method":"hooks/configuration","params":{"protocolVersion":"draft"}}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Forwarded-Host", "forwarded.invalid")
			request.Header.Set("Forwarded", "host=forwarded.invalid;proto=http")
			for key, value := range ahpAuthHeaders {
				request.Header.Set(key, value)
			}
			if tc.base != "" {
				request = request.WithContext(requestorigin.WithContext(request.Context(), requestorigin.Origin{Surface: tc.surface, BaseURL: tc.base, OrganizationID: auth.ActiveOrganizationID}))
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			want := tc.base
			if want == "" {
				want = ti.service.serverURL.String()
			}
			require.Contains(t, response.Body.String(), want+ahpUploadPath)
			require.NotContains(t, response.Body.String(), testPlatformBaseURL)
			require.NotContains(t, response.Body.String(), "forged.invalid")
			require.NotContains(t, response.Body.String(), "forwarded.invalid")
		})
	}
	// Private ingress currently exposes only MCP routes. The context case above
	// verifies origin selection, not an AHP route on the private listener.
	require.False(t, netingress.IsPrivateRoute(http.MethodPost, "/ahp"))
	require.False(t, netingress.IsPrivateRoute(http.MethodPost, ahpUploadPath))
}

func TestAHPConfigurationUploadBasePath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		base, want string
		invalid    bool
	}{
		{"https://api.example.test/prefix", "https://api.example.test/prefix/ahp/uploads", false},
		{"https://api.example.test/prefix/", "https://api.example.test/prefix/ahp/uploads", false},
		{"https://api.example.test/api%2Fv1/space%20here?ignored=1#ignored", "https://api.example.test/api%2Fv1/space%20here/ahp/uploads", false},
		{"https://userinfo@api.example.test/prefix", "", true},
	} {
		t.Run(tc.base, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestHooksService(t)
			base, err := url.Parse(tc.base)
			require.NoError(t, err)
			ti.service.serverURL = base
			ti.service.orgHosts = nil
			config, err := ti.service.ahpResolveConfiguration(ctx)
			if tc.invalid {
				require.Error(t, err)
				require.Nil(t, config)
				return
			}
			require.NoError(t, err)
			raw, err := json.Marshal(config)
			require.NoError(t, err)
			var fields struct {
				Subscriptions []struct {
					Upload struct {
						Endpoint string `json:"endpoint"`
					} `json:"upload"`
				} `json:"subscriptions"`
			}
			require.NoError(t, json.Unmarshal(raw, &fields))
			require.Len(t, fields.Subscriptions, 2)
			for _, sub := range fields.Subscriptions {
				require.Equal(t, tc.want, sub.Upload.Endpoint)
			}
			require.Equal(t, tc.base, base.String(), "resolution must not mutate shared configuration")
		})
	}
}

func TestAHPConfigurationOriginMiddleware(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	auth := hookAuthContext(t, ctx)
	ti.service.auth = &ahpTestAuthorizer{authCtx: auth}
	domains := domainsrepo.New(ti.conn)
	domain, err := domains.CreateCustomDomain(ctx, domainsrepo.CreateCustomDomainParams{OrganizationID: auth.ActiveOrganizationID, Domain: "custom.example.test", IpAllowlist: []string{}})
	require.NoError(t, err)
	_, err = domains.UpdateCustomDomain(ctx, domainsrepo.UpdateCustomDomainParams{ID: domain.ID, Verified: true, Activated: true})
	require.NoError(t, err)
	handler, err := ti.service.ahpHandler()
	require.NoError(t, err)
	handler = customdomains.Middleware(ti.service.logger, ti.conn, "prod", ti.service.serverURL, map[string]string{"alternate.example.test": "https://alternate.example.test"})(handler)
	for _, tc := range []struct {
		host, want string
		status     int
	}{
		{"alternate.example.test", "https://alternate.example.test/ahp/uploads", http.StatusOK},
		{"custom.example.test", "https://custom.example.test/ahp/uploads", http.StatusOK},
		{"unknown.invalid", "", http.StatusForbidden},
	} {
		request := httptest.NewRequest(http.MethodPost, "https://"+tc.host+"/ahp", strings.NewReader(`{"jsonrpc":"2.0","id":"discovery","method":"hooks/configuration","params":{"protocolVersion":"draft"}}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-Host", "forged.invalid")
		for key, value := range ahpAuthHeaders {
			request.Header.Set(key, value)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, tc.status, response.Code, tc.host)
		if tc.want != "" {
			require.Contains(t, response.Body.String(), tc.want)
		}
		require.NotContains(t, response.Body.String(), "forged.invalid")
	}
}
