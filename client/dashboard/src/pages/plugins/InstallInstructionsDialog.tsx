import { ClaudeCodeSettingsInstall } from "@/components/claude-code-settings-install";
import { CLAUDE_CODE_REQUIRE_MARKETPLACE_DOCS_URL } from "@/lib/claude-code-marketplace";
import { CodeBlock } from "@/components/code";
import { InstallSteps } from "@/components/install-steps";
import { Button } from "@/components/ui/Button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { useFetcher } from "@/contexts/Fetcher";
import { getCursorInstallCommand } from "@/lib/cursor-install-command";
import { PERSONAL_ACCOUNT_GOVERNANCE_NOTE } from "@/lib/personal-account-governance";
import { cn } from "@/lib/utils";
import { useObservabilityPluginDownload } from "./useObservabilityPluginDownload";
import { useMarketplaceSettings } from "@gram/client/react-query/marketplaceSettings";
import { usePlugins } from "@gram/client/react-query/plugins";
import { Button as MoonshineButton } from "@/components/ui/Button";
import {
  ArrowLeft,
  BookOpen,
  Download,
  ExternalLink,
  Info,
} from "lucide-react";
import { useState } from "react";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { agentProvidersForSurface } from "@/components/agent-providers/agent-providers";

const COWORK_DOCS_URL =
  "https://support.claude.com/en/articles/13837433-manage-claude-cowork-plugins-for-your-organization";

const CLAUDE_CODE_SETTINGS_DOCS_URL =
  "https://code.claude.com/docs/en/settings";

const CLAUDE_PLUGINS_DOCS_URL =
  "https://claude.com/docs/plugins/overview#find-and-add-a-plugin";

const CURSOR_DASHBOARD_URL = "https://cursor.com/dashboard";

const CURSOR_PLUGINS_DOCS_URL = "https://cursor.com/docs/plugins";

type ContentProps = {
  repoOwner: string;
  repoName: string;
  marketplaceUrl: string | undefined;
  /** Display name of the specific plugin being installed, if any (vs. a generic marketplace-registration flow). */
  pluginName?: string;
  /** URL-safe slug for the specific plugin — required for the `<plugin>@<marketplace>` addressing Claude Code and Codex use. */
  pluginSlug?: string;
  /** Restricts the plugin-picker step to these plugins (e.g. the ones a given MCP server is actually installed to) instead of every plugin in the org. */
  candidatePlugins?: { name: string; slug: string; description?: string }[];
};

const providers = agentProvidersForSurface("plugins");
type Provider = (typeof providers)[number]["id"];

function ExternalTextLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-sky-500 hover:text-sky-600 inline-flex items-center gap-0.5 hover:underline"
    >
      {children}
      <ExternalLink className="size-3" />
    </a>
  );
}

function RelatedLinks({ links }: { links: { href: string; label: string }[] }) {
  return (
    <div className="mt-4 space-y-2">
      <h3 className="text-sm font-semibold">Related links</h3>
      <ul className="space-y-1.5">
        {links.map((link) => (
          <li key={link.href} className="flex items-start gap-1.5">
            <span className="text-muted-foreground" aria-hidden="true">
              -
            </span>
            <ExternalTextLink href={link.href}>{link.label}</ExternalTextLink>
          </li>
        ))}
      </ul>
    </div>
  );
}

/**
 * Claude Code install: one settings snippet that registers the marketplace
 * with autoUpdate on and enables the plugin, for the reader's own
 * ~/.claude/settings.json or for managed settings. The marketplace.json
 * "name" (not the GitHub repo name) keys extraKnownMarketplaces and suffixes
 * the enabledPlugins entry; see server/internal/plugins/naming/naming.go.
 * Cowork's plugin distribution is its own tab.
 */
