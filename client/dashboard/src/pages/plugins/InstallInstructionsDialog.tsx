import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/ui/copy-button";
import { Dialog } from "@/components/ui/dialog";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { BookOpen, ExternalLink } from "lucide-react";
import { useState } from "react";
import { HookSourceIcon } from "../hooks/HookSourceIcon";

const COWORK_DOCS_URL =
  "https://support.claude.com/en/articles/13837433-manage-claude-cowork-plugins-for-your-organization";

const CLAUDE_CODE_SETTINGS_DOCS_URL =
  "https://code.claude.com/docs/en/settings";

const CURSOR_DASHBOARD_URL = "https://cursor.com/dashboard";

type ContentProps = {
  repoOwner: string;
  repoName: string;
  marketplaceUrl: string | undefined;
};

type Provider =
  | "claude-code"
  | "claude-cowork"
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
    id: "claude-code",
    label: "Claude Code",
    source: "claude-code",
    available: true,
  },
  {
    id: "claude-cowork",
    label: "Claude Cowork",
    source: "cowork",
    available: true,
  },
  { id: "cursor", label: "Cursor", source: "cursor", available: true },
  { id: "codex", label: "Codex", source: "codex", available: false },
  { id: "copilot", label: "Copilot", source: "copilot", available: false },
  { id: "gemini", label: "Gemini", source: "gemini", available: false },
  { id: "glean", label: "Glean", source: "glean", available: false },
  {
    id: "bedrock",
    label: "AWS Bedrock",
    source: "aws-bedrock",
    available: false,
  },
];

/**
 * Claude Code (individual CLI) install. Two paths covered here:
 *  - per-user registration via the slash command, served by the marketplace
 *    proxy
 *  - org-wide enforcement via Claude.ai's Managed Settings, which pushes an
 *    extraKnownMarketplaces entry into every org member's Claude Code
 *    install.
 *
 * Both go through Claude Code itself; neither involves Cowork's plugin
 * distribution (that's its own tab).
 */
