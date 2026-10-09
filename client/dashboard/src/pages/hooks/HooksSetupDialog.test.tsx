import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { HooksSetupDialog } from "./HooksSetupDialog";

const { state, defaults } = vi.hoisted(() => {
  const defaults = (): {
    marketplaceSettings: {
      data?: { effectiveName: string };
      isPending: boolean;
    };
    observabilityPlugin?: string;
  } => ({
    marketplaceSettings: {
      data: { effectiveName: "example-marketplace" },
      isPending: false,
    },
    observabilityPlugin: "example-observability",
  });
  return { state: defaults(), defaults };
});

vi.mock("@gram/client/react-query/marketplaceSettings", () => ({
  useMarketplaceSettings: () => state.marketplaceSettings,
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({
    data: {
      configured: false,
      connected: false,
      marketplaceUrl: "https://example.invalid/marketplace.git",
      claudeObservabilityPlugin: state.observabilityPlugin,
    },
  }),
}));
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: string }) => <pre>{children}</pre>,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ plugins: { href: () => "/plugins" } }),
}));

afterEach(() => {
  cleanup();
  Object.assign(state, defaults());
});

function claudeText(): string {
  render(
    <TooltipProvider>
      <HooksSetupDialog
        open
        onOpenChange={vi.fn<() => void>()}
        defaultProvider="claude"
      />
    </TooltipProvider>,
  );
  return document.body.textContent!.replace(/\s+/g, " ");
}

describe("HooksSetupDialog Claude Code instructions", () => {
  it("enables the plugin with enabledPlugins under the marketplace name", () => {
    const text = claudeText();
    expect(text).toContain(
      '"extraKnownMarketplaces": { "example-marketplace": { "autoUpdate": true,',
    );
    expect(text).toContain(
      '"enabledPlugins": { "example-observability@example-marketplace": true }',
    );
    // Claude Code has no plugins.required setting; a snippet using it enabled
    // nothing.
    expect(text).not.toContain('"required"');
    expect(text).toContain("Use this exact marketplace name.");
  });

  it("installs from settings, with no CLI step to run", () => {
    const text = claudeText();
    expect(text).toContain("Add to your Claude Code settings");
    expect(text).toContain("Restart Claude Code");
    expect(text).not.toContain("claude plugin marketplace add");
    expect(text).not.toContain("claude plugin install");
    expect(text).not.toContain("Auto-update is off");
  });

  it("still registers the marketplace when there is no observability plugin", () => {
    state.observabilityPlugin = undefined;
    const text = claudeText();
    expect(text).toContain("no observability plugin yet");
    expect(text).toContain('"extraKnownMarketplaces": { "example-marketplace"');
    expect(text).not.toContain("enabledPlugins");
  });

  it("waits for the marketplace name instead of hiding the instructions", () => {
    state.marketplaceSettings = { data: undefined, isPending: true };
    const text = claudeText();
    expect(text).toContain("Add to your Claude Code settings");
    expect(text).toContain("Loading the marketplace name");
    expect(text).not.toContain("Publish your plugins to GitHub first");
  });
});
