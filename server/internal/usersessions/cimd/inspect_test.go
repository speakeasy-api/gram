package cimd

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Inspect is the authenticated management view of the same resolution Resolve
// performs. These tests pin the outcome taxonomy Resolve deliberately
// flattens, and pin that Detail never carries transport error text.

func TestInspect_Valid(t *testing.T) {
	t.Parallel()

	_, resolver, clientID := serveDocumentJSON(t, validDocumentJSON)

	result := resolver.Inspect(t.Context(), clientID)

	require.Equal(t, OutcomeValid, result.Outcome)
	require.Equal(t, http.StatusOK, result.HTTPStatus)
	require.Empty(t, result.Reason)
	require.NotNil(t, result.Document)
	require.Equal(t, "CIMD Test Client", result.Document.ClientName)
	require.Equal(t, "The client ID metadata document is reachable and valid.", result.Detail)
}

func TestInspect_InvalidURL(t *testing.T) {
	t.Parallel()

	_, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no fetch should be attempted for a syntactically invalid client_id")
	})

	// http, not https: rejected by §3 before any request is made.
	result := resolver.Inspect(t.Context(), "http://client.example.com/client.json")

	require.Equal(t, OutcomeInvalidURL, result.Outcome)
	require.Zero(t, result.HTTPStatus, "no response was received")
	require.Equal(t, string(reasonClientIDScheme), result.Reason)
	require.Nil(t, result.Document)
	require.NotEmpty(t, result.Detail)
}

func TestInspect_Unreachable_Status(t *testing.T) {
	t.Parallel()

	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	result := resolver.Inspect(t.Context(), srv.URL+"/client.json")

	require.Equal(t, OutcomeUnreachable, result.Outcome)
	require.Equal(t, http.StatusNotFound, result.HTTPStatus)
	require.Nil(t, result.Document)
	require.Contains(t, result.Detail, "HTTP 404")
}

func TestInspect_Unreachable_Redirect(t *testing.T) {
	t.Parallel()

	// §5 forbids following redirects, so a 302 surfaces as unreachable
	// rather than being chased to a document that would validate.
	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example.com/client.json", http.StatusFound)
	})

	result := resolver.Inspect(t.Context(), srv.URL+"/client.json")

	require.Equal(t, OutcomeUnreachable, result.Outcome)
	require.Equal(t, http.StatusFound, result.HTTPStatus)
	require.Contains(t, result.Detail, "without redirecting")
}

func TestInspect_Unreachable_NoResponse(t *testing.T) {
	t.Parallel()

	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {})
	clientID := srv.URL + "/client.json"
	srv.Close()

	result := resolver.Inspect(t.Context(), clientID)

	require.Equal(t, OutcomeUnreachable, result.Outcome)
	require.Zero(t, result.HTTPStatus)
	// The transport error names the refused connection and the local port;
	// none of that may reach the operator.
	require.Equal(t, "Gram could not reach the document endpoint.", result.Detail)
}

func TestInspect_Unreachable_OversizedBody(t *testing.T) {
	t.Parallel()

	// A 200 whose body runs past the read cap. Blaming the status here would
	// tell the operator "returned HTTP 200, it must return 200"; blaming the
	// size cap is only correct because the sentinel says so.
	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"padding":"` + strings.Repeat("x", 8*1024) + `"}`)); err != nil {
			return // the client hangs up at the cap, which is the point
		}
	})

	result := resolver.Inspect(t.Context(), srv.URL+"/client.json")

	require.Equal(t, OutcomeUnreachable, result.Outcome)
	require.Equal(t, http.StatusOK, result.HTTPStatus)
	require.Contains(t, result.Detail, "larger than")
	require.Contains(t, result.Detail, "byte limit")
}

func TestInspect_Unreachable_ReadFailureIsNotBlamedOnSize(t *testing.T) {
	t.Parallel()

	// A 200 that promises more than it delivers: the read fails partway, but
	// nothing exceeded the cap, so the size-cap wording must not appear.
	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"client_id":`)); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}
	})

	result := resolver.Inspect(t.Context(), srv.URL+"/client.json")

	require.Equal(t, OutcomeUnreachable, result.Outcome)
	require.NotContains(t, result.Detail, "byte limit")
}

func TestInspect_Unparseable(t *testing.T) {
	t.Parallel()

	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte("<html>not json</html>")); err != nil {
			t.Errorf("write body: %v", err)
		}
	})

	result := resolver.Inspect(t.Context(), srv.URL+"/client.json")

	require.Equal(t, OutcomeUnparseable, result.Outcome)
	require.Equal(t, http.StatusOK, result.HTTPStatus)
	require.Nil(t, result.Document)
	require.Equal(t, "The document endpoint responded, but the body is not valid JSON.", result.Detail)
}

func TestInspect_InvalidDocument(t *testing.T) {
	t.Parallel()

	_, resolver, clientID := serveDocumentJSON(t, func(clientID string) map[string]any {
		doc := validDocumentJSON(clientID)
		delete(doc, "client_name")
		return doc
	})

	result := resolver.Inspect(t.Context(), clientID)

	require.Equal(t, OutcomeInvalidDocument, result.Outcome)
	require.Equal(t, http.StatusOK, result.HTTPStatus)
	require.Equal(t, string(reasonMissingClientName), result.Reason)
	require.Nil(t, result.Document)
	// The detail is the client-safe OAuth description, not a generic string.
	require.NotEmpty(t, result.Detail)
	require.NotEqual(t, "The client ID metadata document is not valid.", result.Detail)
}

func TestInspect_MismatchedClientID(t *testing.T) {
	t.Parallel()

	_, resolver, clientID := serveDocumentJSON(t, func(clientID string) map[string]any {
		doc := validDocumentJSON(clientID)
		doc["client_id"] = "https://impostor.example.com/client.json"
		return doc
	})

	result := resolver.Inspect(t.Context(), clientID)

	require.Equal(t, OutcomeInvalidDocument, result.Outcome)
	require.Equal(t, string(reasonClientIDMismatch), result.Reason)
}

// Resolve must keep its opaque contract even though it now shares an
// implementation with Inspect: the outcome taxonomy must not become
// reachable through the error it returns.
func TestResolve_StaysOpaqueAfterInspectRefactor(t *testing.T) {
	t.Parallel()

	srv, resolver := newDocServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte("<html>not json</html>")); err != nil {
			t.Errorf("write body: %v", err)
		}
	})

	_, err := resolver.Resolve(t.Context(), srv.URL+"/client.json", noCache)

	require.Error(t, err)
	// A parse failure is still reported as an opaque wrapped error carrying
	// no OAuth error shape, exactly as before the refactor — otherwise an
	// unauthenticated caller could tell "reachable but not JSON" apart from
	// "unreachable" and use Gram as a probe oracle.
	require.Contains(t, err.Error(), "parse client metadata document")
	require.Empty(t, safeDescriptionOf(err))
}
