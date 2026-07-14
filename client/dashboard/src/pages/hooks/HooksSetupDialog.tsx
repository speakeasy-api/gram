import { Card } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { useRoutes } from "@/routes";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { ExternalLink, Plus, Sparkles } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { HookSourceIcon } from "./HookSourceIcon";

function ClaudeInstallContent() {
  return (
    <div className="space-y-6">
      <div>
        <Heading variant="h6" className="mb-2 font-semibold">
          Test Yourself
        </Heading>
        <Type muted small className="mb-4">
          Try hooks in your Claude Code instance:
        </Type>
        <div className="bg-muted/50 space-y-2 p-4 font-mono text-sm">
          <div className="flex items-center justify-between">
            <code>claude plugin marketplace add speakeasy-api/gram</code>
          </div>
          <div className="flex items-center justify-between">
            <code>claude plugin install gram-hooks@gram</code>
          </div>
        </div>
      </div>

      <div>
        <Heading variant="h6" className="mb-2 font-semibold">
          Distribute to Your Team
        </Heading>
        <Type muted small className="mb-4">
          Require your team to use hooks by configuring their Claude Code
          settings:
        </Type>

        <div className="space-y-4">
          <div>
            <Type
              mono
              small
              muted
              as="div"
              className="mb-2 uppercase tracking-[0.08em]"
            >
              1. Require the marketplace
            </Type>
            <div className="bg-muted/50 p-4 font-mono text-sm">
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
            <Type
              mono
              small
              muted
              as="div"
              className="mb-2 uppercase tracking-[0.08em]"
            >
              2. Require the plugin
            </Type>
            <div className="bg-muted/50 p-4 font-mono text-sm">
              <code>
                {`{
  "plugins": {
    "required": ["gram-hooks@gram"]
  }
}`}
              </code>
            </div>
          </div>

          <Button variant="secondary" size="sm" asChild>
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
        <Heading variant="h6" className="mb-2 font-semibold">
          1. Publish the Plugin
        </Heading>
        <Type muted small className="mb-4">
          Add the hooks plugin to your Cursor team marketplace and mark it as
          required so it auto-installs for all team members:
        </Type>
        <div className="bg-muted/50 p-4 font-mono text-sm">
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
        <Heading variant="h6" className="mb-2 font-semibold">
          2. Configure Credentials
        </Heading>
        <Type muted small className="mb-4">
          In the Cursor team dashboard, add a{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">Session Start</code>{" "}
          hook that injects your platform credentials. These are automatically
          passed to all subsequent hooks in the session.
        </Type>
        <Type muted small className="mb-4">
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
        </Type>
        <div className="bg-muted/50 space-y-3 p-4 text-sm">
          <div className="flex items-baseline gap-2">
            <span className="text-muted-foreground shrink-0 font-medium">
              Hook Name:
            </span>
            <code>Platform Hooks</code>
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
            <div className="bg-background/50 mt-1 overflow-x-auto p-3 font-mono text-xs break-all whitespace-pre-wrap">
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
          with your platform credentials. Find your API key in your project's
          API Keys settings. This config syncs to all team members
          automatically.
        </p>
      </div>

      <div className="flex items-center gap-3">
        <Button variant="secondary" size="sm" asChild>
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
        <Button variant="secondary" size="sm" asChild>
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
        <Heading variant="h6" className="mb-2 font-semibold">
          1. Register the marketplace
        </Heading>
        <Type muted small className="mb-4">
          Register your org's published marketplace with Codex:
        </Type>
        {addCommand ? (
          <div className="bg-muted/50 p-4 font-mono text-sm">
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
          <Type muted small italic>
            Publish your plugins to GitHub first to get a marketplace URL.
          </Type>
        )}
      </div>

      <div>
        <Heading variant="h6" className="mb-2 font-semibold">
          2. Enable hooks and the plugin in{" "}
          <code className="text-sm">~/.codex/config.toml</code>
        </Heading>
        <Type muted small className="mb-3">
          Hooks are behind a feature flag and the plugin must be explicitly
          enabled. Add all of the following to{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">
            ~/.codex/config.toml
          </code>
          :
        </Type>
        <div className="bg-muted/50 p-4 font-mono text-sm">
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
        <Heading variant="h6" className="mb-2 font-semibold">
          3. Approve hooks in Codex
        </Heading>
        <Type muted small>
          After restarting Codex, open{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">Settings → Hooks</code>{" "}
          and enable each hook listed under the{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">
            {pluginName ?? "observability"}
          </code>{" "}
          plugin. Codex requires manual approval for each hook event before it
          will fire.
        </Type>
      </div>

      <div>
        <Heading variant="h6" className="mb-2 font-semibold">
          Or install from a ZIP
        </Heading>
        <Type muted small className="mb-4">
          Download a self-contained Codex plugin ZIP from the{" "}
          <strong>Plugins</strong> page (
          <code className="bg-muted px-1 py-0.5 text-xs">
            Download Observability Plugin → Codex
          </code>
          ). The ZIP includes an{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">install.sh</code> that
          handles all three steps automatically:
        </Type>
        <div className="bg-muted/50 space-y-1 p-4 font-mono text-sm">
          <code>unzip observability-codex.zip -d ~/gram-observability</code>
          <div className="mt-1">
            <code>bash ~/gram-observability/install.sh</code>
          </div>
        </div>
      </div>

      <div className="flex items-center gap-3">
        <Button variant="secondary" size="sm" asChild>
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
        <Button variant="secondary" size="sm" asChild>
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
    label: "Copilot",
    source: "copilot",
    available: false,
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
    <Card className="border-primary/30 hover:border-primary/30 bg-primary/5 mb-6">
      <div className="mb-2 flex items-center gap-2">
        <Sparkles className="text-primary size-4" />
        <Heading variant="h6" className="font-semibold">
          Recommended: install via your org's published marketplace
        </Heading>
      </div>
      <Type muted small className="mb-3">
        Your org publishes plugins to a private GitHub repo with credentials
        already embedded. Installing the <code>base</code> plugin gives your
        team observability automatically — no manual SessionStart hook, no
        credential paste.
      </Type>
      <Button variant="secondary" size="sm" asChild>
        <Link to={routes.plugins.href()}>Go to Plugins</Link>
      </Button>
    </Card>
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
}): JSX.Element {
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
                onClick={() => {
                  void (p.available && setSelected(p.id));
                }}
                disabled={!p.available}
                className={cn(
                  "relative flex items-center gap-2 border px-3 py-2 text-sm font-medium transition-colors",
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
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

export function HooksSetupButton(): JSX.Element {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button variant="secondary" size="sm" onClick={() => setOpen(true)}>
        <Button.LeftIcon>
          <Plus />
        </Button.LeftIcon>
        <Button.Text>Add provider</Button.Text>
      </Button>
      <HooksSetupDialog open={open} onOpenChange={setOpen} />
    </>
  );
}
