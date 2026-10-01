package organizations

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/supportmatrix"
	"github.com/stretchr/testify/require"
)

// A matrix small enough to read: Anthropic's inference does not apply on its
// own platform, the Cursor API does on Cursor's, OpenAI's two methods apply on
// Codex, LiteLLM belongs to no vendor and applies everywhere, and OpenCode's
// platform is unknown for every vendor-specific method.
const applicabilityMatrix = `
capabilities:
  - { id: session, name: Session tracking, group: Observe }
platforms:
  - { id: claude-code-cli, name: Claude Code, vendor: Anthropic, family: Claude Code, surface: CLI }
  - { id: cursor-ide, name: Cursor, vendor: Cursor, family: Cursor, surface: IDE }
  - { id: codex-cli, name: Codex, vendor: OpenAI, family: Codex, surface: CLI }
  - { id: opencode-app, name: OpenCode, vendor: OpenCode, family: OpenCode, surface: App }
methods:
  - id: inference
    name: Inference
    vendor: Anthropic
    plans: ""
    claims:
      session: { status: supported, note: "", verify: false }
    platforms:
      - { platform: claude-code-cli, applicability: na, accounts: { personal: unsupported, team: unsupported, enterprise: unsupported } }
      - { platform: cursor-ide, applicability: na, accounts: { personal: unsupported, team: unsupported, enterprise: unsupported } }
      - { platform: codex-cli, applicability: na, accounts: { personal: unsupported, team: unsupported, enterprise: unsupported } }
      - { platform: opencode-app, applicability: unknown, accounts: { personal: unknown, team: unknown, enterprise: unknown } }
  - id: anthropic-api
    name: Anthropic API
    vendor: Anthropic
    plans: ""
    claims:
      session: { status: supported, note: "", verify: false }
    platforms:
      - { platform: claude-code-cli, applicability: applicable, accounts: { personal: supported, team: supported, enterprise: supported }, cells: { session: { status: supported, note: "", verify: false } } }
      - { platform: cursor-ide, applicability: na, accounts: { personal: unsupported, team: unsupported, enterprise: unsupported } }
      - { platform: codex-cli, applicability: na, accounts: { personal: unsupported, team: unsupported, enterprise: unsupported } }
      - { platform: opencode-app, applicability: unknown, accounts: { personal: unknown, team: unknown, enterprise: unknown } }
  - id: cursor-api
    name: Cursor API
    vendor: Cursor
    plans: ""
    claims:
      session: { status: supported, note: "", verify: false }
    platforms:
      - { platform: claude-code-cli, applicability: na, accounts: { personal: unsupported, team: unsupported, enterprise: unsupported } }
      - { platform: cursor-ide, applicability: applicable, accounts: { personal: supported, team: supported, enterprise: supported }, cells: { session: { status: supported, note: "", verify: false } } }
      - { platform: codex-cli, applicability: na, accounts: { personal: unsupported, team: unsupported, enterprise: unsupported } }
      - { platform: opencode-app, applicability: unknown, accounts: { personal: unknown, team: unknown, enterprise: unknown } }
  - id: openai-api
    name: OpenAI API
    vendor: OpenAI
    plans: ""
    claims:
      session: { status: supported, note: "", verify: false }
    platforms:
      - { platform: claude-code-cli, applicability: na, accounts: { personal: unsupported, team: unsupported, enterprise: unsupported } }
      - { platform: cursor-ide, applicability: na, accounts: { personal: unsupported, team: unsupported, enterprise: unsupported } }
      - { platform: codex-cli, applicability: applicable, accounts: { personal: supported, team: supported, enterprise: supported }, cells: { session: { status: supported, note: "", verify: false } } }
      - { platform: opencode-app, applicability: unknown, accounts: { personal: unknown, team: unknown, enterprise: unknown } }
  - id: conversations
    name: Conversations
    vendor: OpenAI
    plans: ""
    claims:
      session: { status: supported, note: "", verify: false }
    platforms:
      - { platform: claude-code-cli, applicability: na, accounts: { personal: unsupported, team: unsupported, enterprise: unsupported } }
      - { platform: cursor-ide, applicability: na, accounts: { personal: unsupported, team: unsupported, enterprise: unsupported } }
      - { platform: codex-cli, applicability: applicable, accounts: { personal: supported, team: supported, enterprise: supported }, cells: { session: { status: supported, note: "", verify: false } } }
      - { platform: opencode-app, applicability: unknown, accounts: { personal: unknown, team: unknown, enterprise: unknown } }
  - id: litellm
    name: LiteLLM
    vendor: Others
    plans: ""
    claims:
      session: { status: supported, note: "", verify: false }
    platforms:
      - { platform: claude-code-cli, applicability: applicable, accounts: { personal: supported, team: supported, enterprise: supported }, cells: { session: { status: supported, note: "", verify: false } } }
      - { platform: cursor-ide, applicability: applicable, accounts: { personal: supported, team: supported, enterprise: supported }, cells: { session: { status: supported, note: "", verify: false } } }
      - { platform: codex-cli, applicability: applicable, accounts: { personal: supported, team: supported, enterprise: supported }, cells: { session: { status: supported, note: "", verify: false } } }
      - { platform: opencode-app, applicability: applicable, accounts: { personal: supported, team: supported, enterprise: supported }, cells: { session: { status: supported, note: "", verify: false } } }
`

func TestJudgeCardsFollowsTheStackAndTheMatrix(t *testing.T) {
	t.Parallel()

	matrix, err := supportmatrix.Parse([]byte(applicabilityMatrix))
	require.NoError(t, err)
	integrations, observability, litellm, identity := "additional-agent-config", "anthropic-observability", "litellm", "identity-provider"
	cards := []string{integrations, observability, litellm, identity}

	// No stack: a card with no method applies, one that belongs to no vendor
	// applies, and a vendor-specific one waits for the stack.
	none := judgeCards(matrix, nil, cards)
	require.True(t, none[identity].applies)
	require.True(t, none[litellm].applies)
	require.Equal(t, verdict{applies: false, reason: "record the organization's stack first"}, none[observability])

	// Cursor alone: the Cursor API settles Configure integrations, while the
	// Anthropic and OpenAI methods name what the stack lacks.
	cursor := judgeCards(matrix, []string{"Cursor"}, cards)
	require.True(t, cursor[integrations].applies)
	require.Equal(t, "needs Anthropic in the stack", cursor[observability].reason)

	// Anthropic alone: inference is not applicable on every Anthropic platform.
	anthropic := judgeCards(matrix, []string{"Anthropic"}, cards)
	require.Equal(t, verdict{applies: false, reason: "the support matrix marks its methods not applicable to this stack"}, anthropic[observability])
	require.True(t, anthropic[integrations].applies, "the Anthropic API applies on Claude Code")

	// OpenCode joining the stack does not rescue inference: its unknown cell
	// is outside the scan, which covers the method's own vendor only.
	wider := judgeCards(matrix, []string{"Anthropic", "OpenCode"}, cards)
	require.False(t, wider[observability].applies)
	require.True(t, wider[litellm].applies)

	// A card this build does not know applies, so a stored step is never
	// judged on nothing.
	require.True(t, judgeCards(matrix, []string{"Cursor"}, []string{"future-step"})["future-step"].applies)
}
