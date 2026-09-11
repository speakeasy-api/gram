import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { InstallInstructionsDialog } from "./InstallInstructionsDialog";

vi.mock("@gram/client/react-query/marketplaceSettings", () => ({
  useMarketplaceSettings: () => ({ data: { effectiveName: "example" } }),
}));
vi.mock("@gram/client/react-query/plugins", () => ({
  usePlugins: () => ({ data: { plugins: [] } }),
}));
vi.mock("@/contexts/Fetcher", () => ({
  useFetcher: () => ({ fetch: vi.fn() }),
}));
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: ReactNode }) => <pre>{children}</pre>,
}));

afterEach(cleanup);

function renderInstructions() {
  render(
    <TooltipProvider>
      <InstallInstructionsDialog
        open
        onOpenChange={vi.fn<(open: boolean) => void>()}
        repoOwner="example"
        repoName="plugins"
        marketplaceUrl="https://example.test/marketplace.git"
        candidatePlugins={[{ name: "Default", slug: "default" }]}
      />
    </TooltipProvider>,
  );
}

describe("InstallInstructionsDialog", () => {
  it("does not offer an empty OpenClaw instruction step", () => {
    renderInstructions();
    const button = screen.getByRole("button", { name: /OpenClaw/ });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    expect(button.textContent).toContain("Coming soon");
  });

  it.each([
    ["Claude Code", "Install in your Claude Code instance"],
    ["Claude Cowork", "Roll out to your organization"],
    ["Cursor", "Roll out to your team in Cursor"],
    ["OpenAI Codex", "Quick install"],
    ["opencode", "Connect an MCP server"],
    ["GitHub Copilot", "Quick install"],
  ])(
    "renders instructions for %s without a device-agent requirement",
    (provider, heading) => {
      renderInstructions();
      fireEvent.click(screen.getByRole("button", { name: provider }));
      expect(screen.getByRole("heading", { name: heading })).toBeTruthy();
      expect(screen.getByRole("button", { name: "Done" })).toBeTruthy();
      expect(screen.queryByText(/device.agent|speakeasy-agent/i)).toBeNull();
    },
  );
});
