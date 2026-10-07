import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Text } from "@/components/ui/Text";
import { HooksSetupDialog } from "@/pages/hooks/HooksSetupDialog";
import { useState } from "react";
import {
  POLICY_SCOPE_MODE_LABEL,
  type PolicyScopeChoice,
  type PolicyScopeMode,
} from "./policy-mcp-scope";

const GROUP_LABEL = "Where this policy applies";

function isScopeChoice(mode: string): mode is PolicyScopeChoice {
  return mode === "everywhere" || mode === "mcp";
}

function SetUpHooksButton({ onClick }: { onClick: () => void }): JSX.Element {
  return (
    <Button variant="tertiary" size="xs" className="-ml-2" onClick={onClick}>
      <Button.Text>Set up hooks</Button.Text>
    </Button>
  );
}

// First thing on the Scope step; the rest of the step stays hidden until a
// card is picked, so each card says what it covers and what it needs. A
// card is offered only when its scope can take effect: Client sessions needs
// hook telemetry configured, Specific MCP servers needs the MCP scoping flag.
export function PolicyScopeModeCards({
  value,
  onChange,
  sessionsAvailable,
  mcpAvailable,
}: {
  value: PolicyScopeMode;
  onChange: (mode: PolicyScopeChoice) => void;
  /** False until the organization has a hooks-scoped key or inference hooks. */
  sessionsAvailable: boolean;
  /** False while the MCP-scoped policies flag is off. */
  mcpAvailable: boolean;
}): JSX.Element {
  const [hooksOpen, setHooksOpen] = useState(false);
  const dialog = hooksOpen ? (
    <HooksSetupDialog open onOpenChange={setHooksOpen} />
  ) : null;

  if (!sessionsAvailable && !mcpAvailable) {
    return (
      <>
        <InlineEmptyState
          icon="workflow"
          heading="Nothing to apply this policy to"
          description="Install the Speakeasy plugin and hooks on your workstations so policies can evaluate client sessions."
          action={<SetUpHooksButton onClick={() => setHooksOpen(true)} />}
        />
        {dialog}
      </>
    );
  }

  return (
    <div className="space-y-3">
      <div className="text-eyebrow">{GROUP_LABEL}</div>
      <RadioCardGroup
        aria-label={GROUP_LABEL}
        size="sm"
        orientation="horizontal"
        className="grid-flow-row md:grid-flow-col"
        value={value === "unset" ? null : value}
        onValueChange={(mode) => {
          if (isScopeChoice(mode)) onChange(mode);
        }}
      >
        {sessionsAvailable ? (
          <RadioCard
            value="everywhere"
            title={POLICY_SCOPE_MODE_LABEL.everywhere}
          >
            <div className="space-y-2">
              <p>
                Evaluates chat sessions and tool calls from coding agents such
                as Claude Code, Cursor, and Codex. Requires the Speakeasy plugin
                and hooks installed on each workstation. Sessions from agents
                without hooks are not evaluated.
              </p>
              <SetUpHooksButton onClick={() => setHooksOpen(true)} />
            </div>
          </RadioCard>
        ) : null}
        {mcpAvailable ? (
          <RadioCard value="mcp" title={POLICY_SCOPE_MODE_LABEL.mcp}>
            Evaluates tool calls through the MCP servers you select, checked at
            the gateway before the tool runs. Apply the policy to all tools on a
            server or only to specific tools. No agent setup required.
          </RadioCard>
        ) : null}
      </RadioCardGroup>
      {!sessionsAvailable ? (
        <div className="flex flex-wrap items-center gap-x-1 gap-y-1">
          <Text small muted>
            Client sessions become available once the Speakeasy plugin and hooks
            are installed on your workstations.
          </Text>
          <SetUpHooksButton onClick={() => setHooksOpen(true)} />
        </div>
      ) : null}
      {dialog}
    </div>
  );
}
