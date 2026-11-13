import { TooltipProvider } from "@/components/ui/Tooltip";
import { act, cleanup, render, screen } from "@testing-library/react";
import type { FleetDevice } from "@gram/client/models/components/fleetdevice.js";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

const sheet = vi.hoisted(() => ({
  onOpenChange: undefined as ((open: boolean) => void) | undefined,
}));

// The panel has its own tests; this suite checks which device it is given.
vi.mock("./device-detail-sheet", () => ({
  DeviceDetailSheet: ({
    device,
    open,
    onOpenChange,
  }: {
    device: FleetDevice | null;
    open: boolean;
    onOpenChange: (open: boolean) => void;
  }) => {
    sheet.onOpenChange = onOpenChange;
    return (
      <div data-testid="sheet" data-open={String(open)}>
        {device?.hostname ?? "none"}
      </div>
    );
  },
}));

import { DeviceTable } from "./coverage-widgets";

function device(
  id: string,
  hostname: string,
  overrides: Partial<FleetDevice> = {},
): FleetDevice {
  return {
    id,
    inInventory: true,
    provider: "jamf",
    externalId: `mdm-${id}`,
    hostname,
    coverageBucket: "agent_active",
    firstSeenAt: new Date("2026-10-01T00:00:00Z"),
    lastSeenAt: new Date("2026-10-29T00:00:00Z"),
    ...overrides,
  } as FleetDevice;
}

const agentOnly = (id: string, hostname: string) =>
  device(id, hostname, {
    inInventory: false,
    provider: undefined,
    externalId: undefined,
  });

function Search() {
  return <div data-testid="search">{useLocation().search}</div>;
}

function renderTable(
  devices: FleetDevice[],
  entry: { pathname: string; search?: string; state?: unknown },
  status: { isLoading?: boolean; isError?: boolean } = {},
  shape: { variant?: "inventory" | "fleet"; mdmConnected?: boolean } = {},
) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <TooltipProvider>
        <DeviceTable
          {...shape}
          devices={devices}
          isLoading={status.isLoading ?? false}
          isError={status.isError}
        />
      </TooltipProvider>
      <Search />
    </MemoryRouter>,
  );
}

describe("DeviceTable linked device", () => {
  afterEach(() => cleanup());

  it("opens the panel for the device a link names", () => {
    renderTable([device("a", "alpha-mbp"), device("b", "beta-mbp")], {
      pathname: "/devices",
      search: "?device=b",
    });

    expect(screen.getByTestId("sheet").dataset.open).toBe("true");
    expect(screen.getByTestId("sheet").textContent).toBe("beta-mbp");
  });

  it("opens from the linked snapshot before that device's page has loaded", () => {
    const later = device("later", "later-mbp");
    renderTable([device("a", "alpha-mbp")], {
      pathname: "/devices",
      search: "?device=later",
      state: { device: later },
    });

    expect(screen.getByTestId("sheet").dataset.open).toBe("true");
    expect(screen.getByTestId("sheet").textContent).toBe("later-mbp");
  });

  it.each([
    ["loading", { isLoading: true }],
    ["failed", { isError: true }],
  ])("opens the linked snapshot while the inventory is %s", (_, status) => {
    const linked = device("linked", "linked-mbp");
    renderTable(
      [],
      {
        pathname: "/devices",
        search: "?device=linked",
        state: { device: linked },
      },
      status,
    );

    expect(screen.getByTestId("sheet").dataset.open).toBe("true");
    expect(screen.getByTestId("sheet").textContent).toBe("linked-mbp");
  });

  it("ignores a snapshot for a different device", () => {
    renderTable([device("a", "alpha-mbp")], {
      pathname: "/devices",
      search: "?device=missing",
      state: { device: device("other", "other-mbp") },
    });

    expect(screen.getByTestId("sheet").dataset.open).toBe("false");
  });

  it("drops the link on close and keeps the device while the panel slides out", () => {
    renderTable([device("a", "alpha-mbp")], {
      pathname: "/devices",
      search: "?device=a",
    });

    act(() => sheet.onOpenChange?.(false));

    expect(screen.getByTestId("search").textContent).toBe("");
    expect(screen.getByTestId("sheet").dataset.open).toBe("false");
    expect(screen.getByTestId("sheet").textContent).toBe("alpha-mbp");
  });
});

describe("DeviceTable on the Devices tab", () => {
  afterEach(() => cleanup());

  const headers = () =>
    screen.getAllByRole("columnheader").map((header) => header.textContent);

  it("leaves out MDM columns and badges with no MDM connected", () => {
    renderTable(
      [agentOnly("agent:a", "alpha-mbp")],
      { pathname: "/devices" },
      {},
      { variant: "fleet", mdmConnected: false },
    );

    expect(headers()).toContain("User");
    expect(headers()).not.toContain("MDM check-in");
    expect(screen.queryByText("Not in an MDM")).toBeNull();
    expect(screen.queryByRole("button", { name: /source/i })).toBeNull();
  });

  it("names a machine by its serial when the hostname is empty", () => {
    renderTable(
      [device("agent:a", "", { inInventory: false, serialNumber: "SER-1" })],
      { pathname: "/devices" },
      {},
      { variant: "fleet", mdmConnected: false },
    );

    expect(screen.getAllByText("SER-1").length).toBeGreaterThan(0);
  });

  it("marks the machines no connected MDM holds", () => {
    renderTable(
      [device("mdm:a", "alpha-mbp"), agentOnly("agent:b", "beta-mbp")],
      { pathname: "/devices" },
      {},
      { variant: "fleet", mdmConnected: true },
    );

    expect(headers()).toContain("MDM check-in");
    expect(screen.getAllByText("Not in an MDM")).toHaveLength(1);
    expect(screen.getByRole("button", { name: /source/i })).toBeTruthy();
  });

  it("keeps the inventory's columns on an MDM page", () => {
    renderTable([device("mdm:a", "alpha-mbp")], { pathname: "/devices" });

    expect(headers()).toContain("Assigned user");
    expect(headers()).toContain("MDM check-in");
  });
});
