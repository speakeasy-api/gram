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
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: string }) => <pre>{children}</pre>,
}));

afterEach(() => {
  cleanup();
});

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
        repoName="example-marketplace"
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

  it("qualifies Claude server policy eligibility and activation", () => {
    const text = openProvider("Claude Code");
    expect(text).toContain("Owner or Primary Owner");
    expect(text).toContain(
      "Endpoint-managed policy files and MDM are a separate mechanism",
    );
    expect(text).toContain("ANTHROPIC_BASE_URL skip the settings fetch");
    expect(text).toContain("Start a new session");
    expect(text).toContain("token-bearing marketplace URL as a secret");
    expect(text).not.toContain("every Claude Code installation");
  });

  it("keys Claude Code settings by the marketplace name and turns on auto-update", () => {
    const text = openProvider("Claude Code");
    expect(text).toContain(
      '"extraKnownMarketplaces": { "example-marketplace": { "autoUpdate": true,',
    );
    expect(text).toContain(
      '"enabledPlugins": { "example-plugin@example-marketplace": true }',
    );
    expect(text).toContain(
      "must be exactly example-marketplace, not the GitHub repository name or an older <org>-gram name",
    );
    expect(text).toContain("Otherwise Claude Code ignores autoUpdate");
    expect(text).toContain(
      "open /plugin → Marketplaces, select example-marketplace, and choose Enable auto-update",
    );
    expect(
      screen
        .getByRole("link", { name: /Claude Code docs/ })
        .getAttribute("href"),
    ).toBe(
      "https://code.claude.com/docs/en/plugins/org#require-a-marketplace-and-its-plugins",
    );
    expect(
      screen
        .getByRole("link", { name: /Turn on auto-update/ })
        .getAttribute("href"),
    ).toBe(
      "https://code.claude.com/docs/en/plugins/host-marketplace#turn-on-auto-update",
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
