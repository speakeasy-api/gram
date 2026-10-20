package googlechat

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func credentials(t *testing.T) toolconfig.ToolCallEnv {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]string{"type": "service_account", "client_email": "app@example-project.iam.gserviceaccount.com", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})), "token_uri": "https://attacker.invalid/steal"})
	require.NoError(t, err)
	env := toolconfig.NewCaseInsensitiveEnv()
	env.Set(ServiceAccountEnvVar, string(raw))
	return toolconfig.ToolCallEnv{SystemEnv: env}
}

func TestOperations(t *testing.T) {
	t.Parallel()
	env := credentials(t)
	const requestID = "1ee04a56-5969-4ac2-959e-019c00921bc6"
	tests := []struct {
		name                       string
		factory                    func(*guardian.HTTPClient) core.PlatformToolExecutor
		input, method, path, scope string
		query                      url.Values
		body                       string
	}{
		{"send", NewSendMessageTool, `{"space":"spaces/AAA","text":"hello","request_id":"` + requestID + `"}`, "POST", "/v1/spaces/AAA/messages", botScope, url.Values{"requestId": {requestID}}, `{"text":"hello"}`},
		{"reply", NewReplyToThreadTool, `{"thread":"spaces/AAA/threads/T.1","text":"hello","request_id":"` + requestID + `"}`, "POST", "/v1/spaces/AAA/messages", botScope, url.Values{"requestId": {requestID}, "messageReplyOption": {"REPLY_MESSAGE_OR_FAIL"}}, `{"text":"hello","thread":{"name":"spaces/AAA/threads/T.1"}}`},
		{"update", NewUpdateMessageTool, `{"name":"spaces/AAA/messages/M.1","text":"hello"}`, "PATCH", "/v1/spaces/AAA/messages/M.1", botScope, url.Values{"updateMask": {"text"}}, `{"name":"spaces/AAA/messages/M.1","text":"hello"}`},
		{"delete", NewDeleteMessageTool, `{"name":"spaces/AAA/messages/client-custom"}`, "DELETE", "/v1/spaces/AAA/messages/client-custom", botScope, url.Values{}, ""},
		{"spaces", NewListSpacesTool, `{}`, "GET", "/v1/spaces", botScope, url.Values{"pageSize": {"100"}}, ""},
		{"get", NewGetMessageTool, `{"name":"spaces/AAA/messages/M"}`, "GET", "/v1/spaces/AAA/messages/M", historyScope, url.Values{}, ""},
		{"messages", NewListMessagesTool, `{"space":"spaces/AAA","thread":"spaces/AAA/threads/T","page_size":20,"page_token":"opaque+/="}`, "GET", "/v1/spaces/AAA/messages", historyScope, url.Values{"pageSize": {"20"}, "pageToken": {"opaque+/="}, "filter": {"thread.name = spaces/AAA/threads/T"}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			h := &guardian.HTTPClient{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() == tokenURL {
					require.Equal(t, "POST", r.Method)
					require.NoError(t, r.ParseForm())
					require.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", r.Form.Get("grant_type"))
					assertion := strings.Split(r.Form.Get("assertion"), ".")
					require.Len(t, assertion, 3)
					raw, err := base64.RawURLEncoding.DecodeString(assertion[1])
					require.NoError(t, err)
					var claims map[string]any
					require.NoError(t, json.Unmarshal(raw, &claims))
					require.Equal(t, tt.scope, claims["scope"])
					require.Equal(t, tokenURL, claims["aud"])
					require.NotContains(t, claims, "sub")
					return response(200, `{"access_token":"mock-token","token_type":"Bearer","expires_in":3600}`), nil
				}
				require.Equal(t, "chat.googleapis.com", r.URL.Host)
				require.Equal(t, tt.method, r.Method)
				require.Equal(t, tt.path, r.URL.Path)
				require.Equal(t, tt.query, r.URL.Query())
				require.Equal(t, "Bearer mock-token", r.Header.Get("Authorization"))
				raw, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				if tt.body == "" {
					require.Empty(t, raw)
				} else {
					require.JSONEq(t, tt.body, string(raw))
				}
				return response(200, `{"name":"spaces/AAA/messages/M","nextPageToken":"next-page"}`), nil
			})}
			tool := tt.factory(h)
			// A caller retries with the same payload: requestId remains unchanged.
			for range 2 {
				var out bytes.Buffer
				require.NoError(t, tool.Call(context.Background(), env, strings.NewReader(tt.input), &out))
				require.Contains(t, out.String(), `"nextPageToken":"next-page"`)
			}
			require.Equal(t, 4, calls)
		})
	}
}

