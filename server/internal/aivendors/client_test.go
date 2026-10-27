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
		{name: "it reads an editor terminal as the IDE", terminal: "vscode", want: aivendors.ClientIDE},
		{name: "it reads a jetbrains terminal as the IDE", terminal: "JetBrains-JediTerm", want: aivendors.ClientIDE},
		{name: "it reads a bare ide terminal as the IDE", terminal: "ide", want: aivendors.ClientIDE},
		{name: "it reads a terminal containing ide as the CLI", terminal: "slide-term", want: aivendors.ClientCLI},
		{name: "it reads any other terminal as the CLI", terminal: "iTerm.app", want: aivendors.ClientCLI},
		{name: "it says nothing for no terminal", terminal: " ", want: aivendors.ClientUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, aivendors.ClientFromTerminal(tt.terminal))
		})
	}
}
