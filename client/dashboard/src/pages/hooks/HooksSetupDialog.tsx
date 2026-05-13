import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/ui/copy-button";
import { Dialog } from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useFetcher } from "@/contexts/Fetcher";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { ExternalLink, Plus, Sparkles } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { HookSourceIcon } from "./HookSourceIcon";

function ClaudeInstallContent() {
  return (
    <div className="space-y-6">
      <div>
        <h3 className="mb-2 text-sm font-semibold">Test Yourself</h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Try Gram Hooks in your Claude Code instance:
        </p>
        <div className="bg-muted/50 space-y-2 rounded-lg p-4 font-mono text-sm">
          <div className="flex items-center justify-between">
            <code>claude plugin marketplace add speakeasy-api/gram</code>
          </div>
          <div className="flex items-center justify-between">
            <code>claude plugin install gram-hooks@gram</code>
          </div>
        </div>
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">Distribute to Your Team</h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Require your team to use Gram Hooks by configuring their Claude Code
          settings:
        </p>

        <div className="space-y-4">
          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              1. Require the marketplace
            </h4>
            <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
              <code>
                {`{
  "pluginMarketplaces": {
    "required": ["speakeasy-api/gram"]
  }
}`}
              </code>
            </div>
          </div>

          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              2. Require the plugin
            </h4>
            <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
              <code>
                {`{
  "plugins": {
    "required": ["gram-hooks@gram"]
  }
}`}
              </code>
            </div>
          </div>

          <Button variant="outline" size="sm" asChild>
            <a
              href="https://code.claude.com/docs/en/plugin-marketplaces#require-marketplaces-for-your-team"
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2"
            >
              <ExternalLink className="size-4" />
              View Full Documentation
            </a>
          </Button>
        </div>
      </div>
    </div>
  );
}

