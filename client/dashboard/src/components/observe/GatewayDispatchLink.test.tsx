import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router";
import type { ReactNode } from "react";
import { encodeIdentityUrn } from "@/lib/identity-urn";
import { GatewayDispatchLink } from "./GatewayDispatchLink";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "viewed" }),
}));
vi.mock("@/components/ui/Tooltip", () => ({
  SimpleTooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
afterEach(cleanup);

function renderLink(trace: Parameters<typeof GatewayDispatchLink>[0]["trace"]) {
  render(
    <MemoryRouter>
      <GatewayDispatchLink trace={trace} />
    </MemoryRouter>,
  );
}

function gatewayLinkHref(name: string): string | null {
  return screen
    .getByRole("link", { name: `Open gateway "${name}"` })
    .getAttribute("href");
}

describe("GatewayDispatchLink", () => {
  it("opens a stored gateway in its own project", () => {
    renderLink({
      viaMetaMcpServerId: "gateway-1",
      viaMetaMcpServerName: "Sales gateway",
      viaMetaMcpServerKind: "project",
      viaMetaMcpServerProjectSlug: "sales",
    });
    expect(gatewayLinkHref("Sales gateway")).toBe(
      "/org/projects/sales/mcp/gateway/gateway-1/overview",
    );
  });

  it("opens the organization gateway from the viewed project", () => {
    renderLink({
      viaMetaMcpServerId: "org-gateway",
      viaMetaMcpServerName: "Acme",
      viaMetaMcpServerKind: "organization",
    });
    expect(gatewayLinkHref("Acme")).toBe(
      "/org/projects/viewed/mcp/organization-gateway",
    );
  });

  it("opens an organization gateway restricted to a project in that project", () => {
    renderLink({
      viaMetaMcpServerId: "project-org-gateway",
      viaMetaMcpServerName: "Sales",
      viaMetaMcpServerKind: "organization",
      viaMetaMcpServerProjectSlug: "sales",
    });
    expect(gatewayLinkHref("Sales")).toBe(
      "/org/projects/sales/mcp/organization-gateway",
    );
  });

  it("opens an agent gateway on the permissions its servers come from", () => {
    renderLink({
      viaMetaMcpServerId: "agent-gateway",
      viaMetaMcpServerName: "Support bot gateway",
      viaMetaMcpServerKind: "agent",
      viaMetaMcpServerAgentId: "agent-1",
    });
    expect(gatewayLinkHref("Support bot gateway")).toBe(
      `/org/projects/viewed/identities/${encodeIdentityUrn("agent:agent-1")}/permissions`,
    );
  });

  it.each([
    {
      case: "an unknown gateway",
      trace: {
        viaMetaMcpServerId: "unknown",
        viaMetaMcpServerName: "Unknown gateway",
      },
    },
    {
      case: "a deleted stored gateway",
      trace: {
        viaMetaMcpServerId: "gateway-1",
        viaMetaMcpServerName: "Old gateway",
        viaMetaMcpServerKind: "project" as const,
      },
    },
    {
      case: "a deleted agent's gateway",
      trace: {
        viaMetaMcpServerId: "agent-gateway",
        viaMetaMcpServerName: "Old bot gateway",
        viaMetaMcpServerKind: "agent" as const,
      },
    },
  ])("keeps $case as a plain icon", ({ trace }) => {
    renderLink(trace);
    expect(screen.queryByRole("link")).toBeNull();
    expect(
      screen.getByRole("img", {
        name: `Dispatched through gateway "${trace.viaMetaMcpServerName}"`,
      }),
    ).toBeTruthy();
  });
});
