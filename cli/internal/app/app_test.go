package app

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvokedAs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg0 string
		want string
	}{
		{name: "bare speakeasy", arg0: "speakeasy", want: "speakeasy"},
		{name: "absolute gram path", arg0: "/opt/homebrew/bin/gram", want: "gram"},
		{name: "relative speakeasy path", arg0: "./bin/speakeasy", want: "speakeasy"},
		{name: "windows gram exe", arg0: `C:\Program Files\gram\gram.exe`, want: "gram"},
		{name: "windows uppercase exe", arg0: `C:\tools\GRAM.EXE`, want: "gram"},
		{name: "windows speakeasy exe", arg0: `C:\tools\speakeasy.exe`, want: "speakeasy"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, invokedAs(tc.arg0))
		})
	}
}

func TestWriteLegacyCommandNotice_Gram(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeLegacyCommandNotice(&buf, "/usr/local/bin/gram")

	require.Equal(t, legacyCommandNotice+"\n", buf.String())
	require.Contains(t, buf.String(), "brew install speakeasy-api/tap/cli")
	require.Contains(t, buf.String(), "npm i -g @speakeasy-api/cli")
}

func TestWriteLegacyCommandNotice_Speakeasy(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeLegacyCommandNotice(&buf, "/usr/local/bin/speakeasy")

	require.Empty(t, buf.String())
}

func TestNewApp_UsesSpeakeasyName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "speakeasy", newApp().Name)
}
