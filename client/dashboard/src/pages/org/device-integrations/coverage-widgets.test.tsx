import { TooltipProvider } from "@/components/ui/Tooltip";
import { act, cleanup, render, screen } from "@testing-library/react";
import type { ManagedDevice } from "@gram/client/models/components/manageddevice.js";
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
    device: ManagedDevice | null;
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

import { ManagedDeviceTable } from "./coverage-widgets";

function device(id: string, hostname: string): ManagedDevice {
  return {
    id,
    provider: "jamf",
    externalId: `mdm-${id}`,
    hostname,
    coverageBucket: "agent_active",
    firstSeenAt: new Date("2026-10-01T00:00:00Z"),
    lastSeenAt: new Date("2026-10-29T00:00:00Z"),
  } as ManagedDevice;
}

function Search() {
  return <div data-testid="search">{useLocation().search}</div>;
}

function renderTable(
  devices: ManagedDevice[],
  entry: { pathname: string; search?: string; state?: unknown },
  status: { isLoading?: boolean; isError?: boolean } = {},
) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <TooltipProvider>
        <ManagedDeviceTable
          devices={devices}
          isLoading={status.isLoading ?? false}
          isError={status.isError}
        />
      </TooltipProvider>
      <Search />
    </MemoryRouter>,
  );
}

describe("ManagedDeviceTable linked device", () => {
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
