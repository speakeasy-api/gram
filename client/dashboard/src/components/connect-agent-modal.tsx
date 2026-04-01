import { CopyButton } from "@/components/ui/copy-button";
import { Dialog } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Button as MoonshineButton } from "@speakeasy-api/moonshine";
import { useTelemetry } from "@/contexts/Telemetry";
import { Terminal } from "lucide-react";
import { useCallback, useState, useSyncExternalStore } from "react";

/**
 * Set this key to a toolset slug to trigger the "Connect your agent" modal.
 * The modal shows once per slug — dismissed slugs are tracked so re-setting
 * the same slug won't re-show it, but a new slug will.
 */
export const CONNECT_AGENT_TOOLSET_KEY = "connect_agent_toolset_slug";
const DISMISSED_SLUGS_KEY = "connect_agent_dismissed_slugs";
const SHOW_ON_LOGIN_KEY = "connect_agent_show_on_login";

function getDismissedSlugs(): Set<string> {
  try {
    return new Set(
      JSON.parse(localStorage.getItem(DISMISSED_SLUGS_KEY) ?? "[]"),
    );
  } catch {
    return new Set();
  }
}

function dismissSlug(slug: string) {
  const slugs = getDismissedSlugs();
  slugs.add(slug);
  localStorage.setItem(DISMISSED_SLUGS_KEY, JSON.stringify([...slugs]));
  localStorage.removeItem(CONNECT_AGENT_TOOLSET_KEY);
}

/** Call after source creation to trigger the modal with the new toolset. */
export function triggerConnectAgentModal(toolsetSlug: string) {
  localStorage.setItem(CONNECT_AGENT_TOOLSET_KEY, toolsetSlug);
  // Notify any listening components
  window.dispatchEvent(
    new StorageEvent("storage", { key: CONNECT_AGENT_TOOLSET_KEY }),
  );
}

const LOGIN_DISMISSED_KEY = "connect_agent_login_dismissed";

/** Call on login to show a generic (no toolset) version of the modal. Shows only once. */
export function triggerConnectAgentOnLogin() {
  if (localStorage.getItem(LOGIN_DISMISSED_KEY)) return;
  localStorage.setItem(SHOW_ON_LOGIN_KEY, "true");
}

function getSnapshot(): string | null {
  const loginShow = localStorage.getItem(SHOW_ON_LOGIN_KEY);
  const toolsetSlug = localStorage.getItem(CONNECT_AGENT_TOOLSET_KEY);

  if (toolsetSlug) {
    const dismissed = getDismissedSlugs();
    if (!dismissed.has(toolsetSlug)) return toolsetSlug;
  }

  if (loginShow === "true") return "__login__";

  return null;
}

function subscribe(callback: () => void) {
  window.addEventListener("storage", callback);
  return () => window.removeEventListener("storage", callback);
}

export function useConnectAgentModal() {
  const slug = useSyncExternalStore(subscribe, getSnapshot);

  const dismiss = useCallback(() => {
    if (slug === "__login__") {
      localStorage.removeItem(SHOW_ON_LOGIN_KEY);
      localStorage.setItem(LOGIN_DISMISSED_KEY, "true");
    } else if (slug) {
      dismissSlug(slug);
    }
    // Force re-render
    window.dispatchEvent(
      new StorageEvent("storage", { key: CONNECT_AGENT_TOOLSET_KEY }),
    );
  }, [slug]);

  return {
    shouldShow: slug !== null,
    toolsetSlug: slug === "__login__" ? null : slug,
    dismiss,
  };
}

function buildInstallCommands(toolsetSlug: string | null) {
  const lines = [
    "claude plugin marketplace add speakeasy-api/gram",
    "claude plugin install gram-skills@gram",
  ];
  if (toolsetSlug) {
    lines.push(`gram install claude-code --toolset ${toolsetSlug}`);
  }
  return lines.join("\n");
}

export function ConnectAgentModal({
  open,
  onOpenChange,
  toolsetSlug,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  toolsetSlug: string | null;
}) {
  const telemetry = useTelemetry();
  const commands = buildInstallCommands(toolsetSlug);

  const handleCopy = () => {
    telemetry.capture("connect_agent_modal", {
      action: "copied_commands",
      toolset_slug: toolsetSlug ?? "none",
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
            {toolsetSlug
              ? "Your MCP server is live. Install the Gram skills plugin to get guided deployment workflows in Claude Code."
              : "Install the Gram skills plugin for guided deployment workflows in Claude Code."}
          </Dialog.Description>
        </Dialog.Header>

        <div className="relative">
          <div className="bg-muted/50 rounded-lg p-4 pr-12 font-mono text-sm space-y-1.5">
            <div className="text-muted-foreground">
              # Add the Gram marketplace
            </div>
            <div>claude plugin marketplace add speakeasy-api/gram</div>
            <div className="mt-3 text-muted-foreground">
              # Install the skills plugin
            </div>
            <div>claude plugin install gram-skills@gram</div>
            {toolsetSlug && (
              <>
                <div className="mt-3 text-muted-foreground">
                  # Connect your MCP server
                </div>
                <div>gram install claude-code --toolset {toolsetSlug}</div>
              </>
            )}
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

/** Standalone button that opens the connect agent modal inline. */
export function ConnectAgentButton({ toolsetSlug }: { toolsetSlug?: string }) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <MoonshineButton
        variant="secondary"
        size="sm"
        onClick={() => setOpen(true)}
      >
        <MoonshineButton.LeftIcon>
          <Terminal className="size-4" />
        </MoonshineButton.LeftIcon>
        <MoonshineButton.Text>Install Skills</MoonshineButton.Text>
      </MoonshineButton>
      <ConnectAgentModal
        open={open}
        onOpenChange={setOpen}
        toolsetSlug={toolsetSlug ?? null}
      />
    </>
  );
}
