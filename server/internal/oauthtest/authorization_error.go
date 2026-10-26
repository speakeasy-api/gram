package oauthtest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"

	"github.com/speakeasy-api/gram/server/internal/oauthui"
)

// RequireAuthorizationErrorCallback checks whether an error automatically
// redirects or requires an explicit click and returns the callback for protocol
// assertions. HTML is parsed as a browser would parse it, including entities.
func RequireAuthorizationErrorCallback(t *testing.T, response *httptest.ResponseRecorder, status int) *url.URL {
	t.Helper()
	require.Equal(t, status, response.Code, response.Body.String())
	callback := response.Header().Get("Location")
	if status == http.StatusBadRequest {
		require.Empty(t, callback)
		require.Empty(t, response.Header().Get("Refresh"))
		require.Equal(t, "text/html; charset=utf-8", response.Header().Get("Content-Type"))
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		require.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))
		doc, err := html.Parse(strings.NewReader(response.Body.String()))
		require.NoError(t, err)
		var callbacks []string
		for node := range doc.Descendants() {
			if node.Type != html.ElementNode {
				continue
			}
			if node.Data == "script" {
				requireNonNavigatingScript(t, node, response.Header().Get("Content-Security-Policy"))
			}
			for _, attr := range node.Attr {
				if node.Data == "a" && attr.Key == "href" {
					callbacks = append(callbacks, attr.Val)
				}
				if node.Data == "meta" && attr.Key == "http-equiv" {
					require.NotEqual(t, "refresh", strings.ToLower(attr.Val))
				}
			}
		}
		require.Len(t, callbacks, 1)
		callback = callbacks[0]
	}
	require.NotEmpty(t, callback)
	parsed, err := url.Parse(callback)
	require.NoError(t, err)
	return parsed
}

// requireNonNavigatingScript allows only the reviewed copy button script,
// admitted by the response's CSP nonce. Matching the exact text is an allowlist:
// a script that could move the browser to the callback cannot slip past it, so
// the page leaves that step to the person.
func requireNonNavigatingScript(t *testing.T, script *html.Node, csp string) {
	t.Helper()
	nonce := ""
	for _, attr := range script.Attr {
		require.NotEqual(t, "src", attr.Key, "authorization error scripts must be inline")
		if attr.Key == "nonce" {
			nonce = attr.Val
		}
	}
	require.NotEmpty(t, nonce)
	require.Contains(t, csp, "script-src 'nonce-"+nonce+"'")
	var body strings.Builder
	for child := range script.Descendants() {
		if child.Type == html.TextNode {
			body.WriteString(child.Data)
		}
	}
	require.Equal(t, oauthui.Script, body.String(), "authorization error pages may only run the reviewed oauthui.Script")
}