function ClaudeCodeInstallContent({
  repoName,
  marketplaceUrl,
}: Pick<ContentProps, "repoName" | "marketplaceUrl">) {
  const installCommand = marketplaceUrl
    ? `/plugin marketplace add ${marketplaceUrl}`
    : null;

  // Schema reference: https://code.claude.com/docs/en/settings — under
  // extraKnownMarketplaces (additive; works for managed settings too) and
  // strictKnownMarketplaces (managed-only, allowlist semantics).
  const managedSettingsJson = marketplaceUrl
    ? JSON.stringify(
        {
          extraKnownMarketplaces: {
            [repoName]: {
              autoUpdate: true,
              source: {
                source: "url",
                url: marketplaceUrl,
              },
            },
          },
        },
        null,
        2,
      )
    : null;

  return (
    <div className="space-y-6">
      <div>
        <h3 className="mb-2 text-sm font-semibold">
          Install in your Claude Code instance
        </h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Run this command from inside Claude Code to register the marketplace
          for your user account:
        </p>
        {installCommand ? (
          <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
            <div className="flex items-center justify-between gap-2">
              <code className="break-all">{installCommand}</code>
              <CopyButton
                size="inline"
                text={installCommand}
                tooltip="Copy install command"
              />
            </div>
          </div>
        ) : (
          <p className="text-muted-foreground text-sm italic">
            Re-publish to mint a marketplace install URL.
          </p>
        )}
        <p className="text-muted-foreground mt-3 text-xs">
          Once registered, install individual plugins with{" "}
          <code className="bg-muted rounded px-1 py-0.5">
            /plugin install &lt;name&gt;
          </code>
          .
        </p>
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">
          Roll out to your team via Managed Settings
        </h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Push the marketplace to every Claude Code install in your organization
          through Claude.ai's Managed Settings — no per-user install command
          required.
        </p>

        <div className="space-y-4">
          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              1. Open Managed Settings on Claude.ai
            </h4>
            <p className="text-muted-foreground text-sm">
              Sign in to{" "}
              <a
                href="https://claude.ai/"
                target="_blank"
                rel="noopener noreferrer"
                className="text-sky-500 hover:text-sky-600 hover:underline"
              >
                claude.ai
              </a>{" "}
              as an organization admin, navigate to{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                Organization settings → Claude Code
              </code>
              , then click{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                Manage
              </code>{" "}
              under{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                Managed Settings
              </code>
              .
            </p>
          </div>

          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              2. Add the marketplace to settings.json
            </h4>
            <p className="text-muted-foreground mb-2 text-sm">
              Merge this entry into the org's managed{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                settings.json
              </code>
              :
            </p>
            {managedSettingsJson ? (
              <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
                <div className="flex items-start justify-between gap-2">
                  <pre className="overflow-x-auto whitespace-pre-wrap">
                    {managedSettingsJson}
                  </pre>
                  <CopyButton
                    size="inline"
                    text={managedSettingsJson}
                    tooltip="Copy settings.json snippet"
                  />
                </div>
              </div>
            ) : (
              <p className="text-muted-foreground text-sm italic">
                Re-publish to mint a marketplace install URL.
              </p>
            )}
            <p className="text-muted-foreground mt-2 text-xs">
              Use{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                strictKnownMarketplaces
              </code>{" "}
              instead of{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                extraKnownMarketplaces
              </code>{" "}
              to lock the org to this marketplace and reject all others.
            </p>
          </div>

          <Button variant="outline" size="sm" asChild>
            <a
              href={CLAUDE_CODE_SETTINGS_DOCS_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2"
            >
              <ExternalLink className="size-4" />
              Claude Code settings docs
            </a>
          </Button>
        </div>
      </div>
    </div>
  );
}

/**
 * Claude Cowork (org-managed) install. Cowork admins point their org at the
 * underlying private GitHub repo on Claude.ai's Organization Settings page;
 * Cowork's own GitHub App syncs from there and rolls the marketplace out to
 * every member's Claude Code and Claude.ai workspace.
 *
 * Note: this path doesn't use the marketplace proxy URL — Cowork talks
 * directly to GitHub via its App installation, not through us.
 */
function ClaudeCoworkInstallContent({
  repoOwner,
  repoName,
}: Pick<ContentProps, "repoOwner" | "repoName">) {
  const repoSlug = `${repoOwner}/${repoName}`;

  return (
    <div className="space-y-6">
      <div>
        <h3 className="mb-2 text-sm font-semibold">
          Roll out to your organization
        </h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Cowork admins register the underlying GitHub repository as a plugin
          source on Claude.ai. Members get the marketplace automatically — no
          per-user install command.
        </p>

        <div className="space-y-4">
          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              1. Open Organization settings on Claude.ai
            </h4>
            <p className="text-muted-foreground text-sm">
              Sign in to{" "}
              <a
                href="https://claude.ai/"
                target="_blank"
                rel="noopener noreferrer"
                className="text-sky-500 hover:text-sky-600 hover:underline"
              >
                claude.ai
              </a>{" "}
              as an organization admin and navigate to{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                Organization settings → Plugins
              </code>
              , then click{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                Add plugin
              </code>
              .
            </p>
          </div>

          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              2. Add the GitHub source
            </h4>
            <p className="text-muted-foreground mb-2 text-sm">
              Select{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                GitHub
              </code>{" "}
              as the source and enter your repo:
            </p>
            <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
              <div className="flex items-center justify-between gap-2">
                <code className="break-all">{repoSlug}</code>
                <CopyButton
                  size="inline"
                  text={repoSlug}
                  tooltip="Copy repository slug"
                />
              </div>
            </div>
          </div>

          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              3. Authorize Claude's GitHub App
            </h4>
            <p className="text-muted-foreground text-sm">
              The Claude GitHub App must be installed on this repository so
              Cowork can sync from it. If the repo doesn't appear in the picker,
              install the app and retry.
            </p>
          </div>

          <Button variant="outline" size="sm" asChild>
            <a
              href={COWORK_DOCS_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2"
            >
              <ExternalLink className="size-4" />
              Cowork setup guide
            </a>
          </Button>
        </div>
      </div>
    </div>
  );
}

/**
 * Cursor (team marketplace) install. Cursor team admins point their team at
 * the underlying private GitHub repo from cursor.com/dashboard; Cursor reads
 * the .cursor-plugin/marketplace.json the publish flow writes there. Steps
 * mirror what we already document in the published repo's README.md
 * (generateReadme in server/internal/plugins/generate.go), so changes here
 * should track those.
 */
function CursorInstallContent({
  repoOwner,
  repoName,
}: Pick<ContentProps, "repoOwner" | "repoName">) {
  const repoUrl = `https://github.com/${repoOwner}/${repoName}`;

  return (
    <div className="space-y-6">
      <div>
        <h3 className="mb-2 text-sm font-semibold">
          Roll out to your team in Cursor
        </h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Cursor team admins register the underlying GitHub repository as a
          plugin marketplace; once imported, plugins are available to every team
          member.
        </p>

        <div className="space-y-4">
          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              1. Open your Cursor team dashboard
            </h4>
            <p className="text-muted-foreground text-sm">
              Sign in to{" "}
              <a
                href={CURSOR_DASHBOARD_URL}
                target="_blank"
                rel="noopener noreferrer"
                className="text-sky-500 hover:text-sky-600 hover:underline"
              >
                cursor.com/dashboard
              </a>{" "}
              as a team admin.
            </p>
          </div>

          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              2. Import the marketplace
            </h4>
            <p className="text-muted-foreground mb-2 text-sm">
              Navigate to{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                Settings → Plugins → Import
              </code>{" "}
              and paste the repository URL:
            </p>
            <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
              <div className="flex items-center justify-between gap-2">
                <code className="break-all">{repoUrl}</code>
                <CopyButton
                  size="inline"
                  text={repoUrl}
                  tooltip="Copy repository URL"
                />
              </div>
            </div>
          </div>

          <div>
            <h4 className="text-muted-foreground mb-2 text-xs font-medium">
              3. Mark observability as required (recommended)
            </h4>
            <p className="text-muted-foreground text-sm">
              In Cursor's team marketplace settings, mark the observability
              plugin (it ships first in your marketplace) as required so tool
              events flow to Gram for every team member without per-user setup.
            </p>
          </div>

          <Button variant="outline" size="sm" asChild>
            <a
              href={CURSOR_DASHBOARD_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2"
            >
              <ExternalLink className="size-4" />
              Open Cursor dashboard
            </a>
          </Button>
        </div>
      </div>
    </div>
  );
}

type DialogProps = ContentProps & {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export function InstallInstructionsDialog({
  open,
  onOpenChange,
  ...content
}: DialogProps) {
  const [selected, setSelected] = useState<Provider>("claude-code");

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-3xl">
        <Dialog.Header>
          <Dialog.Title>Distribute your marketplace</Dialog.Title>
        </Dialog.Header>

        <div className="mb-6 flex flex-wrap gap-3">
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

        {selected === "claude-code" && (
          <ClaudeCodeInstallContent
            repoName={content.repoName}
            marketplaceUrl={content.marketplaceUrl}
          />
        )}
        {selected === "claude-cowork" && (
          <ClaudeCoworkInstallContent
            repoOwner={content.repoOwner}
            repoName={content.repoName}
          />
        )}
        {selected === "cursor" && (
          <CursorInstallContent
            repoOwner={content.repoOwner}
            repoName={content.repoName}
          />
        )}
      </Dialog.Content>
    </Dialog>
  );
}

/**
 * Convenience trigger that owns its own open state. Use this when the page
 * doesn't need to control the dialog imperatively.
 */
export function InstallInstructionsButton(props: ContentProps) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <BookOpen className="h-4 w-4" />
        Install instructions
      </Button>
      <InstallInstructionsDialog
        open={open}
        onOpenChange={setOpen}
        {...props}
      />
    </>
  );
}
