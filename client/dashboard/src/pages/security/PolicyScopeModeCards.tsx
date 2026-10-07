import { Button } from "@/components/ui/Button";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
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

// First thing on the Scope step; the rest of the step stays hidden until a
// card is picked, so each card says what it covers and what it needs.
export function PolicyScopeModeCards({
  value,
  onChange,
  mcpAvailable,
}: {
  value: PolicyScopeMode;
  onChange: (mode: PolicyScopeChoice) => void;
  /** False while the MCP-scoped policies flag is off: only the session card. */
  mcpAvailable: boolean;
}): JSX.Element {
  const [hooksOpen, setHooksOpen] = useState(false);

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
        <RadioCard
          value="everywhere"
          title={POLICY_SCOPE_MODE_LABEL.everywhere}
        >
          <div className="space-y-2">
            <p>
              Evaluates chat sessions and tool calls from coding agents such as
              Claude Code, Cursor, and Codex. Requires the Speakeasy plugin and
              hooks installed on each workstation. Sessions from agents without
              hooks are not evaluated.
            </p>
            <Button
              variant="tertiary"
              size="xs"
              className="-ml-2"
              onClick={() => setHooksOpen(true)}
            >
              <Button.Text>Set up hooks</Button.Text>
            </Button>
          </div>
        </RadioCard>
        {mcpAvailable ? (
          <RadioCard value="mcp" title={POLICY_SCOPE_MODE_LABEL.mcp}>
            Evaluates tool calls through the MCP servers you select, checked at
            the gateway before the tool runs. Apply the policy to all tools on a
            server or only to specific tools. No agent setup required.
          </RadioCard>
        ) : null}
      </RadioCardGroup>
      {hooksOpen ? <HooksSetupDialog open onOpenChange={setHooksOpen} /> : null}
    </div>
  );
}
