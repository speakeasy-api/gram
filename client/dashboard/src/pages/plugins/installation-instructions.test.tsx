import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { InstallInstructionsDialog } from "./InstallInstructionsDialog";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { PublishDialog } from "./PublishDialog";
import { PERSONAL_ACCOUNT_GOVERNANCE_NOTE } from "@/lib/personal-account-governance";

vi.mock("@gram/client/react-query/marketplaceSettings", () => ({
  useMarketplaceSettings: () => ({
    data: { effectiveName: "example-marketplace" },
  }),
}));
vi.mock("@gram/client/react-query/plugins", () => ({
  usePlugins: () => ({ data: { plugins: [] } }),
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({
    data: { vibeObservabilityPlugin: "example-observability" },
  }),
}));
vi.mock("./usePluginQueryScope", () => ({
  usePluginQueryScope: () => ({ gramProject: "default", gramSession: "s" }),
}));
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: string }) => <pre>{children}</pre>,
}));

afterEach(() => {
  cleanup();
});

function bodyText(): string {
  return document.body.textContent!.replace(/\s+/g, " ");
}

function openProvider(
  provider: string,
  pluginSlug: string | undefined = "example-plugin",
) {
  render(
    <TooltipProvider>
      <InstallInstructionsDialog
        open
        onOpenChange={vi.fn<() => void>()}
        repoOwner="example-owner"
        repoName="example-org-plugins"
        marketplaceUrl="https://example.invalid/marketplace"
        pluginName="Example plugin"
        pluginSlug={pluginSlug}
      />
    </TooltipProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: new RegExp(provider) }));
  return document.body.textContent!.replace(/\s+/g, " ");
}