func TestInvalidInputNeverAuthenticates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		factory func(*guardian.HTTPClient) core.PlatformToolExecutor
		input   string
	}{
		{NewSendMessageTool, `{"space":"spaces/A/../B","text":"hi","request_id":"bad"}`},
		{NewSendMessageTool, `{"space":"spaces/A","text":"hi"}`},
		{NewSendMessageTool, `{"space":"spaces/A","text":"","request_id":"1ee04a56-5969-4ac2-959e-019c00921bc6"}`},
		{NewReplyToThreadTool, `{"thread":"spaces/A/threads/T?evil=1","text":"hi"}`},
		{NewGetMessageTool, `{"name":"https://attacker.invalid/"}`},
		{NewDeleteMessageTool, `{"name":"spaces/A/messages/%2e%2e"}`},
		{NewDeleteMessageTool, `{"name":"spaces/A/messages/.."}`},
		{NewUpdateMessageTool, `{"name":"spaces/A/messages/M","text":"  "}`},
		{NewListMessagesTool, `{"space":"spaces/A","thread":"spaces/B/threads/T"}`},
		{NewListMessagesTool, `{"space":"spaces/A","thread":"spaces/A/threads/T OR true"}`},
		{NewListMessagesTool, `{"space":"spaces/A","page_size":101}`},
		{NewListSpacesTool, `{"page_size":-1}`},
		{NewListSpacesTool, `{} {}`},
		{NewListSpacesTool, `{"unknown":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			h := &guardian.HTTPClient{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected HTTP call"); return nil, nil })}
			require.Error(t, tt.factory(h).Call(context.Background(), toolconfig.ToolCallEnv{}, strings.NewReader(tt.input), io.Discard))
		})
	}
}

func TestCredentialAndUpstreamErrorsAreSanitized(t *testing.T) {
	t.Parallel()
	env := credentials(t)
	for _, stage := range []string{"token", "api", "redirect"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			calls := 0
			h := &guardian.HTTPClient{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() == tokenURL && stage == "api" {
					return response(200, `{"access_token":"mock-token","token_type":"Bearer","expires_in":3600}`), nil
				}
				if stage == "redirect" {
					res := response(302, "")
					res.Header.Set("Location", "https://attacker.invalid/steal")
					return res, nil
				}
				return response(403, `{"error":"sensitive-private-key-and-assertion"}`), nil
			})}
			err := NewListSpacesTool(h).Call(context.Background(), env, strings.NewReader(`{}`), io.Discard)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "sensitive")
			require.NotContains(t, err.Error(), "mock-token")
			if stage == "api" {
				require.Equal(t, 2, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
	for _, raw := range []string{"", `{"type":"authorized_user","private_key":"secret"}`, `{"type":"service_account","client_email":"app@example-project.iam.gserviceaccount.com","private_key":"secret"}`} {
		e := toolconfig.NewCaseInsensitiveEnv()
		e.Set(ServiceAccountEnvVar, raw)
		h := &guardian.HTTPClient{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected HTTP request"); return nil, nil })}
		err := NewListSpacesTool(h).Call(context.Background(), toolconfig.ToolCallEnv{SystemEnv: e}, strings.NewReader(`{}`), io.Discard)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
}

func TestDescriptorContracts(t *testing.T) {
	t.Parallel()
	for _, factory := range []func(*guardian.HTTPClient) core.PlatformToolExecutor{NewSendMessageTool, NewReplyToThreadTool, NewUpdateMessageTool, NewDeleteMessageTool, NewListSpacesTool, NewGetMessageTool, NewListMessagesTool} {
		d := factory(nil).Descriptor()
		require.Equal(t, "googlechat", d.SourceSlug)
		require.True(t, d.Managed)
		require.True(t, *d.Annotations.IdempotentHint)
		var schema map[string]any
		require.NoError(t, json.Unmarshal(d.InputSchema, &schema))
		require.Equal(t, "object", schema["type"])
		if d.HandlerName == "send_message" || d.HandlerName == "reply_to_thread" {
			require.Contains(t, schema["required"], "request_id")
		}
	}
}
