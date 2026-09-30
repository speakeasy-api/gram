import { TooltipProvider } from "@/components/ui/Tooltip";
import type { UseAgentToken } from "@/hooks/useAgentToken";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DeviceAgentSetup } from "./device-agent-setup";

const agentToken = vi.hoisted(() => ({ current: {} as UseAgentToken }));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ slug: "example-corp", name: "Example Corp" }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ apiKeys: { href: () => "/example-corp/api-keys" } }),
}));
vi.mock("@/hooks/useAgentToken", () => ({
  useAgentToken: () => agentToken.current,
}));

function renderSetup(token: Partial<UseAgentToken>) {
  agentToken.current = {
    generatedToken: null,
    autoCopied: false,
    isPending: false,
    isError: false,
    canGenerate: true,
    hasExistingAgentKey: false,
    generate: vi.fn(),
    ...token,
  };
  render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <MemoryRouter>
          <DeviceAgentSetup />
        </MemoryRouter>
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

  it("confirms before rotating an existing token", () => {
    const token = renderSetup({ hasExistingAgentKey: true });

    fireEvent.click(screen.getByRole("button", { name: /Rotate token/ }));
    expect(token.generate).not.toHaveBeenCalled();

    const dialog = screen.getByRole("dialog");
    fireEvent.click(
      Array.from(dialog.querySelectorAll("button")).find(
        (b) => b.textContent === "Rotate token",
      )!,
    );
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