function ClaudeCodeInstallContent({
  marketplaceUrl,
  marketplaceName,
  pluginSlug,
}: Pick<ContentProps, "marketplaceUrl" | "pluginSlug"> & {
  marketplaceName: string | undefined;
}) {
  if (!marketplaceUrl || !marketplaceName) {
    return (
      <p className="text-muted-foreground text-sm italic">
        Re-publish to mint a marketplace install URL.
      </p>
    );
  }

  return (
    <div className="min-w-0 space-y-6">
      <ClaudeCodeSettingsInstall
        marketplaceName={marketplaceName}
        marketplaceUrl={marketplaceUrl}
        plugins={[pluginSlug ?? "<plugin-slug>"]}
        secretUrl
      />
      {!pluginSlug && (
        <p className="text-muted-foreground text-xs">
          Replace{" "}
          <code className="bg-muted px-1 py-0.5">&lt;plugin-slug&gt;</code> with
          the plugin to enable.
        </p>
      )}
      <RelatedLinks
        links={[
          {
            href: CLAUDE_CODE_REQUIRE_MARKETPLACE_DOCS_URL,
            label: "Require a marketplace and its plugins",
          },
          {
            href: CLAUDE_CODE_SETTINGS_DOCS_URL,
            label: "Claude Code settings and precedence",
          },
        ]}
      />
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
          On Team or Enterprise, an Owner or Primary Owner registers the private
          GitHub repository as an organization marketplace. Set installation
          preferences to control member rollout; importing alone does not
          require every plugin.
        </p>

        <InstallSteps
          steps={[
            {
              title: "Open Organization settings on Claude.ai",
              description: (
                <>
                  Sign in to{" "}
                  <ExternalTextLink href="https://claude.ai/">
                    claude.ai
                  </ExternalTextLink>{" "}
                  as an Owner or Primary Owner and navigate to{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    Organization settings → Plugins &amp; skills → Marketplaces
                  </code>
                  , then click{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    Add plugins → Sync from GitHub
                  </code>
                  .
                </>
              ),
            },
            {
              title: "Add the GitHub source",
              description: (
                <>
                  Enter your private or internal GitHub repository. The importer
                  needs repository access; members receiving the organization
                  plugin do not each need a GitHub invitation:
                </>
              ),
              code: repoSlug,
              language: "text",
            },
            {
              title: "Authorize Claude's GitHub App",
              description:
                "The Claude GitHub App must be installed on this repository so Cowork can sync from it. If the repo doesn't appear in the picker, install the app and retry.",
            },
            {
              title: "Set installation preferences and verify",
              description:
                "Choose Required to pre-install the plugin without allowing members to remove it, or Installed by default to allow opting out. Start a new Cowork session and verify expected components. Required installation does not guarantee hook execution or telemetry delivery. Native Cowork monitoring is a separate Team/Enterprise feature.",
            },
          ]}
        />

        <RelatedLinks
          links={[{ href: COWORK_DOCS_URL, label: "Cowork setup guide" }]}
        />
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">
          Using a personal Claude account
        </h3>
        <p className="text-muted-foreground mb-4 text-sm">
          On Pro or Max, add a personal marketplace (including a private GitHub
          repository) or upload a plugin ZIP. For a private marketplace, connect
          GitHub and give the Claude GitHub App repository access when prompted.
          These are personal installations, not organization-required rollout.
          The ZIP alternative is shown below.
        </p>

        <InstallSteps
          steps={[
            {
              title: "Download the plugin",
              description: (
                <>
                  Close this dialog, open the plugin's Install menu, and choose{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    Download as zip — Claude
                  </code>
                  .
                </>
              ),
            },
            {
              title: "Upload it in Claude",
              description: (
                <>
                  In Claude, open{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    Customize → Plugins → Add → Upload plugin
                  </code>
                  , then select the ZIP. For updates, download a fresh ZIP after
                  publishing and upload the replacement with the same plugin
                  name. Start a new Cowork session and verify the expected
                  components; a successful upload does not prove hooks execute.
                  Marketplace installs update from their source instead.
                </>
              ),
            },
          ]}
        />
        <p className="text-muted-foreground mt-3 flex items-start gap-1.5 text-xs leading-relaxed">
          <Info className="mt-0.5 size-3.5 shrink-0" />
          <span>{PERSONAL_ACCOUNT_GOVERNANCE_NOTE}</span>
        </p>

        <RelatedLinks
          links={[
            {
              href: CLAUDE_PLUGINS_DOCS_URL,
              label: "Add a marketplace or ZIP",
            },
            {
              href: "https://claude.com/docs/plugins/platform-support",
              label: "Plugin component support",
            },
          ]}
        />
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
  pluginSlug,
}: Pick<ContentProps, "repoOwner" | "repoName" | "pluginName" | "pluginSlug">) {
  const repoUrl = `https://github.com/${repoOwner}/${repoName}`;
  // Generic plugin ZIPs need a manifest, but do not necessarily contain hooks.
  // The per-plugin ZIP (generateCursorPluginFlat) names its manifest with the
  // raw slug; only the team marketplace repo uses the `<slug>-cursor` name.
  const localInstallCommand = pluginSlug
    ? getCursorInstallCommand({
        pluginName: pluginSlug,
        archiveName: `${pluginSlug}.zip`,
      })
    : undefined;

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

        <InstallSteps
          steps={[
            {
              title: "Open your Cursor team dashboard",
              description: (
                <>
                  Sign in to{" "}
                  <ExternalTextLink href={CURSOR_DASHBOARD_URL}>
                    cursor.com/dashboard
                  </ExternalTextLink>{" "}
                  as a team admin.
                </>
              ),
            },
            {
              title: "Import the marketplace",
              description: (
                <>
                  Navigate to{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    Dashboard → Plugins &amp; MCPs → Team Marketplaces → Add
                    Marketplace → Import from Repo
                  </code>{" "}
                  and paste the repository URL:
                </>
              ),
              code: repoUrl,
              language: "text",
            },
            {
              title: `Mark the plugin as required${pluginName ? "" : " (recommended)"}`,
              description: pluginName ? (
                <>
                  In Cursor's team marketplace settings, mark the{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    {pluginName}
                  </code>{" "}
                  plugin as required so its tools are available to every team
                  member without per-user setup.
                </>
              ) : (
                "In Cursor's team marketplace settings, mark the appropriate plugin(s) as required so their tools are available to every team member without per-user setup."
              ),
            },
          ]}
        />

        <p className="text-muted-foreground mt-3 text-sm">
          Required prevents uninstalling; Default On allows opting out and
          Default Off requires members to install. For updates, enable Auto
          Refresh with the Cursor GitHub App installed on the repository, or
          click Refresh manually. Verify runtime behavior separately from
          installation.
        </p>

        <RelatedLinks
          links={[
            { href: CURSOR_DASHBOARD_URL, label: "Open Cursor dashboard" },
            { href: CURSOR_PLUGINS_DOCS_URL, label: "Team marketplace setup" },
          ]}
        />
      </div>

      <div>
        <h3 className="mb-2 text-sm font-semibold">
          Using a personal Cursor account
        </h3>
        <p className="text-muted-foreground mb-4 text-sm">
          Team marketplaces need a Cursor Teams or Enterprise plan. On an
          individual plan, install the plugin as a local plugin instead.
        </p>

        <InstallSteps
          steps={[
            {
              title: "Download the plugin",
              description: (
                <>
                  Close this dialog, open the plugin's Install menu, and choose{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    Download as zip — Cursor
                  </code>
                  .
                </>
              ),
            },
            {
              title: "Unzip it into Cursor's local plugins folder",
              description:
                "Run in Bash on macOS or Linux with unzip and Python 3 installed. Check the ZIP filename and Downloads location first (your browser may add a suffix). This is not a native PowerShell command; Windows hook runtime compatibility must be verified separately.",
              code: localInstallCommand,
              language: "bash",
              children: !pluginSlug ? (
                <p className="text-muted-foreground mt-3 text-xs leading-relaxed">
                  Open the Install menu on a specific plugin to generate its
                  validated installation command.
                </p>
              ) : undefined,
            },
            {
              title: "Reload Cursor",
              description: (
                <>
                  Run{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    Developer: Reload Window
                  </code>{" "}
                  and check expected components in the sidebar's Customize. For
                  a team-managed account, ask an admin to enable Allow Local
                  Plugin Imports at Dashboard → Settings → Security &amp;
                  Identity → Marketplace and Plugins (off by default on
                  Enterprise). An installed marketplace plugin with the same
                  name takes precedence over the local copy. For updates,
                  download a fresh ZIP after publishing, replace it with the
                  command above, then reload. Rerunning the same ZIP does not
                  fetch updates. Local copies are user-modifiable, not enforced
                  organization rollout.
                </>
              ),
            },
          ]}
        />

        <RelatedLinks
          links={[
            { href: CURSOR_PLUGINS_DOCS_URL, label: "Cursor plugins docs" },
          ]}
        />
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

  const pluginName = pluginSlug ?? "<plugin-slug>";
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
          enables hooks, and configures compatible Codex OpenTelemetry logs,
          traces, and metrics in{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">
            ~/.codex/config.toml
          </code>
          . Existing exporters are preserved and may require manual Gram setup.
          The script also pre-approves all hook events, so no manual Settings →
          Hooks step is required. Suitable for MDM deployment. This script sets
          up Speakeasy's observability plugin specifically.
        </p>
        <Button
          variant="secondary"
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
          <code className="bg-muted px-1 py-0.5">
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

        <InstallSteps
          steps={[
            {
              // The manual path reads the private repo with the user's own git
              // credentials, so they must be a collaborator first.
              title: "Accept the GitHub invite",
              description:
                "Ask an admin to add your GitHub username as a collaborator on this marketplace. GitHub emails an invite to the address on your GitHub account. Accept it before continuing.",
            },
            {
              title: "Register the marketplace",
              code: addCommand,
              language: "bash",
            },
            {
              title: (
                <>
                  Enable hooks and the plugin in{" "}
                  <code className="text-sm">~/.codex/config.toml</code>
                </>
              ),
              description: (
                <>
                  Hooks are behind a feature flag and the plugin must be
                  explicitly enabled. Add all of the following to{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    ~/.codex/config.toml
                  </code>
                  :
                </>
              ),
              code: configBlock,
              language: "toml",
              children: !pluginSlug && (
                <p className="text-muted-foreground text-xs">
                  Replace{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    &lt;plugin-slug&gt;
                  </code>{" "}
                  with the slug of the plugin you want to enable.
                </p>
              ),
            },
            {
              title: "Approve hooks in Codex",
              description: (
                <>
                  After restarting Codex, open{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    Settings → Hooks
                  </code>{" "}
                  and enable each hook listed under the{" "}
                  <code className="bg-muted px-1 py-0.5 text-xs">
                    {pluginName}
                  </code>{" "}
                  plugin. Codex requires manual approval for each hook event
                  before it will fire.
                </>
              ),
            },
          ]}
        />

        <RelatedLinks
          links={[
            {
              href: "https://learn.chatgpt.com/docs/hooks",
              label: "Hooks Docs",
            },
            {
              href: "https://developers.openai.com/plugins/build/plugins",
              label: "Plugin Docs",
            },
          ]}
        />
      </div>
    </div>
  );
}

/**
 * opencode install. opencode has no plugin-marketplace or deep-link concept
 * (unlike Claude Code / Cursor / Codex), so the primary path is the
 * server-generated observability ZIP (plugins.downloadObservabilityPlugin) —
 * a self-contained `.opencode` plugin (`plugin/agenthooks.ts` + `speakeasy.json`
 * + bootstrappers) with a freshly-minted hooks-scoped key already embedded,
 * extracted straight into a repo's `.opencode/`. The manual CLI path
 * (speakeasy-hooks install --provider=opencode) is kept as a fallback, plus
 * connecting an MCP server through opencode's own `mcp` config block. The MCP
 * snippet uses placeholders — grab the real name/URL from that server's own
 * hosted install page.
 */
function OpencodeInstallContent(): JSX.Element {
  const { isDownloading, download: handleDownloadPlugin } =
    useObservabilityPluginDownload("opencode", "observability-opencode.zip");

  const installBinary = `curl -fsSL https://raw.githubusercontent.com/speakeasy-api/gram/main/hooks/install.sh | sh`;

  const installCommand = `GRAM_HOOKS_ORG_KEY="your-hooks-scoped-api-key" \\
speakeasy-hooks install --provider=opencode --dir=. --project=your-project-slug`;

  const mcpConfig = `{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "<server-name>": {
      "type": "remote",
      "enabled": true,
      "url": "<mcp-server-url>",
      "headers": {
        "Authorization": "Bearer {env:MCP_SERVER_API_KEY}"
      }
    }
  }
}`;

  return (
    <div className="min-w-0 space-y-6">
      {/* ── Quick install ─────────────────────────────────────────────────── */}
      <div>
        <h3 className="mb-2 text-sm font-semibold">Quick install</h3>
        <p className="text-muted-foreground mb-3 text-sm">
          Download the Gram observability plugin as a ZIP — a self-contained{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">.opencode</code> plugin
          with a hooks-scoped API key already embedded (no CLI, no key to
          export). Extract it into your repo's{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">.opencode/</code> (or{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">
            ~/.config/opencode/
          </code>{" "}
          for every repo) and opencode auto-discovers it on next start.
        </p>
        <Button
          variant="secondary"
          size="sm"
          disabled={isDownloading}
          onClick={() => void handleDownloadPlugin()}
          className="inline-flex items-center gap-2"
        >
          <Download className="size-4" />
          {isDownloading ? "Downloading…" : "Download Plugin"}
        </Button>
        <p className="text-muted-foreground mt-2 text-xs">
          Then extract:{" "}
          <code className="bg-muted px-1 py-0.5">
            unzip observability-opencode.zip -d .opencode
          </code>
        </p>
      </div>

      <div className="border-t" />

      {/* ── Manual setup ──────────────────────────────────────────────────── */}
      <div className="space-y-4">
        <p className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
          Manual setup
        </p>
        <p className="text-muted-foreground text-sm">
          Prefer the CLI? Install the{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">speakeasy-hooks</code>{" "}
          binary:
        </p>
        <CodeBlock language="bash" className="bg-background">
          {installBinary}
        </CodeBlock>
        <p className="text-muted-foreground text-sm">
          Then run it from your repo to render the same plugin into{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">
            .opencode/plugin/
          </code>
          :
        </p>
        <CodeBlock language="bash" className="bg-background">
          {installCommand}
        </CodeBlock>
      </div>

      {/* ── Connect an MCP server ─────────────────────────────────────────── */}
      <div>
        <h3 className="mb-2 text-sm font-semibold">Connect an MCP server</h3>
        <p className="text-muted-foreground mb-3 text-sm">
          Merge an entry into the{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">mcp</code> block of
          your project's{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">opencode.json</code>.
          Replace the placeholders with the name, URL, and auth token from that
          server's own install page — that token is separate from the Gram hooks
          credential.
        </p>
        <CodeBlock language="json" className="bg-background">
          {mcpConfig}
        </CodeBlock>
      </div>
    </div>
  );
}

/**
 * GitHub Copilot install. Same server-generated observability ZIP as opencode
 * (plugins.downloadObservabilityPlugin?platform=copilot) — plugin.json +
 * hooks/hooks.json + speakeasy.json + bootstrappers, with a freshly-minted
 * hooks-scoped key already embedded. Exported so the hooks setup dialog can
 * show the same instructions without a second copy of them.
 */
export function CopilotInstallContent(): JSX.Element {
  const { isDownloading, download: handleDownloadPlugin } =
    useObservabilityPluginDownload("copilot", "observability-copilot.zip");

  return (
    <div className="min-w-0 space-y-6">
      {/* ── Quick install ─────────────────────────────────────────────────── */}
      <div>
        <h3 className="mb-2 text-sm font-semibold">Quick install</h3>
        <p className="text-muted-foreground mb-3 text-sm">
          Download the Gram observability plugin as a ZIP — a self-contained
          Copilot plugin with a hooks-scoped API key already embedded (no CLI,
          no key to export).
        </p>
        <Button
          variant="secondary"
          size="sm"
          disabled={isDownloading}
          onClick={() => void handleDownloadPlugin()}
          className="inline-flex items-center gap-2"
        >
          <Download className="size-4" />
          {isDownloading ? "Downloading…" : "Download Plugin"}
        </Button>
        <p className="text-muted-foreground mt-2 text-xs">
          Then extract and load it:{" "}
          <code className="bg-muted px-1 py-0.5">
            unzip observability-copilot.zip -d gram-hooks && copilot
            --plugin-dir gram-hooks
          </code>
        </p>
      </div>

      <div className="border-t" />

      {/* ── Caveats ───────────────────────────────────────────────────────── */}
      <div className="space-y-3">
        <p className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
          Before you install
        </p>
        <p className="text-muted-foreground text-sm">
          Hooks run in{" "}
          <span className="text-foreground font-medium">Copilot CLI</span> only.
          MCP servers and skills from your Gram plugin also load in VS Code and
          the Copilot app, but those surfaces never fire hooks — so no
          telemetry, spend gating, or policy enforcement there.
        </p>
        <p className="text-muted-foreground text-sm">
          Copilot stops running a tool's hook chain at the first deny. If
          another plugin denies a tool call before Gram's entry runs, that call
          is never reported.
        </p>
      </div>

      <RelatedLinks
        links={[
          {
            href: "https://docs.github.com/en/copilot/reference/hooks-reference",
            label: "Hooks Reference",
          },
          {
            href: "https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-plugin-reference",
            label: "Plugin Reference",
          },
        ]}
      />
    </div>
  );
}

/**
 * Pi install. Pi has no plugin marketplace, no hook configuration, and no MCP
 * client of its own — every integration point is a TypeScript extension loaded
 * into the Pi process — so the primary path is the server-generated
 * observability ZIP (plugins.downloadObservabilityPlugin?platform=pi): the
 * generated extension plus speakeasy.json and bootstrappers, with a
 * freshly-minted hooks-scoped key already embedded. The CLI path
 * (speakeasy-hooks install --provider=pi) renders the same extension. The MCP
 * snippet is the config a Pi MCP extension reads; Speakeasy reads the same
 * file to report which servers a workspace can reach.
 */
function PiInstallContent(): JSX.Element {
  const { isDownloading, download: handleDownloadPlugin } =
    useObservabilityPluginDownload("pi", "observability-pi.zip");

  const installBinary = `curl -fsSL https://raw.githubusercontent.com/speakeasy-api/gram/main/hooks/install.sh | sh`;

  const installCommand = `GRAM_HOOKS_ORG_KEY="your-hooks-scoped-api-key" \\
speakeasy-hooks install --provider=pi --dir=. --project=your-project-slug`;

  const mcpConfig = `{
  "mcpServers": {
    "<server-name>": {
      "transport": "streamable-http",
      "url": "<mcp-server-url>"
    }
  }
}`;

  return (
    <div className="min-w-0 space-y-6">
      {/* ── Quick install ─────────────────────────────────────────────────── */}
      <div>
        <h3 className="mb-2 text-sm font-semibold">Quick install</h3>
        <p className="text-muted-foreground mb-3 text-sm">
          Download the Gram observability plugin as a ZIP — a self-contained Pi
          extension with a hooks-scoped API key already embedded (no CLI, no key
          to export). Extract it into your repo&apos;s{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">.pi/</code> (or{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">~/.pi/agent/</code> for
          every repo) and Pi loads it on next start.
        </p>
        <Button
          variant="secondary"
          size="sm"
          disabled={isDownloading}
          onClick={() => void handleDownloadPlugin()}
          className="inline-flex items-center gap-2"
        >
          <Download className="size-4" />
          {isDownloading ? "Downloading…" : "Download Plugin"}
        </Button>
        <p className="text-muted-foreground mt-2 text-xs">
          Then extract:{" "}
          <code className="bg-muted px-1 py-0.5">
            unzip observability-pi.zip -d .pi
          </code>
          , or for every repo:{" "}
          <code className="bg-muted px-1 py-0.5">
            unzip observability-pi.zip -d ~/.pi/agent
          </code>
        </p>
        <p className="text-muted-foreground mt-2 text-xs">
          Project-local extensions load only after you trust the project, so
          answer Pi&apos;s trust prompt on first start.
        </p>
      </div>

      <div className="border-t" />

      {/* ── Manual setup ──────────────────────────────────────────────────── */}
      <div className="space-y-4">
        <p className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
          Manual setup
        </p>
        <p className="text-muted-foreground text-sm">
          Prefer the CLI? Install the{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">speakeasy-hooks</code>{" "}
          binary:
        </p>
        <CodeBlock language="bash" className="bg-background">
          {installBinary}
        </CodeBlock>
        <p className="text-muted-foreground text-sm">
          Then run it from your repo to render the same extension into{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">.pi/extensions/</code>:
        </p>
        <CodeBlock language="bash" className="bg-background">
          {installCommand}
        </CodeBlock>
      </div>

      {/* ── Connect an MCP server ─────────────────────────────────────────── */}
      <div>
        <h3 className="mb-2 text-sm font-semibold">Connect an MCP server</h3>
        <p className="text-muted-foreground mb-3 text-sm">
          Pi ships no MCP client. Install an MCP extension for Pi, then declare
          servers in{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">.pi/mcp.json</code> (or{" "}
          <code className="bg-muted px-1 py-0.5 text-xs">
            ~/.pi/agent/mcp.json
          </code>
          ). Speakeasy reads the same file, so every server a Pi workspace can
          reach shows up in your MCP inventory and its tool calls are attributed
          to it. Replace the placeholders with the name and URL from that
          server&apos;s own install page.
        </p>
        <CodeBlock language="json" className="bg-background">
          {mcpConfig}
        </CodeBlock>
      </div>
    </div>
  );
}

type DialogProps = ContentProps & {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

const providerLabel = (id: Provider): string =>
  providers.find((p) => p.id === id)?.name ?? id;

export function InstallInstructionsDialog({
  open,
  onOpenChange,
  ...content
}: DialogProps): JSX.Element {
  const [selected, setSelected] = useState<Provider | null>(null);
  const [selectedPluginSlug, setSelectedPluginSlug] = useState<string | null>(
    content.pluginSlug ?? null,
  );
  const [pluginConfirmed, setPluginConfirmed] = useState(false);
  const { data: marketplaceSettings } = useMarketplaceSettings();
  const { data: pluginsData } = usePlugins();
  const marketplaceName = marketplaceSettings?.effectiveName;

  // Restrict to the plugins this context actually cares about (e.g. the ones
  // a given MCP server is installed to) when the caller knows them; only fall
  // back to every org plugin when there's no such context (the marketplace
  // list page).
  const candidatePlugins =
    content.candidatePlugins ??
    (pluginsData?.plugins ?? []).map((p) => ({
      name: p.name,
      slug: p.slug,
      description: p.description,
    }));
  const needsPluginPicker = candidatePlugins.length > 1;
  const singleCandidate =
    candidatePlugins.length === 1 ? candidatePlugins[0] : undefined;

  const matchedPlugin = candidatePlugins.find(
    (p) => p.slug === selectedPluginSlug,
  );
  const effectivePluginName = needsPluginPicker
    ? (matchedPlugin?.name ?? content.pluginName)
    : (singleCandidate?.name ?? content.pluginName);
  const effectivePluginSlug = needsPluginPicker
    ? (matchedPlugin?.slug ?? selectedPluginSlug ?? undefined)
    : (singleCandidate?.slug ?? content.pluginSlug);

  const totalSteps = needsPluginPicker ? 3 : 2;
  const stepIndex = needsPluginPicker
    ? !pluginConfirmed
      ? 0
      : selected
        ? 2
        : 1
    : selected
      ? 1
      : 0;

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      setSelected(null);
      setPluginConfirmed(false);
      setSelectedPluginSlug(content.pluginSlug ?? null);
    }
    onOpenChange(nextOpen);
  };

  const goToStep = (idx: number) => {
    if (idx >= stepIndex) return;
    if (needsPluginPicker) {
      if (idx === 0) {
        setPluginConfirmed(false);
        setSelected(null);
      } else if (idx === 1) {
        setSelected(null);
      }
    } else if (idx === 0) {
      setSelected(null);
    }
  };

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-[662px]"
      >
        <SheetHeader className="sr-only">
          <SheetTitle>Install instructions</SheetTitle>
          <SheetDescription>
            Steps to install this plugin in your AI coding assistant.
          </SheetDescription>
        </SheetHeader>
        <div className="flex items-center gap-1.5 px-6 pt-6 pr-14">
          {Array.from({ length: totalSteps }, (_, idx) => (
            <button
              key={idx}
              type="button"
              onClick={() => goToStep(idx)}
              aria-label={`Step ${idx + 1} of ${totalSteps}`}
              aria-current={idx === stepIndex ? "step" : undefined}
              className={cn(
                "h-1 rounded-full transition-all",
                idx === stepIndex
                  ? "bg-foreground w-6"
                  : idx < stepIndex
                    ? "bg-foreground/40 hover:bg-foreground/60 w-4 cursor-pointer"
                    : "bg-border w-4",
              )}
            />
          ))}
          <span className="text-muted-foreground ml-auto text-[11px] tabular-nums">
            {stepIndex + 1}/{totalSteps}
          </span>
        </div>

        <div className="relative flex-1 overflow-hidden">
          <div
            className="flex h-full transition-transform duration-300 ease-in-out"
            style={{ transform: `translateX(-${stepIndex * 100}%)` }}
          >
            {needsPluginPicker && (
              <div className="w-full min-w-0 shrink-0 space-y-4 overflow-y-auto px-6 pb-6">
                <div>
                  <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                    Step 1
                  </p>
                  <h3 className="text-foreground mt-1 text-lg font-semibold">
                    Select a plugin
                  </h3>
                  <p className="text-muted-foreground mt-1 text-sm">
                    Choose which plugin you're installing.
                  </p>
                </div>

                <div className="grid grid-cols-2 gap-3">
                  {candidatePlugins.map((plugin) => (
                    <button
                      key={plugin.slug}
                      type="button"
                      onClick={() => {
                        setSelectedPluginSlug(plugin.slug);
                        setPluginConfirmed(true);
                      }}
                      className={cn(
                        "border-border bg-card hover:border-primary/50 hover:bg-muted/50 flex cursor-pointer flex-col items-start gap-1 border p-4 text-left transition-colors",
                        plugin.slug === selectedPluginSlug &&
                          "border-primary bg-primary/5",
                      )}
                    >
                      <span className="text-sm font-medium">{plugin.name}</span>
                      {plugin.description && (
                        <span className="text-muted-foreground text-xs">
                          {plugin.description}
                        </span>
                      )}
                    </button>
                  ))}
                </div>
              </div>
            )}

            <div className="w-full min-w-0 shrink-0 space-y-4 overflow-y-auto px-6 pb-6">
              <div>
                <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                  Step {needsPluginPicker ? 2 : 1}
                </p>
                <h3 className="text-foreground mt-1 text-lg font-semibold">
                  {effectivePluginName
                    ? `Install ${effectivePluginName}`
                    : "Distribute your marketplace"}
                </h3>
                <p className="text-muted-foreground mt-1 text-sm">
                  Choose where your team runs this{" "}
                  {effectivePluginName ? "plugin" : "marketplace"}.
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
                        "border-border bg-card flex flex-col items-center gap-2 border p-4 text-center transition-colors",
                        p.available
                          ? "hover:border-primary/50 hover:bg-muted/50 cursor-pointer"
                          : "cursor-not-allowed opacity-50",
                      )}
                    >
                      <div className="bg-secondary flex h-10 w-10 items-center justify-center">
                        <AgentProviderIcon
                          source={p.iconSource}
                          className="size-5"
                        />
                      </div>
                      <span className="text-sm font-medium">{p.name}</span>
                      {!p.available && (
                        <span className="text-muted-foreground text-[10px] tracking-wide uppercase">
                          Coming soon
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

            <div className="w-full min-w-0 shrink-0 space-y-4 overflow-y-auto px-6 pb-6">
              <div>
                <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
                  Step {needsPluginPicker ? 3 : 2}
                </p>
                <h3 className="text-foreground mt-1 text-lg font-semibold">
                  {selected && providerLabel(selected)}
                </h3>
              </div>

              {selected === "claude" && (
                <ClaudeCodeInstallContent
                  marketplaceUrl={content.marketplaceUrl}
                  marketplaceName={marketplaceName}
                  pluginSlug={effectivePluginSlug}
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
                  pluginName={effectivePluginName}
                  pluginSlug={effectivePluginSlug}
                />
              )}
              {selected === "codex" && (
                <CodexInstallContent
                  repoOwner={content.repoOwner}
                  repoName={content.repoName}
                  marketplaceName={marketplaceName}
                  pluginSlug={effectivePluginSlug}
                />
              )}
              {selected === "opencode" && <OpencodeInstallContent />}
              {selected === "copilot" && <CopilotInstallContent />}
              {selected === "pi" && <PiInstallContent />}
            </div>
          </div>
        </div>

        {stepIndex > 0 && (
          <div className="border-border flex items-center justify-between border-t px-6 py-4">
            <MoonshineButton
              variant="tertiary"
              size="sm"
              onClick={() => goToStep(stepIndex - 1)}
            >
              <MoonshineButton.LeftIcon>
                <ArrowLeft className="h-3 w-3" />
              </MoonshineButton.LeftIcon>
              <MoonshineButton.Text>Back</MoonshineButton.Text>
            </MoonshineButton>
            {stepIndex === totalSteps - 1 && (
              <MoonshineButton
                variant="primary"
                size="sm"
                onClick={() => handleOpenChange(false)}
              >
                <MoonshineButton.Text>Done</MoonshineButton.Text>
              </MoonshineButton>
            )}
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
      <Button variant="secondary" size="sm" onClick={() => setOpen(true)}>
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
