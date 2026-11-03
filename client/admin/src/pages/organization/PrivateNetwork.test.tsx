import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { PrivateNetwork } from "@/pages/organization/PrivateNetwork";
import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";
import type { AdminOrganizationNetworkIngress } from "@gram/admin-client/models/components/adminorganizationnetworkingress";

const mocks = vi.hoisted(() => ({
  fetchIngress: vi.fn(),
  move: vi.fn(),
}));

vi.mock("@/lib/gramAdminClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminClient")>();
  return {
    ...actual,
    organizationNetworkIngressQuery: (organizationID: string) => ({
      queryKey: ["admin", "networkIngress", organizationID],
      queryFn: () => mocks.fetchIngress(organizationID),
      retry: false,
    }),
    moveOrganizationNetworkIngressToGateway: mocks.move,
  };
});

const ORG = anOrganization();

function ingressState(
  overrides: Partial<
    NonNullable<AdminOrganizationNetworkIngress["ingress"]>
  > = {},
): AdminOrganizationNetworkIngress {
  return {
    organizationId: ORG.id,
    gatewayTag: "tag:speakeasy",
    ingress: {
      id: "00000000-0000-0000-0000-000000000001",
      provider: "tailscale",
      hostname: "private",
      dnsName: "private.example.ts.net",
      runtime: "operator",
      status: "online",
      enabled: true,
      credentialsConfigured: true,
      createdAt: new Date("2026-10-01T00:00:00Z"),
      gatewayNodes: [],
      ...overrides,
    },
  };
}

describe("PrivateNetwork", () => {
  beforeEach(() => {
    mocks.fetchIngress.mockReset();
    mocks.move.mockReset();
  });
  afterEach(() => cleanup());

  it("moves an operator-hosted ingress after staff confirm the prerequisites", async () => {
    mocks.fetchIngress.mockResolvedValue(ingressState());
    mocks.move.mockResolvedValue(
      ingressState({ runtime: "gateway", status: "pending" }),
    );
    await renderWithApp(<PrivateNetwork org={ORG} />);

    expect(await screen.findByText("Tailscale operator")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Move to tailnet gateway" }),
    );

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getAllByText("tag:speakeasy").length).toBeGreaterThan(
      0,
    );
    expect(within(dialog).getByText("svc:private")).toBeTruthy();
    expect(mocks.move).not.toHaveBeenCalled();

    fireEvent.click(
      within(dialog).getByRole("button", { name: "Move to tailnet gateway" }),
    );
    await waitFor(() => expect(mocks.move).toHaveBeenCalledWith(ORG.id));
    expect(await screen.findByText("Tailnet gateway")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Move to tailnet gateway" }),
    ).toBeNull();
  });

  it("does nothing when staff cancel the move", async () => {
    mocks.fetchIngress.mockResolvedValue(ingressState());
    await renderWithApp(<PrivateNetwork org={ORG} />);

    fireEvent.click(
      await screen.findByRole("button", { name: "Move to tailnet gateway" }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(mocks.move).not.toHaveBeenCalled();
  });

  it("lists the gateway nodes of a moved ingress", async () => {
    mocks.fetchIngress.mockResolvedValue(
      ingressState({
        runtime: "gateway",
        gatewayNodes: [
          {
            slot: 0,
            status: "online",
            podName: "gram-tailnet-gateway-0",
            retired: false,
          },
          {
            slot: 1,
            status: "error",
            lastError: "login timed out",
            retired: false,
          },
          { slot: 2, status: "stopped", retired: true },
        ],
      }),
    );
    await renderWithApp(<PrivateNetwork org={ORG} />);

    expect(await screen.findByText("Gateway nodes")).toBeTruthy();
    expect(screen.getByText("gram-tailnet-gateway-0")).toBeTruthy();
    expect(screen.getByText("login timed out")).toBeTruthy();
    expect(screen.getByText("retiring")).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Move to tailnet gateway" }),
    ).toBeNull();
  });

  it("explains that an organization without a connection will start on the gateway", async () => {
    mocks.fetchIngress.mockResolvedValue({
      organizationId: ORG.id,
      gatewayTag: "tag:speakeasy",
    });
    await renderWithApp(<PrivateNetwork org={ORG} />);

    expect(
      await screen.findByText(/When it does, it starts on the tailnet gateway/),
    ).toBeTruthy();
  });
});
