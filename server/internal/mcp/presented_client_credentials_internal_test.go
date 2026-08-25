package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
)

// newFormRequest builds a form-encoded POST carrying the given fields.
func newFormRequest(t *testing.T, fields map[string]string) *http.Request {
	t.Helper()

	form := url.Values{}
	for k, v := range fields {
		form.Set(k, v)
	}
	r := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// The credential extractor labels what a request presented, including the
// combinations RFC 6749 §2.3 forbids, so the credential event logs can show a
// misconfigured client rather than a bare rejection.
func TestExtractClientCredentials_Labels(t *testing.T) {
	t.Parallel()

	assertion := "eyJ.eyJ.sig"
	for _, tc := range []struct {
		name   string
		form   map[string]string
		basic  [2]string
		method string
		id     string
	}{
		{name: "public", form: map[string]string{"client_id": "c"}, method: "none", id: "c"},
		{name: "post", form: map[string]string{"client_id": "c", "client_secret": "s"}, method: "client_secret_post", id: "c"},
		{name: "basic", basic: [2]string{"c", "s"}, method: "client_secret_basic", id: "c"},
		{name: "assertion", form: map[string]string{"client_id": "c", "client_assertion": assertion, "client_assertion_type": clientauth.AssertionType}, method: "private_key_jwt", id: "c"},
		{name: "assertion without client_id", form: map[string]string{"client_assertion": assertion, "client_assertion_type": clientauth.AssertionType}, method: "private_key_jwt", id: ""},
		{name: "assertion plus secret", form: map[string]string{"client_id": "c", "client_secret": "s", "client_assertion": assertion}, method: "multiple", id: "c"},
		{name: "basic plus assertion", basic: [2]string{"c", "s"}, form: map[string]string{"client_assertion": assertion}, method: "multiple", id: "c"},
		{name: "basic plus form", basic: [2]string{"c", "s"}, form: map[string]string{"client_id": "other"}, method: "multiple", id: "c"},
		{name: "nothing", method: "none", id: ""},
	} {
		r := newFormRequest(t, tc.form)
		if tc.basic[0] != "" {
			r.SetBasicAuth(tc.basic[0], tc.basic[1])
		}
		require.NoError(t, r.ParseForm())
		creds := extractClientCredentials(r)
		require.Equal(t, tc.method, creds.method, tc.name)
		require.Equal(t, tc.id, creds.clientID, tc.name)
	}
}