describe("installation instructions", () => {
  it("offers Pro/Max personal marketplaces and ZIPs without claiming a vendor restriction", () => {
    const text = openProvider("Claude Cowork");
    expect(text).toContain("On Pro or Max, add a personal marketplace");
    expect(text).toContain("Customize → Plugins → Add → Upload plugin");
    expect(text).toContain("download a fresh ZIP");
    expect(text).toContain("Owner or Primary Owner");
    expect(text).toContain(
      "Organization settings → Plugins & skills → Marketplaces",
    );
    expect(text).toContain("Add plugins → Sync from GitHub");
    expect(text).toContain("does not prove hooks execute");
    expect(text).toContain(PERSONAL_ACCOUNT_GOVERNANCE_NOTE);
    expect(text).not.toContain("can't sync a private GitHub marketplace");
    expect(
      screen
        .getByRole("link", { name: "Add a marketplace or ZIP" })
        .getAttribute("href"),
    ).toBe("https://claude.com/docs/plugins/overview#find-and-add-a-plugin");
  });

  it("installs Claude Code from settings for the organization or the user", () => {
    openProvider("Claude Code");
    let text = bodyText();
    expect(text).toContain("Add to your Claude Code settings");
    expect(text).toContain("Restart Claude Code");
    // The snippet turns auto-update on, so there is no CLI step and no
    // auto-update warning.
    expect(text).not.toContain("plugin marketplace add");
    expect(text).not.toContain("/plugin install");
    expect(text).not.toContain("Auto-update is off");

    // My organization comes first and is selected by default; its radio is
    // described by the tagline only, not the revealed snippet.
    const org = screen.getByRole("radio", { name: /My organization/ });
    expect(screen.getAllByRole("radio")[0]).toBe(org);
    expect(org.getAttribute("aria-checked")).toBe("true");
    const describedBy = org.getAttribute("aria-describedby") ?? "";
    expect(document.getElementById(describedBy)?.textContent).toBe(
      "Managed settings, applied to everyone",
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Where are managed settings?" }),
    );
    text = bodyText();
    expect(text).toContain("Team or Enterprise Owners");
    expect(text).toContain("managed-settings.json");
    expect(text).toContain("Not fetched with a custom ANTHROPIC_BASE_URL");

    fireEvent.click(screen.getByRole("radio", { name: /Just me/ }));
    fireEvent.click(
      screen.getByRole("button", { name: "Already have a settings.json?" }),
    );
    text = bodyText();
    expect(text).toContain("~/.claude/settings.json");
    expect(text).toContain(
      "Otherwise merge each top-level key into the one in your file.",
    );
    expect(
      screen.queryByRole("button", { name: "Where are managed settings?" }),
    ).toBeNull();
  });

  it("keys Claude Code settings by the marketplace name", () => {
    const text = openProvider("Claude Code");
    expect(text).toContain(
      '"extraKnownMarketplaces": { "example-marketplace": { "autoUpdate": true,',
    );
    expect(text).toContain(
      '"enabledPlugins": { "example-plugin@example-marketplace": true }',
    );
    // The repository name differs from the marketplace name; only the
    // marketplace name may key the settings.
    expect(text).not.toContain("example-org-plugins");
    expect(text).toContain(
      "Use this exact marketplace name. Keep the URL private.",
    );
    expect(text).not.toContain("<org>-gram");
    expect(
      screen.getByRole("link", { name: /Docs/ }).getAttribute("href"),
    ).toBe(
      "https://code.claude.com/docs/en/plugins/org#require-a-marketplace-and-its-plugins",
    );
  });

  it("uses team-admin controls, current menus and explicit local snapshot updates for Cursor", () => {
    const text = openProvider("Cursor");
    expect(text).toContain(
      "Dashboard → Plugins & MCPs → Team Marketplaces → Add Marketplace → Import from Repo",
    );
    expect(text).toContain("ask an admin to enable Allow Local Plugin Imports");
    expect(text).toContain(
      "Dashboard → Settings → Security & Identity → Marketplace and Plugins",
    );
    expect(text).toContain("takes precedence over the local copy");
    expect(text).toContain("download a fresh ZIP");
    expect(text).toContain(
      "Bash on macOS or Linux with unzip and Python 3 installed",
    );
    expect(text).toContain("Default Off requires members to install");
    expect(text).toContain("unzip -tq");
    expect(text).toContain("plugin='example-plugin'");
    expect(text).not.toContain("rm -rf ~/.cursor/plugins/local");
    expect(text).toContain("Cursor GitHub App");
    expect(text).not.toContain(
      "turn on Allow Local Plugin Imports in Cursor's settings",
    );
  });

  it("does not offer an unresolved destructive command without a selected plugin", () => {
    const text = openProvider("Cursor", "");
    expect(text).toContain("Open the Install menu on a specific plugin");
    expect(text).not.toContain("GRAM_CURSOR_INSTALL");
  });

  it("installs the observability plugin into ~/.vibe/plugins for Mistral Vibe", () => {
    const text = openProvider("Mistral Vibe", "example-observability");
    // Quick install: the server-generated ZIP for platform=vibe, extracted
    // straight into the user-scope plugin directory under the real install
    // name from the publish status.
    expect(text).toContain("Download Plugin");
    expect(text).toContain(
      "unzip observability-vibe.zip -d ~/.vibe/plugins/example-observability",
    );
    // Manual install from the marketplace repository's <slug>-vibe/ directory:
    // clone into a fresh temp dir chained with &&, clear the destination, copy.
    expect(text).toContain("tmp=$(mktemp -d)");
    expect(text).toContain(`trap 'rm -rf "$tmp"' EXIT`);
    expect(text).toContain(
      'git clone https://github.com/example-owner/example-org-plugins "$tmp/marketplace"',
    );
    expect(text).toContain("rm -rf ~/.vibe/plugins/example-observability");
    expect(text).toContain(
      'cp -R "$tmp/marketplace/example-observability-vibe" ~/.vibe/plugins/example-observability',
    );
    expect(text).toContain("/reload-plugins");
    expect(text).toContain("ai.mistral.vibe/hooks.toml");
    // MCP servers are added with `vibe mcp add` (which writes the OAuth auth
    // table) and signed in with /mcp login — not a hand-written block.
    expect(text).toContain("vibe mcp add <server-name> --url <mcp-server-url>");
    expect(text).toContain("/mcp login <server-name>");
    expect(text).toContain("~/.vibe/config.toml");
  });

  it("offers Mistral Vibe without the disabled Mistral coming-soon tile", () => {
    render(
      <TooltipProvider>
        <InstallInstructionsDialog
          open
          onOpenChange={vi.fn<() => void>()}
          repoOwner="example-owner"
          repoName="example-org-plugins"
          marketplaceUrl="https://example.invalid/marketplace"
          pluginName="Example plugin"
          pluginSlug="example-plugin"
        />
      </TooltipProvider>,
    );
    const mistralButtons = screen.getAllByRole("button", { name: /^Mistral/ });
    expect(mistralButtons.map((b) => b.textContent?.trim())).toEqual([
      "Mistral Vibe",
    ]);
    // The surviving Mistral tile must be enabled, not another coming-soon entry.
    expect(mistralButtons[0]?.getAttribute("disabled")).toBeNull();
  });

  it("keeps the feature plugin's <slug>-vibe copy steps alongside the Quick install for Mistral Vibe", () => {
    // A specific feature plugin installs from the marketplace repo's
    // <slug>-vibe/ directory under its bare slug — and the observability
    // Quick install still renders, since the org's observability package is
    // published.
    const text = openProvider("Mistral Vibe", "example-plugin");
    expect(text).toContain("Download Plugin");
    expect(text).toContain("example-plugin-vibe/");
    expect(text).toContain("rm -rf ~/.vibe/plugins/example-plugin");
    expect(text).toContain(
      'cp -R "$tmp/marketplace/example-plugin-vibe" ~/.vibe/plugins/example-plugin',
    );
    expect(text).toContain("/reload-plugins");
    // The feature plugin's manual copy does not claim automatic hooks — that
    // note belongs to the observability Quick install above it.
    expect(text).not.toContain("ships with the package");
    expect(text).toContain("vibe mcp add <server-name> --url <mcp-server-url>");
  });

  it("shows the reason instead of Vibe copy steps for a plugin that is not AP1.0-portable", () => {
    render(
      <TooltipProvider>
        <InstallInstructionsDialog
          open
          onOpenChange={vi.fn<() => void>()}
          repoOwner="example-owner"
          repoName="example-org-plugins"
          marketplaceUrl="https://example.invalid/marketplace"
          pluginName="Example plugin"
          pluginSlug="example-plugin"
          candidatePlugins={[
            {
              name: "Example plugin",
              slug: "example-plugin",
              agentPluginsV1Compatible: false,
            },
          ]}
        />
      </TooltipProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: /Mistral Vibe/ }));
    const text = document.body.textContent!.replace(/\s+/g, " ");
    expect(text).toContain(
      "not published as a portable Agent Plugins 1.0 package",
    );
    // The reason names the missing package directory, but no copy command.
    expect(text).not.toContain("cp -R");
    expect(text).not.toContain("git clone");
  });

  it.each(["publish", "manage"] as const)(
    "qualifies GitHub invitations in %s mode",
    (mode) => {
      render(
        <PublishDialog
          open
          onOpenChange={vi.fn<() => void>()}
          onPublish={vi.fn<() => void>()}
          isPending={false}
          mode={mode}
        />,
      );
      const text = document.body.textContent!.replace(/\s+/g, " ");
      expect(text).toContain(
        "New collaborators may receive a GitHub invitation",
      );
      expect(text).toContain("Existing collaborators need no new invitation");
      expect(text).not.toContain("will receive an email");
      expect(text).not.toContain("must accept it before they can install");
      if (mode === "publish") {
        expect(text).toContain("ZIP recipients and Claude Code users");
        expect(text).toContain("do not each need to be GitHub collaborators");
      }
    },
  );
});
