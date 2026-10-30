import { TooltipProvider } from "@/components/ui/Tooltip";
import type { FleetCoverage } from "@gram/client/models/components/fleetcoverage.js";
import type { FleetDevice } from "@gram/client/models/components/fleetdevice.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

const queries = vi.hoisted(() => ({
  coverage: undefined as FleetCoverage | undefined,
  devices: [] as FleetDevice[],
}));

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    deviceAgent: {
      href: () => "/org/device-agent",
      mdmIntegrations: { href: () => "/org/device-agent/mdm-integrations" },
    },
  }),
}));
vi.mock("@gram/client/react-query/fleetCoverage.js", () => ({
  useFleetCoverage: () => ({
    data: queries.coverage,
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/fleetDevices.js", () => ({
  useFleetDevicesInfinite: () => ({
    data: { pages: [{ result: { devices: queries.devices } }] },
    isLoading: false,
    isError: false,
    hasNextPage: false,
    isFetchingNextPage: false,
    refetch: vi.fn(),
    fetchNextPage: vi.fn(),
  }),
}));
// The panel has its own tests.
vi.mock("@/pages/org/device-integrations/device-detail-sheet", () => ({
  DeviceDetailSheet: () => null,
}));

import { DeviceAgentDevicesTab } from "./device-agent-devices";

function coverage(overrides: Partial<FleetCoverage>): FleetCoverage {
  return {
    organizationId: "org",
    activeWindowMinutes: 60,
    mdmConnected: false,
    attestation: "device",
    agentActive: 0,
    agentActiveDeviceAttested: 0,
    agentStale: 0,
    agentOtherDevice: 0,
    noAgent: 0,
    noEmail: 0,
    unresolvedEmail: 0,
    missing: 0,
    agentOnlyDevices: 0,
    totalDevices: 0,
    policyAttested: 0,
    policyOutdated: 0,
    policyUnreported: 0,
    enforcementHalted: 0,
    ...overrides,
  };
}

function agentDevice(id: string, hostname: string): FleetDevice {
  return {
    id,
    inInventory: false,
    hostname,
    coverageBucket: "agent_active",
    policyState: "attested",
    firstSeenAt: new Date("2026-10-01T00:00:00Z"),
    lastSeenAt: new Date("2026-10-29T00:00:00Z"),
  } as FleetDevice;
}

function renderTab() {
  render(
    <MemoryRouter>
      <TooltipProvider>
        <DeviceAgentDevicesTab />
      </TooltipProvider>
    </MemoryRouter>,
  );
}

describe("DeviceAgentDevicesTab", () => {
  afterEach(() => {
    cleanup();
    queries.coverage = undefined;
    queries.devices = [];
  });

  it("points at the install steps before any agent reports", () => {
    queries.coverage = coverage({});
    renderTab();

    expect(screen.getByText(/No devices yet/)).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Install the agent" })
        .getAttribute("href"),
    ).toBe("/org/device-agent");
  });

  it("lists agent machines without an MDM and hides the MDM gaps", () => {
    queries.coverage = coverage({
      agentActive: 2,
      agentActiveDeviceAttested: 2,
      policyAttested: 2,
      agentOnlyDevices: 2,
      totalDevices: 2,
    });
    queries.devices = [
      agentDevice("agent:a", "alpha-mbp"),
      agentDevice("agent:b", "beta-mbp"),
    ];
    renderTab();

    expect(screen.getByText("alpha-mbp")).toBeTruthy();
    expect(screen.getByText("beta-mbp")).toBeTruthy();
    // No MDM lists the devices without the agent, so a "0" there would
    // read as full coverage.
    expect(screen.queryByText("No agent")).toBeNull();
    expect(screen.queryByText("Not in an MDM")).toBeNull();
    expect(
      screen.getByRole("link", { name: "Connect an MDM" }).getAttribute("href"),
    ).toBe("/org/device-agent/mdm-integrations");
  });

  it("shows the gaps and the agent-only count once an MDM is connected", () => {
    queries.coverage = coverage({
      mdmConnected: true,
      agentActive: 1,
      agentActiveDeviceAttested: 1,
      policyAttested: 1,
      noAgent: 3,
      agentOnlyDevices: 1,
      totalDevices: 4,
    });
    queries.devices = [agentDevice("agent:a", "alpha-mbp")];
    renderTab();

    expect(screen.getAllByText("No agent").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Not in an MDM").length).toBeGreaterThan(0);
    expect(screen.queryByRole("link", { name: "Connect an MDM" })).toBeNull();
  });
});
