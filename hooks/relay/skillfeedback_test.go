package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubmitSkillFeedbackPostsWithResolvedCreds(t *testing.T) {
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "test-hooks-key")

	var calls atomic.Int32
	var gotKey, gotProject, gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		gotKey = r.Header.Get("Gram-Key")
		gotProject = r.Header.Get("Gram-Project")
		gotPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	cfg := Config{ServerURL: server.URL, ProjectSlug: "acme", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	note := "missed an edge case"
	err := submitSkillFeedback(t.Context(), cfg, skillFeedbackInput{Skill: "release-notes", Outcome: "partially_helped", Note: note})
	require.NoError(t, err)

	require.Equal(t, int32(1), calls.Load())
	require.Equal(t, "/rpc/hooks.skillFeedback", gotPath)
	require.Equal(t, "test-hooks-key", gotKey)
	require.Equal(t, "acme", gotProject)
	require.Equal(t, map[string]any{
		"schema_version": "hook.skill-feedback.v1",
		"skill":          "release-notes",
		"outcome":        "partially_helped",
		"note":           note,
	}, gotBody)
}

func TestSubmitSkillFeedbackRejectsInvalidInputBeforeSending(t *testing.T) {
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "test-hooks-key")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no request must reach the server for invalid input")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	cfg := Config{ServerURL: server.URL, ProjectSlug: "acme", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	err := submitSkillFeedback(t.Context(), cfg, skillFeedbackInput{Skill: "Not A Slug", Outcome: "helped", Note: ""})
	require.ErrorContains(t, err, "canonical")

	err = submitSkillFeedback(t.Context(), cfg, skillFeedbackInput{Skill: "release-notes", Outcome: "changed_my_life", Note: ""})
	require.ErrorContains(t, err, "outcome")
}

func TestSubmitSkillFeedbackWithoutCredsAsksForLogin(t *testing.T) {
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")

	cfg := Config{ServerURL: "http://127.0.0.1:1", ProjectSlug: "acme", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
	err := submitSkillFeedback(t.Context(), cfg, skillFeedbackInput{Skill: "release-notes", Outcome: "helped", Note: ""})
	require.ErrorContains(t, err, "login")
}

func TestSkillFeedbackInputSchemaConstrainsOutcome(t *testing.T) {
	t.Parallel()

	schema := skillFeedbackInputSchema()
	raw, err := json.Marshal(schema)
	require.NoError(t, err)
	require.Contains(t, string(raw), "partially_helped")
	require.Contains(t, string(raw), skillNamePattern.String())
	require.ElementsMatch(t, []string{"skill", "outcome"}, schema.Required)
}
