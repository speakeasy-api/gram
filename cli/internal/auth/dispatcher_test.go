package auth

import (
	"html"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var redirectTargetPattern = regexp.MustCompile(`0;url=([^"]+)"`)

func TestDispatch_OpensUserOnlyRedirectFileInsteadOfURL(t *testing.T) {
	t.Parallel()

	callbackURL := "http://127.0.0.1:5555/callback?state=" + testState

	var opened string
	d := NewDispatcher(slog.New(slog.DiscardHandler), func(target string) error {
		opened = target
		return nil
	})

	cleanup, err := d.Dispatch(t.Context(), "https://app.example.test", callbackURL)
	require.NoError(t, err)

	require.NotEmpty(t, opened)
	require.False(t, strings.HasPrefix(opened, "http"), "process arguments must not carry the state-bearing URL")
	require.NotContains(t, opened, testState)

	info, err := os.Stat(opened)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	page, err := os.ReadFile(filepath.Clean(opened))
	require.NoError(t, err)
	m := redirectTargetPattern.FindStringSubmatch(string(page))
	require.NotNil(t, m, "redirect file must carry the sign-in URL")

	authURL, err := url.Parse(html.UnescapeString(m[1]))
	require.NoError(t, err)
	require.Equal(t, "app.example.test", authURL.Host)
	require.Equal(t, "true", authURL.Query().Get("from_cli"))
	require.Equal(t, callbackURL, authURL.Query().Get("cli_callback_url"))

	cleanup()
	_, err = os.Stat(opened)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDispatch_NoBrowserStillSucceeds(t *testing.T) {
	t.Parallel()

	var opened string
	d := NewDispatcher(slog.New(slog.DiscardHandler), func(target string) error {
		opened = target
		return ErrNoBrowser
	})

	cleanup, err := d.Dispatch(t.Context(), "https://app.example.test", "http://127.0.0.1:5555/callback?state="+testState)
	require.NoError(t, err)
	cleanup()

	_, err = os.Stat(opened)
	require.ErrorIs(t, err, os.ErrNotExist)
}
