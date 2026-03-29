import { CopyButton } from "@/components/ui/copy-button";
import { Dialog } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useTelemetry } from "@/contexts/Telemetry";
import { Terminal } from "lucide-react";

const CONNECT_AGENT_DISMISSED_KEY = "connect_agent_modal_dismissed";

/**
 * Set by the onboarding wizard when MCP setup completes.
 * The Home page reads this to know which toolset slug to pre-fill.
 */
export const CONNECT_AGENT_TOOLSET_KEY = "connect_agent_toolset_slug";

export function useConnectAgentModal() {
  const dismissed = localStorage.getItem(CONNECT_AGENT_DISMISSED_KEY);
  const toolsetSlug = localStorage.getItem(CONNECT_AGENT_TOOLSET_KEY);

  const shouldShow = !dismissed && !!toolsetSlug;

  const dismiss = () => {
    localStorage.setItem(CONNECT_AGENT_DISMISSED_KEY, "true");
    localStorage.removeItem(CONNECT_AGENT_TOOLSET_KEY);
  };

  return { shouldShow, toolsetSlug, dismiss };
}

function buildInstallCommands(toolsetSlug: string) {
  return [
    "claude plugin marketplace add speakeasy-api/gram",
    "claude plugin install gram-hooks@gram",
    "claude plugin install gram-skills@gram",
    `gram install claude-code --toolset ${toolsetSlug}`,
  ].join("\n");
}

export function ConnectAgentModal({
  open,
  onOpenChange,
  toolsetSlug,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  toolsetSlug: string;
}) {
  const telemetry = useTelemetry();

  const commands = buildInstallCommands(toolsetSlug);

  const handleCopy = () => {
    telemetry.capture("connect_agent_modal", {
      action: "copied_commands",
      toolset_slug: toolsetSlug,
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-lg">
        <Dialog.Header>
          <Dialog.Title className="flex items-center gap-2">
            <Terminal className="size-5" />
            Connect your coding agent
          </Dialog.Title>
          <Dialog.Description>
            Your MCP server is live. Paste these commands into your terminal to
            connect Claude Code with Gram hooks, skills, and your new toolset.
          </Dialog.Description>
        </Dialog.Header>

        <div className="relative">
          <div className="bg-muted/50 rounded-lg p-4 pr-12 font-mono text-sm space-y-1.5">
            <div className="text-muted-foreground">
              # Add the Gram marketplace
            </div>
            <div>claude plugin marketplace add speakeasy-api/gram</div>
            <div className="mt-3 text-muted-foreground"># Install plugins</div>
            <div>claude plugin install gram-hooks@gram</div>
            <div>claude plugin install gram-skills@gram</div>
            <div className="mt-3 text-muted-foreground">
              # Connect your MCP server
            </div>
            <div>gram install claude-code --toolset {toolsetSlug}</div>
          </div>
          <CopyButton text={commands} absolute onCopy={handleCopy} />
        </div>

        <Dialog.Footer>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Skip
          </Button>
          <Button
            onClick={() => {
              navigator.clipboard.writeText(commands);
              handleCopy();
              onOpenChange(false);
            }}
          >
            Copy and close
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