function CursorInstallContent() {
  return (
    <div className="space-y-6">
      <div>
        <h3 className="mb-2 text-sm font-semibold">1. Publish the Plugin</h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Add the Gram hooks plugin to your Cursor team marketplace and mark it
          as required so it auto-installs for all team members:
        </p>
        <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
          <a
            href="https://cursor.com/dashboard/team-content"
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary hover:text-primary/80 underline underline-offset-4"
          >
            cursor.com/dashboard/team-content
          </a>
        </div>
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">2. Configure Credentials</h3>
        <p className="text-muted-foreground mb-4 text-sm">
          In the Cursor team dashboard, add a{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            Session Start
          </code>{" "}
          hook that injects your Gram credentials. These are automatically
          passed to all subsequent hooks in the session.
        </p>
        <p className="text-muted-foreground mb-4 text-sm">
          Go to{" "}
          <a
            href="https://cursor.com/dashboard/team-content?section=hooks"
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary hover:text-primary/80 underline underline-offset-4"
          >
            cursor.com/dashboard/team-content
          </a>{" "}
          and create a new hook with:
        </p>
        <div className="bg-muted/50 space-y-3 rounded-lg p-4 text-sm">
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground shrink-0 font-medium">
              Hook Name:
            </span>
            <code>Gram Hooks</code>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground shrink-0 font-medium">
              Hook Type:
            </span>
            <code>Command</code>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground shrink-0 font-medium">
              Hook Step:
            </span>
            <code>Session Start</code>
          </div>
          <div>
            <span className="text-muted-foreground font-medium">
              Script Content:
            </span>
            <div className="bg-background/50 mt-1 overflow-x-auto rounded p-3 font-mono text-xs break-all whitespace-pre-wrap">
              {`#!/bin/bash\necho '{"env":{"GRAM_API_KEY":"`}
              <span className="text-primary font-semibold">{`<YOUR_API_KEY>`}</span>
              {`","GRAM_PROJECT_SLUG":"`}
              <span className="text-primary font-semibold">{`<YOUR_PROJECT_SLUG>`}</span>
              {`"}}'`}
            </div>
          </div>
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground shrink-0 font-medium">
              Platforms:
            </span>
            <code>Mac, Linux</code>
          </div>
        </div>
        <p className="text-muted-foreground mt-2 text-xs">
          Replace{" "}
          <code className="text-primary text-xs">{`<YOUR_API_KEY>`}</code> and{" "}
          <code className="text-primary text-xs">{`<YOUR_PROJECT_SLUG>`}</code>{" "}
          with your Gram credentials. Find your API key in your project's API
          Keys settings. This config syncs to all team members automatically.
        </p>
      </div>

      <div className="flex items-center gap-3">
        <Button variant="outline" size="sm" asChild>
          <a
            href="https://cursor.com/docs/plugins"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2"
          >
            <ExternalLink className="size-4" />
            Plugin Docs
          </a>
        </Button>
        <Button variant="outline" size="sm" asChild>
          <a
            href="https://cursor.com/docs/hooks"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2"
          >
            <ExternalLink className="size-4" />
            Hooks Docs
          </a>
        </Button>
      </div>
    </div>
  );
}

function CodexInstallContent({
  marketplaceUrl,
  repoName,
}: {
  marketplaceUrl?: string;
  repoName?: string;
}) {
  const addCommand = marketplaceUrl
    ? `codex plugin marketplace add ${marketplaceUrl}`
    : null;

  const marketplaceKey = repoName ?? null;
  const pluginName = repoName
    ? repoName.replace(/-gram$/, "-observability-codex")
    : null;
  const pluginEntry =
    pluginName && marketplaceKey
      ? `[plugins."${pluginName}@${marketplaceKey}"]\nenabled = true`
      : null;

  const featureFlags = `features.hooks = true\nfeatures.plugin_hooks = true`;

  return (
    <div className="space-y-6">
      <div>
        <h3 className="mb-2 text-sm font-semibold">
          1. Register the marketplace
        </h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Register your org's published marketplace with Codex:
        </p>
        {addCommand ? (
          <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
            <div className="flex items-center justify-between gap-2">
              <code className="break-all">{addCommand}</code>
              <CopyButton
                size="inline"
                text={addCommand}
                tooltip="Copy command"
              />
            </div>
          </div>
        ) : (
          <p className="text-muted-foreground text-sm italic">
            Publish your plugins to GitHub first to get a marketplace URL.
          </p>
        )}
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">
          2. Enable hooks and the plugin in{" "}
          <code className="text-sm">~/.codex/config.toml</code>
        </h3>
        <p className="text-muted-foreground mb-3 text-sm">
          Hooks are behind a feature flag and the plugin must be explicitly
          enabled. Add all of the following to{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            ~/.codex/config.toml
          </code>
          :
        </p>
        <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
          <div className="flex items-start justify-between gap-2">
            <pre className="whitespace-pre-wrap">
              {[
                featureFlags,
                pluginEntry ??
                  `[plugins."${pluginName ?? "<plugin-name>"}@${marketplaceKey ?? "<marketplace>"}"]` +
                    `\nenabled = true`,
              ].join("\n\n")}
            </pre>
            <CopyButton
              size="inline"
              text={[featureFlags, pluginEntry ?? ""].join("\n\n").trim()}
              tooltip="Copy config entries"
            />
          </div>
        </div>
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">
          3. Approve hooks in Codex
        </h3>
        <p className="text-muted-foreground text-sm">
          After restarting Codex, open{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            Settings → Hooks
          </code>{" "}
          and enable each hook listed under the{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            {pluginName ?? "observability"}
          </code>{" "}
          plugin. Codex requires manual approval for each hook event before it
          will fire.
        </p>
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">Or install from a ZIP</h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Download a self-contained Codex plugin ZIP from the{" "}
          <strong>Plugins</strong> page (
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            Download Observability Plugin → Codex
          </code>
          ). The ZIP includes an{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            install.sh
          </code>{" "}
          that handles all three steps automatically:
        </p>
        <div className="bg-muted/50 space-y-1 rounded-lg p-4 font-mono text-sm">
          <code>unzip observability-codex.zip -d ~/gram-observability</code>
          <div className="mt-1">
            <code>bash ~/gram-observability/install.sh</code>
          </div>
        </div>
      </div>

      <div className="flex items-center gap-3">
        <Button variant="outline" size="sm" asChild>
          <a
            href="https://developers.openai.com/codex/hooks"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2"
          >
            <ExternalLink className="size-4" />
            Hooks Docs
          </a>
        </Button>
        <Button variant="outline" size="sm" asChild>
          <a
            href="https://developers.openai.com/codex/plugins/build"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2"
          >
            <ExternalLink className="size-4" />
            Plugin Docs
          </a>
        </Button>
      </div>
    </div>
  );
}

function CopilotInstallContent({
  marketplaceUrl,
  repoName,
}: {
  marketplaceUrl?: string;
  repoName?: string;
}) {
  const { fetch: authFetch } = useFetcher();
  const [downloading, setDownloading] = useState(false);

  // chat.plugins.marketplaces accepts a key → URL map; we use repoName
  // ("<org-slug>-gram") as the key so it lines up with the marketplace
  // identifier users see in the @agentPlugins search UI.
  const marketplaceKey = repoName ?? null;
  const observabilityPluginName = repoName
    ? repoName.replace(/-gram$/, "-observability-vscode")
    : null;

  const marketplaceSettings =
    marketplaceUrl && marketplaceKey
      ? JSON.stringify(
          {
            "chat.plugins.enabled": true,
            "chat.plugins.marketplaces": {
              [marketplaceKey]: marketplaceUrl,
            },
          },
          null,
          2,
        )
      : null;

  const handleDownload = async () => {
    setDownloading(true);
    try {
      const resp = await authFetch(
        `/rpc/plugins.downloadObservabilityPlugin?platform=vscode`,
        {},
      );
      if (!resp.ok) {
        toast.error("Failed to download VSCode Copilot plugin");
        return;
      }
      const blob = await resp.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download =
        resp.headers
          .get("Content-Disposition")
          ?.match(/filename="(.+)"/)?.[1] ?? "observability-vscode.zip";
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      toast.error("Failed to download VSCode Copilot plugin");
      console.error("vscode observability plugin download failed", err);
    } finally {
      setDownloading(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h3 className="mb-2 text-sm font-semibold">
          1. Enable plugins and register the marketplace
        </h3>
        <p className="text-muted-foreground mb-3 text-sm">
          Add the following to VSCode user settings (
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            settings.json
          </code>
          ). Gram's marketplace proxy serves the repo over a token-bearing Smart
          HTTP URL — no GitHub credentials required on each developer's machine.
        </p>
        {marketplaceSettings ? (
          <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
            <code className="whitespace-pre-wrap">{marketplaceSettings}</code>
          </div>
        ) : (
          <p className="text-muted-foreground text-sm italic">
            Publish your plugins first to mint a marketplace URL.
          </p>
        )}
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">
          2. Install the observability plugin
        </h3>
        <p className="text-muted-foreground text-sm">
          Open the Extensions view, filter with{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            @agentPlugins
          </code>
          , and install{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            {observabilityPluginName ?? "<org>-observability-vscode"}
          </code>
          . The embedded hooks-scoped API key attributes every event to your org
          and project — no per-user setup required.
        </p>
      </div>

      <div className="space-y-3 border-t pt-4">
        <p className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
          Fleet rollout
        </p>
        <p className="text-muted-foreground text-sm">
          VSCode's enterprise policy schema exposes{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            ChatPluginsEnabled
          </code>{" "}
          but no granular policy for{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            chat.plugins.marketplaces
          </code>
          . Push the snippet above to each developer's{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            settings.json
          </code>{" "}
          via your MDM (Intune, Jamf, Workspace ONE) and toggle the policy on
          org-wide.
        </p>
      </div>

      <div className="space-y-3 border-t pt-4">
        <p className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
          Standalone ZIP (fallback)
        </p>
        <p className="text-muted-foreground text-sm">
          For orgs that don't use the marketplace, download a self-contained ZIP
          with the observability plugin and register it via{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            chat.pluginLocations
          </code>
          . Each download mints a fresh hooks-scoped API key.
        </p>
        <Button
          variant="outline"
          size="sm"
          onClick={handleDownload}
          disabled={downloading}
        >
          {downloading ? "Downloading…" : "Download VSCode Copilot ZIP"}
        </Button>
      </div>

      <div className="flex items-center gap-3">
        <Button variant="outline" size="sm" asChild>
          <a
            href="https://code.visualstudio.com/docs/copilot/customization/agent-plugins"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2"
          >
            <ExternalLink className="size-4" />
            Plugin Docs
          </a>
        </Button>
        <Button variant="outline" size="sm" asChild>
          <a
            href="https://code.visualstudio.com/docs/enterprise/policies"
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-2"
          >
            <ExternalLink className="size-4" />
            Enterprise Policies
          </a>
        </Button>
      </div>
    </div>
  );
}

type Provider =
  | "claude"
  | "cursor"
  | "codex"
  | "copilot"
  | "gemini"
  | "glean"
  | "bedrock";

const providers: {
  id: Provider;
  label: string;
  source: string;
  available: boolean;
}[] = [
  {
    id: "claude",
    label: "Claude Code",
    source: "claude-code",
    available: true,
  },
  { id: "cursor", label: "Cursor", source: "cursor", available: true },
  { id: "codex", label: "Codex", source: "codex", available: true },
  {
    id: "copilot",
    label: "VSCode Copilot",
    source: "copilot",
    available: true,
  },
  { id: "gemini", label: "Gemini", source: "gemini", available: false },
  { id: "glean", label: "Glean", source: "glean", available: false },
  {
    id: "bedrock",
    label: "AWS Bedrock",
    source: "aws-bedrock",
    available: false,
  },
];

// PublishedRepoPanel surfaces the simpler install path for orgs that have
// already connected a published GitHub repo: a one-click "install the base
// plugin" flow that bakes credentials in, so they can skip the manual
// SessionStart hook below.
function PublishedRepoPanel() {
  const routes = useRoutes();
  return (
    <div className="border-primary/30 bg-primary/5 mb-6 rounded-lg border p-4">
      <div className="mb-2 flex items-center gap-2">
        <Sparkles className="text-primary size-4" />
        <h3 className="text-sm font-semibold">
          Recommended: install via your org's published marketplace
        </h3>
      </div>
      <p className="text-muted-foreground mb-3 text-sm">
        Your org publishes plugins to a private GitHub repo with credentials
        already embedded. Installing the <code>base</code> plugin gives your
        team observability automatically — no manual SessionStart hook, no
        credential paste.
      </p>
      <Button variant="outline" size="sm" asChild>
        <Link to={routes.plugins.href()}>Go to Plugins</Link>
      </Button>
    </div>
  );
}

export function HooksSetupDialog({
  open,
  onOpenChange,
  defaultProvider = "claude",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultProvider?: Provider;
}) {
  const [selected, setSelected] = useState<Provider>(defaultProvider);
  const { data: publishStatus } = usePublishStatus();
  const showPublishedPanel =
    publishStatus?.configured &&
    publishStatus?.connected &&
    Boolean(publishStatus?.repoUrl);

  useEffect(() => {
    setSelected(defaultProvider);
  }, [defaultProvider]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="flex max-h-[90vh] max-w-7xl flex-col sm:max-w-7xl">
        <Dialog.Header>
          <Dialog.Title>Setup Hooks</Dialog.Title>
        </Dialog.Header>

        {showPublishedPanel && publishStatus?.repoUrl && <PublishedRepoPanel />}

        <div className="flex flex-wrap gap-3">
          {providers.map((p) => {
            const button = (
              <button
                key={p.id}
                onClick={() => p.available && setSelected(p.id)}
                disabled={!p.available}
                className={cn(
                  "relative flex items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium transition-colors",
                  selected === p.id
                    ? "border-primary bg-primary/5"
                    : "border-border hover:border-primary/50 hover:bg-muted/50",
                  !p.available &&
                    "hover:border-border cursor-not-allowed opacity-50 hover:bg-transparent",
                )}
              >
                <HookSourceIcon source={p.source} className="size-5" />
                {p.label}
                {!p.available && (
                  <span className="text-muted-foreground ml-1 text-[10px] tracking-wide uppercase">
                    Soon
                  </span>
                )}
              </button>
            );

            if (!p.available) {
              return (
                <Tooltip key={p.id}>
                  <TooltipTrigger asChild>{button}</TooltipTrigger>
                  <TooltipContent>
                    <p>Coming soon</p>
                  </TooltipContent>
                </Tooltip>
              );
            }

            return button;
          })}
        </div>

        <div className="min-h-0 overflow-y-auto">
          {selected === "claude" && <ClaudeInstallContent />}
          {selected === "cursor" && <CursorInstallContent />}
          {selected === "codex" && (
            <CodexInstallContent
              marketplaceUrl={publishStatus?.marketplaceUrl}
              repoName={publishStatus?.repoName ?? undefined}
            />
          )}
          {selected === "copilot" && (
            <CopilotInstallContent
              marketplaceUrl={publishStatus?.marketplaceUrl}
              repoName={publishStatus?.repoName ?? undefined}
            />
          )}
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

export function HooksSetupButton() {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <Plus className="h-4 w-4" />
        Add provider
      </Button>
      <HooksSetupDialog open={open} onOpenChange={setOpen} />
    </>
  );
}
