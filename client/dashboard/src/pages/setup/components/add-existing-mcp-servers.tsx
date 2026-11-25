import { Button } from "@/components/ui/Button";
import { useOrganization } from "@/contexts/Auth";
import { useOrganizationPlatformMCPOnboarding } from "@/hooks/useOrganizationPlatformMCPOnboarding";
import { useState } from "react";
import { StepSection } from "./step-section";

// oxlint-disable-next-line react/only-export-components -- Export the prompt alongside its panel for focused safety-contract tests.
export function existingMCPServersPrompt(currentProjectSlug?: string): string {
  const project = currentProjectSlug
    ? `Ask me to confirm the destination project ${JSON.stringify(currentProjectSlug)}.`
    : "List eligible Speakeasy projects and ask me to choose the destination.";
  return [
    "Help me add remote MCP servers I already use in Claude Code to Speakeasy.",
    "Use the add-existing-mcp-servers skill if available. If it is unavailable, stop and guide me to install the Speakeasy Platform MCP plugin before proceeding.",
    "Before any local discovery, successfully call list_projects through your OWN Speakeasy connection in this Claude Code session; otherwise stop for Speakeasy sign-in/setup. Dashboard state or another client's authentication is not proof of access.",
    "Before running claude mcp list, explain that it health-checks approved servers, can launch stdio processes and contact local/private-network endpoints BEFORE filtering, and may cause process side effects. Obtain explicit informed consent for those effects, or offer a user-sanitized manual inventory instead without running discovery. Show only sanitized inventory and explain excluded servers.",
    project,
    "Ask which servers I want, check for existing registrations, inspect missing candidates, and confirm the exact batch before adding anything.",
    "For each missing server, search the catalogue by exact endpoint first, then by provider/name as a secondary lookup. Show candidate differences (endpoint, provider, capabilities, and authentication) and require my confirmation before substituting a catalogue candidate. Register confirmed candidates through the catalogue registration flow, not as direct remote servers. If I decline a candidate or no match exists, offer the original URL registration path, inspect that direct target, and require fresh explicit confirmation of the direct target and exact batch before registering. Do not treat confirmation of a declined catalogue candidate as consent to direct registration.",
    "Never copy local credentials or change local configuration. Authentication must happen through Speakeasy's secure setup flow.",
    "Verify and report every selected server separately. Do not claim partial success is complete, or require plugin/gateway distribution.",
  ].join(" ");
}

export function AddExistingMCPServers({
  currentProjectSlug,
}: {
  currentProjectSlug?: string;
}): JSX.Element | null {
  const organization = useOrganization();
  const query = useOrganizationPlatformMCPOnboarding(organization.id, {
    throwOnError: false,
    staleTime: 10_000,
  });
  const state = query.data;
  // Client family is install intent, not client-specific authentication evidence.
  // Offer a handoff; the Claude session must verify its own access.
  if (
    query.isError ||
    !state?.enabled ||
    state.clientFamily !== "claude_code"
  ) {
    return null;
  }
  const prompt = existingMCPServersPrompt(currentProjectSlug);
  return (
    <StepSection
      index={2}
      slug="add-existing-mcp-servers"
      title="Import existing MCP servers in Claude"
      badge="Optional"
    >
      <div className="space-y-4">
        <p className="text-muted-foreground text-sm">
          Copy the prompt and paste it into Claude Code. Claude will guide you
          through choosing which remote MCP servers to import into Speakeasy.
        </p>
        <CopyPrompt
          key={JSON.stringify([organization.id, prompt])}
          prompt={prompt}
        />
        <details className="text-muted-foreground text-sm">
          <summary className="cursor-pointer">
            View prompt and setup help
          </summary>
          <div className="mt-3 space-y-3">
            <div className="border-border bg-muted/30 rounded-md border p-3">
              <code className="block text-xs break-words whitespace-normal">
                {prompt}
              </code>
            </div>
            <p>
              If Claude cannot find the import skill,{" "}
              <a
                className="text-foreground underline underline-offset-4"
                href="https://github.com/speakeasy-api/marketplace#readme"
              >
                Install the Speakeasy Platform MCP plugin
              </a>
              .
            </p>
          </div>
        </details>
      </div>
    </StepSection>
  );
}

function CopyPrompt({ prompt }: { prompt: string }): JSX.Element {
  const [copyStatus, setCopyStatus] = useState<"idle" | "copied" | "error">(
    "idle",
  );
  async function copyPrompt() {
    try {
      await navigator.clipboard.writeText(prompt);
      setCopyStatus("copied");
    } catch {
      setCopyStatus("error");
    }
  }
  return (
    <div className="space-y-2">
      <Button
        variant="secondary"
        size="sm"
        icon="copy"
        className="focus-visible:ring-2 focus-visible:ring-offset-3 focus-visible:ring-[var(--border-focus)] focus-visible:ring-offset-[var(--bg-surface-primary-default)]"
        onClick={() => void copyPrompt()}
      >
        <Button.Text>Copy prompt</Button.Text>
      </Button>
      {copyStatus === "copied" && (
        <p role="status" className="text-muted-foreground text-sm">
          Prompt copied
        </p>
      )}
      {copyStatus === "error" && (
        <p role="alert" className="text-destructive text-sm">
          Could not copy the prompt. Open “View prompt and setup help” to copy
          it manually.
        </p>
      )}
    </div>
  );
}
