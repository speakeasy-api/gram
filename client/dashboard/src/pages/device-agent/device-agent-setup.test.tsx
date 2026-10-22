import { TooltipProvider } from "@/components/ui/Tooltip";
import type { UseAgentToken } from "@/hooks/useAgentToken";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DeviceAgentSetup, ManagedProfileExample } from "./device-agent-setup";

// buildCopyText is what useAgentToken writes to the clipboard after minting,
// captured so tests can check the copied managed.json.
const agentToken = vi.hoisted(() => ({
  current: {} as UseAgentToken,
  buildCopyText: (_token: string): string => "",
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ slug: "example-corp", name: "Example Corp" }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ apiKeys: { href: () => "/example-corp/api-keys" } }),
}));
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: string }) => <pre>{children}</pre>,
}));
vi.mock("@/hooks/useAgentToken", () => ({
  useAgentToken: (opts: { buildCopyText: (token: string) => string }) => {
    agentToken.buildCopyText = opts.buildCopyText;
    return agentToken.current;
  },
}));

function renderSetup(
  token: Partial<UseAgentToken>,
  ui: React.ReactElement = <DeviceAgentSetup />,
) {
  agentToken.current = {
    generatedToken: null,
    autoCopied: false,
    isPending: false,
    isError: false,
    canGenerate: true,
    hasExistingAgentKey: false,
    generate: vi.fn<() => void>(),
    ...token,
  };
  render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <MemoryRouter>{ui}</MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return agentToken.current;
}

afterEach(cleanup);

describe("DeviceAgentSetup organization values", () => {
  it("shows org_slug with a copy button outside the platform walkthroughs", () => {
    renderSetup({});

    expect(screen.getByText("example-corp")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy org_slug" })).toBeTruthy();
  });

  it("mints a token directly when none exists", () => {
    const token = renderSetup({});

    fireEvent.click(screen.getByRole("button", { name: /Generate token/ }));

    expect(token.generate).toHaveBeenCalledOnce();
  });

  it("re-generates without a confirmation when a token already exists", () => {
    const token = renderSetup({ hasExistingAgentKey: true });

    fireEvent.click(screen.getByRole("button", { name: /Re-generate token/ }));

    expect(screen.queryByRole("dialog")).toBeNull();
    expect(token.generate).toHaveBeenCalledOnce();
  });

  it("shows a minted token once with a copy button", () => {
    renderSetup({ generatedToken: "spk_org_test" });

    expect(screen.getByText("spk_org_test")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy org_token" })).toBeTruthy();
    expect(screen.getByText(/shown only once/)).toBeTruthy();
  });

  it("disables minting without org:admin", () => {
    renderSetup({ canGenerate: false });

    const button = screen.getByRole("button", { name: /Generate token/ });
    expect((button as HTMLButtonElement).disabled).toBe(true);
  });
});

describe("ManagedProfileExample auto_update", () => {
  it.each([
    ["macos", "automatic"],
    ["linux", "automatic"],
    ["windows", "notify"],
  ] as const)("ships %s fleets with %s", (os, mode) => {
    renderSetup({}, <ManagedProfileExample os={os} />);

    expect(document.body.textContent).toContain(`"auto_update": "${mode}"`);
    expect(screen.queryByText(/On Windows, set/)).toBeNull();
  });

  it.each([
    ["macos", "automatic"],
    ["linux", "automatic"],
    ["windows", "notify"],
  ] as const)(
    "shows and copies a minted token with %s's %s mode",
    (os, mode) => {
      renderSetup(
        { generatedToken: "spk_org_test" },
        <ManagedProfileExample os={os} />,
      );

      expect(document.body.textContent).toContain(
        `"org_token": "spk_org_test"`,
      );
      expect(document.body.textContent).toContain(`"auto_update": "${mode}"`);
      expect(
        JSON.parse(agentToken.buildCopyText("spk_org_test")),
      ).toMatchObject({ org_token: "spk_org_test", auto_update: mode });
    },
  );

  it("uses automatic and calls out Windows when no OS is given", () => {
    renderSetup({}, <ManagedProfileExample />);

    expect(document.body.textContent).toContain(`"auto_update": "automatic"`);
    expect(screen.getByText(/On Windows, set/)).toBeTruthy();
  });
});
