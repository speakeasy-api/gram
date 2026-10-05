import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { HooksSetupDialog } from "./HooksSetupDialog";

vi.mock("@gram/client/react-query/marketplaceSettings", () => ({
  useMarketplaceSettings: () => ({
    data: { effectiveName: "example-marketplace" },
  }),
}));
vi.mock("@gram/client/react-query/publishStatus", () => ({
  usePublishStatus: () => ({
    data: {
      configured: false,
      connected: false,
      marketplaceUrl: "https://example.invalid/marketplace.git",
      claudeObservabilityPlugin: "example-observability",
    },
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ plugins: { href: () => "/plugins" } }),
}));

afterEach(cleanup);

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
    expect(text).toContain("must be exactly example-marketplace");
  });

  it("tells users who add the marketplace themselves to turn on auto-update", () => {
    const text = claudeText();
    expect(text).toContain(
      "claude plugin marketplace add https://example.invalid/marketplace.git",
    );
    expect(text).toContain(
      "select example-marketplace, and choose Enable auto-update",
    );
  });
});
