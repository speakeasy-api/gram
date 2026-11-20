package aivendors_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

func TestClientFromTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		terminal string
		want     aivendors.Client
	}{
		{name: "it reads vscode as the IDE", terminal: "vscode", want: aivendors.ClientIDE},
		{name: "it reads cursor as the IDE", terminal: "cursor", want: aivendors.ClientIDE},
		{name: "it reads zed as the IDE", terminal: "zed", want: aivendors.ClientIDE},
		{name: "it reads webstorm as the IDE", terminal: "webstorm", want: aivendors.ClientIDE},
		{name: "it reads phpstorm as the IDE", terminal: "phpstorm", want: aivendors.ClientIDE},
		{name: "it reads jetbrains as the IDE", terminal: "jetbrains", want: aivendors.ClientIDE},
		{name: "it reads goland as the IDE", terminal: "goland", want: aivendors.ClientIDE},
		{name: "it reads the jetbrains terminal emulator as the IDE", terminal: "JetBrains-JediTerm", want: aivendors.ClientIDE},
		{name: "it reads a bare ide terminal as the IDE", terminal: "ide", want: aivendors.ClientIDE},
		{name: "it reads the desktop app's terminal as the desktop", terminal: "claude-desktop", want: aivendors.ClientDesktop},
		{name: "it reads a headless run as no client", terminal: "non-interactive", want: aivendors.ClientUnknown},
		{name: "it says nothing for no terminal", terminal: " ", want: aivendors.ClientUnknown},
		{name: "it reads ghostty as the CLI", terminal: "ghostty", want: aivendors.ClientCLI},
		{name: "it reads iTerm as the CLI", terminal: "iTerm.app", want: aivendors.ClientCLI},
		{name: "it reads herdr as the CLI", terminal: "herdr", want: aivendors.ClientCLI},
		{name: "it reads rex as the CLI", terminal: "rex", want: aivendors.ClientCLI},
		{name: "it reads xterm as the CLI", terminal: "xterm-256color", want: aivendors.ClientCLI},
		{name: "it reads Apple Terminal as the CLI", terminal: "Apple_Terminal", want: aivendors.ClientCLI},
		{name: "it reads Warp as the CLI", terminal: "WarpTerminal", want: aivendors.ClientCLI},
		{name: "it reads an ssh session as the CLI", terminal: "ssh-session", want: aivendors.ClientCLI},
		{name: "it reads tmux as the CLI", terminal: "tmux", want: aivendors.ClientCLI},
		{name: "it reads kitty as the CLI", terminal: "kitty", want: aivendors.ClientCLI},
		{name: "it reads WezTerm as the CLI", terminal: "WezTerm", want: aivendors.ClientCLI},
		{name: "it reads gnome-terminal as the CLI", terminal: "gnome-terminal", want: aivendors.ClientCLI},
		{name: "it reads windows-terminal as the CLI", terminal: "windows-terminal", want: aivendors.ClientCLI},
		{name: "it reads WSL as the CLI", terminal: "wsl-Ubuntu", want: aivendors.ClientCLI},
		{name: "it reads linux as the CLI", terminal: "linux", want: aivendors.ClientCLI},
		{name: "it reads dumb as the CLI", terminal: "dumb", want: aivendors.ClientCLI},
		{name: "it reads vte as the CLI", terminal: "vte-based", want: aivendors.ClientCLI},
		{name: "it reads a terminal containing ide as the CLI", terminal: "slide-term", want: aivendors.ClientCLI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, aivendors.ClientFromTerminal(tt.terminal))
		})
	}
}
