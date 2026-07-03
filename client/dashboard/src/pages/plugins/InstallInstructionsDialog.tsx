import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/ui/copy-button";
import { Sheet, SheetContent } from "@/components/ui/sheet";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useFetcher } from "@/contexts/Fetcher";
import { cn } from "@/lib/utils";
import { useMarketplaceSettings } from "@gram/client/react-query/marketplaceSettings";
import { Button as MoonshineButton } from "@speakeasy-api/moonshine";
import { ArrowLeft, BookOpen, Download, ExternalLink } from "lucide-react";
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
  /** Display name of the specific plugin being installed, if any (vs. a generic marketplace-registration flow). */
  pluginName?: string;
  /** URL-safe slug for the specific plugin — required for the `<plugin>@<marketplace>` addressing Claude Code and Codex use. */
  pluginSlug?: string;
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
  { id: "codex", label: "Codex", source: "codex", available: true },
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
 *    extraKnownMarketplaces entry — and, when a specific plugin is being
 *    installed, an enabledPlugins entry — into every org member's Claude
 *    Code install.
 *
 * Both go through Claude Code itself; neither involves Cowork's plugin
 * distribution (that's its own tab).
 */
function ClaudeCodeInstallContent({
  marketplaceUrl,
  marketplaceName,
  pluginSlug,
}: Pick<ContentProps, "marketplaceUrl" | "pluginSlug"> & {
  marketplaceName: string | undefined;
}) {
  const installCommand = marketplaceUrl
    ? `/plugin marketplace add ${marketplaceUrl}`
    : null;
  const pluginInstallCommand =
    pluginSlug && marketplaceName
      ? `/plugin install ${pluginSlug}@${marketplaceName}`
      : null;

  // Schema reference: https://code.claude.com/docs/en/settings — under
  // extraKnownMarketplaces (additive; works for managed settings too) and
  // strictKnownMarketplaces (managed-only, allowlist semantics). The
  // marketplace.json "name" (not the GitHub repo name, which can be
  // anything) is what both extraKnownMarketplaces' key and enabledPlugins'
  // `<plugin>@<marketplace>` suffix reference — see
  // server/internal/plugins/naming/naming.go.
  const managedSettingsJson =
    marketplaceUrl && marketplaceName
      ? JSON.stringify(
          {
            extraKnownMarketplaces: {
              [marketplaceName]: {
                autoUpdate: true,
                source: {
                  source: "git",
                  url: marketplaceUrl,
                },
              },
            },
            ...(pluginSlug && {
              enabledPlugins: {
                [`${pluginSlug}@${marketplaceName}`]: true,
              },
            }),
          },
          null,
          2,
        )
      : null;

  return (
    <div className="min-w-0 space-y-6">
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
        {pluginInstallCommand ? (
          <div className="bg-muted/50 mt-3 rounded-lg p-4 font-mono text-sm">
            <div className="flex items-center justify-between gap-2">
              <code className="break-all">{pluginInstallCommand}</code>
              <CopyButton
                size="inline"
                text={pluginInstallCommand}
                tooltip="Copy plugin install command"
              />
            </div>
          </div>
        ) : (
          <p className="text-muted-foreground mt-3 text-xs">
            Once registered, install individual plugins with{" "}
            <code className="bg-muted rounded px-1 py-0.5">
              /plugin install &lt;name&gt;
            </code>
            .
          </p>
        )}
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
              2.{" "}
              {pluginSlug
                ? "Register the marketplace and enable the plugin"
                : "Add the marketplace to settings.json"}
            </h4>
            <p className="text-muted-foreground mb-2 text-sm">
              Merge this entry into the org's managed{" "}
              <code className="bg-muted rounded px-1 py-0.5 text-xs">
                settings.json
              </code>
              {pluginSlug
                ? " — this registers the marketplace and enables this specific plugin for every developer in one step:"
                : ":"}
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
    <div className="min-w-0 space-y-6">
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
  pluginName,
}: Pick<ContentProps, "repoOwner" | "repoName" | "pluginName">) {
  const repoUrl = `https://github.com/${repoOwner}/${repoName}`;

  return (
    <div className="min-w-0 space-y-6">
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
              3. Mark the plugin as required{pluginName ? "" : " (recommended)"}
            </h4>
            <p className="text-muted-foreground text-sm">
              {pluginName ? (
                <>
                  In Cursor's team marketplace settings, mark the{" "}
                  <code className="bg-muted rounded px-1 py-0.5 text-xs">
                    {pluginName}
                  </code>{" "}
                  plugin as required so its tools are available to every team
                  member without per-user setup.
                </>
              ) : (
                "In Cursor's team marketplace settings, mark the appropriate plugin(s) as required so their tools are available to every team member without per-user setup."
              )}
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

/**
 * Codex install. Offers a one-command install script as the primary path, with
 * manual 3-step instructions as a fallback.
 *
 * The downloadable quick-install script (plugins.downloadCodexInstallScript)
 * is server-generated and bootstraps Speakeasy's own observability plugin
 * specifically — it does not parameterize by an arbitrary plugin. The manual
 * setup section below is corrected to reference the actual plugin/marketplace
 * being installed when known, but the quick-install script's behavior is a
 * known, separate limitation (backend work, out of scope here).
 */
function CodexInstallContent({
  repoOwner,
  repoName,
  marketplaceName,
  pluginSlug,
}: Pick<ContentProps, "repoOwner" | "repoName" | "pluginSlug"> & {
  marketplaceName: string | undefined;
}) {
  const { fetch: authFetch } = useFetcher();
  const [isDownloading, setIsDownloading] = useState(false);

  const repoUrl = `https://github.com/${repoOwner}/${repoName}`;
  const addCommand = `codex plugin marketplace add ${repoUrl}`;

  // Falls back to the observability-plugin naming convention (repoName =
  // "<org-slug>-gram") when no specific plugin is being installed.
  const pluginName =
    pluginSlug ?? repoName.replace(/-gram$/, "-observability-codex");
  const marketplaceSuffix = marketplaceName ?? repoName;
  const featureFlags = `features.hooks = true\nfeatures.plugin_hooks = true`;
  const pluginEntry = `[plugins."${pluginName}@${marketplaceSuffix}"]\nenabled = true`;
  const configBlock = `${featureFlags}\n\n${pluginEntry}`;

  const handleDownloadInstallScript = async () => {
    setIsDownloading(true);
    try {
      const resp = await authFetch(
        "/rpc/plugins.downloadCodexInstallScript",
        {},
      );
      if (!resp.ok) return;
      const blob = await resp.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download =
        resp.headers
          .get("Content-Disposition")
          ?.match(/filename="(.+)"/)?.[1] ?? "gram-codex-install.sh";
      a.click();
      URL.revokeObjectURL(url);
    } finally {
      setIsDownloading(false);
    }
  };

  return (
    <div className="min-w-0 space-y-6">
      {/* ── Quick install ─────────────────────────────────────────────────── */}
      <div>
        <h3 className="mb-2 text-sm font-semibold">Quick install</h3>
        <p className="text-muted-foreground mb-3 text-sm">
          Download a one-command install script that registers the marketplace,
          enables hooks in{" "}
          <code className="bg-muted rounded px-1 py-0.5 text-xs">
            ~/.codex/config.toml
          </code>
          , and pre-approves all hook events — no manual Settings → Hooks step
          required. Suitable for MDM deployment. This script sets up Speakeasy's
          observability plugin specifically.
        </p>
        <Button
          variant="outline"
          size="sm"
          disabled={isDownloading}
          onClick={() => void handleDownloadInstallScript()}
          className="inline-flex items-center gap-2"
        >
          <Download className="size-4" />
          {isDownloading ? "Downloading…" : "Download Install Script"}
        </Button>
        <p className="text-muted-foreground mt-2 text-xs">
          Then run:{" "}
          <code className="bg-muted rounded px-1 py-0.5">
            bash ~/Downloads/gram-codex-install.sh
          </code>
        </p>
      </div>

      <div className="border-t" />

      {/* ── Manual setup ──────────────────────────────────────────────────── */}
      <div className="space-y-4">
        <p className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
          Manual setup
        </p>

        <div>
          <h4 className="mb-2 text-sm font-semibold">
            1. Register the marketplace
          </h4>
          <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
            <div className="flex items-center justify-between gap-2">
              <code className="break-all">{addCommand}</code>
              <CopyButton
                size="inline"
                text={addCommand}
                tooltip="Copy marketplace add command"
              />
            </div>
          </div>
        </div>

        <div>
          <h4 className="mb-2 text-sm font-semibold">
            2. Enable hooks and the plugin in{" "}
            <code className="text-sm">~/.codex/config.toml</code>
          </h4>
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
              <pre className="whitespace-pre-wrap">{configBlock}</pre>
              <CopyButton
                size="inline"
                text={configBlock}
                tooltip="Copy config entries"
              />
            </div>
          </div>
        </div>

        <div>
          <h4 className="mb-2 text-sm font-semibold">
            3. Approve hooks in Codex
          </h4>
          <p className="text-muted-foreground text-sm">
            After restarting Codex, open{" "}
            <code className="bg-muted rounded px-1 py-0.5 text-xs">
              Settings → Hooks
            </code>{" "}
            and enable each hook listed under the{" "}
            <code className="bg-muted rounded px-1 py-0.5 text-xs">
              {pluginName}
            </code>{" "}
            plugin. Codex requires manual approval for each hook event before it
            will fire.
          </p>
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
    </div>
  );
}

type DialogProps = ContentProps & {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const providerLabel = (id: Provider): string =>
  providers.find((p) => p.id === id)?.label ?? id;

function InstallInstructionsDialog({
  open,
  onOpenChange,
  ...content
}: DialogProps) {
  const [selected, setSelected] = useState<Provider | null>(null);
  const { data: marketplaceSettings } = useMarketplaceSettings();
  const marketplaceName = marketplaceSettings?.effectiveName;
  const panelIndex = selected ? 1 : 0;

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) setSelected(null);
    onOpenChange(nextOpen);
  };

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-[662px]"
      >
        <div className="flex items-center gap-1.5 px-6 pt-6 pr-14">
          {[0, 1].map((idx) => (
            <button
              key={idx}
              type="button"
              onClick={() => {
                if (idx === 0 && panelIndex > 0) setSelected(null);
              }}
              className={cn(
                "h-1 rounded-full transition-all",
                idx === panelIndex
                  ? "bg-foreground w-6"
                  : idx < panelIndex
                    ? "bg-foreground/40 hover:bg-foreground/60 w-4 cursor-pointer"
                    : "bg-border w-4",
              )}
            />
          ))}
          <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
            {panelIndex + 1}/2
          </span>
        </div>

        <div className="relative flex-1 overflow-hidden">
          <div
            className="flex h-full transition-transform duration-300 ease-in-out"
            style={{ transform: `translateX(-${panelIndex * 100}%)` }}
          >
            <div className="w-full shrink-0 space-y-4 overflow-y-auto px-6 pb-6">
              <div>
                <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                  Step 1
                </p>
                <h3 className="text-foreground mt-1 text-lg font-semibold">
                  {content.pluginName
                    ? `Install ${content.pluginName}`
                    : "Distribute your marketplace"}
                </h3>
                <p className="text-muted-foreground mt-1 text-sm">
                  Choose where your team runs this{" "}
                  {content.pluginName ? "plugin" : "marketplace"}.
                </p>
              </div>

              <div className="grid grid-cols-2 gap-3">
                {providers.map((p) => {
                  const tile = (
                    <button
                      key={p.id}
                      type="button"
                      disabled={!p.available}
                      onClick={() => {
                        if (p.available) setSelected(p.id);
                      }}
                      className={cn(
                        "border-border bg-card flex flex-col items-center gap-2 rounded-lg border p-4 text-center transition-colors",
                        p.available
                          ? "hover:border-primary/50 hover:bg-muted/50 cursor-pointer"
                          : "cursor-not-allowed opacity-50",
                      )}
                    >
                      <div className="bg-secondary flex h-10 w-10 items-center justify-center rounded-lg">
                        <HookSourceIcon source={p.source} className="size-5" />
                      </div>
                      <span className="text-sm font-medium">{p.label}</span>
                      {!p.available && (
                        <span className="text-muted-foreground text-[10px] tracking-wide uppercase">
                          Soon
                        </span>
                      )}
                    </button>
                  );

                  if (!p.available) {
                    return (
                      <Tooltip key={p.id}>
                        <TooltipTrigger asChild>{tile}</TooltipTrigger>
                        <TooltipContent>
                          <p>Coming soon</p>
                        </TooltipContent>
                      </Tooltip>
                    );
                  }

                  return tile;
                })}
              </div>
            </div>

            <div className="w-full shrink-0 space-y-4 overflow-y-auto px-6 pb-6">
              <div>
                <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                  Step 2
                </p>
                <h3 className="text-foreground mt-1 text-lg font-semibold">
                  {selected && providerLabel(selected)}
                </h3>
              </div>

              {selected === "claude-code" && (
                <ClaudeCodeInstallContent
                  marketplaceUrl={content.marketplaceUrl}
                  marketplaceName={marketplaceName}
                  pluginSlug={content.pluginSlug}
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
                  pluginName={content.pluginName}
                />
              )}
              {selected === "codex" && (
                <CodexInstallContent
                  repoOwner={content.repoOwner}
                  repoName={content.repoName}
                  marketplaceName={marketplaceName}
                  pluginSlug={content.pluginSlug}
                />
              )}
            </div>
          </div>
        </div>

        {selected && (
          <div className="border-border flex items-center justify-between border-t px-6 py-4">
            <MoonshineButton
              variant="tertiary"
              size="sm"
              onClick={() => setSelected(null)}
            >
              <MoonshineButton.LeftIcon>
                <ArrowLeft className="h-3 w-3" />
              </MoonshineButton.LeftIcon>
              <MoonshineButton.Text>Back</MoonshineButton.Text>
            </MoonshineButton>
            <MoonshineButton
              variant="primary"
              size="sm"
              onClick={() => onOpenChange(false)}
            >
              <MoonshineButton.Text>Done</MoonshineButton.Text>
            </MoonshineButton>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

/**
 * Convenience trigger that owns its own open state. Use this when the page
 * doesn't need to control the dialog imperatively.
 */
export function InstallInstructionsButton(props: ContentProps): JSX.Element {
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
