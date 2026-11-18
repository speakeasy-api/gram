import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { anOrganization } from "@/test/fixtures";
import { renderWithApp } from "@/test/harness";

import { HookFleetView } from "./HookFleet";

const FLEET = {
  organization_id: "org_1",
  window_days: 30,
  since: "2026-10-11T00:00:00Z",
  latest_version: "0.3.46",
  quiet_after_days: 3,
  truncated: false,
  total_installs: 2,
  quiet_installs: 1,
  legacy_events: 12,
  versions: [
    { version: "0.3.46", status: "current", installs: 1 },
    { version: "", status: "unknown", installs: 1 },
  ],
  routes: [{ value: "marketplace", installs: 1, events: 0 }],
  generations: [
    { value: "ingest_binary", installs: 1, events: 40 },
    { value: "legacy_keyed", installs: 1, events: 12 },
  ],
  installs: [
    {
      project_id: "project_1",
      user_email: "dev@example.com",
      hostname: "dev-laptop",
      harness: "claude",
      version: "0.3.46",
      version_status: "current",
      endpoint_generation: "ingest_binary",
      install_route: "marketplace",
      first_seen: "2026-11-01T00:00:00Z",
      last_seen: "2026-11-09T00:00:00Z",
      events: 40,
      errors: 0,
      quiet: false,
    },
    {
      project_id: "project_1",
      user_email: "old@example.com",
      hostname: "old-laptop",
      harness: "claude",
      version: "",
      version_status: "unknown",
      endpoint_generation: "legacy_keyed",
      install_route: "",
      first_seen: "2026-10-20T00:00:00Z",
      last_seen: "2026-11-02T00:00:00Z",
      events: 12,
      errors: 2,
      quiet: true,
    },
  ],
};

let requested: string[] = [];
let fleetResponse: () => Response = () => Response.json(FLEET);

beforeEach(() => {
  requested = [];
  fleetResponse = () => Response.json(FLEET);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(
        input instanceof Request ? input.url : String(input),
        window.location.origin,
      );
      requested.push(url.pathname + url.search);
      if (url.pathname === "/admin/organizationHookFleet.get") {
        return fleetResponse();
      }
      return new Response("unexpected", { status: 500 });
    }),
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("HookFleetView", () => {
  it("shows the organization's installs and breakdowns", async () => {
    await renderWithApp(<HookFleetView org={anOrganization()} />);

    expect(await screen.findByText("dev-laptop")).toBeTruthy();
    expect(screen.getByText("old-laptop")).toBeTruthy();
    expect(requested).toContain(
      "/admin/organizationHookFleet.get?organization_id=org_1",
    );
    expect(screen.getByText(/Speakeasy ships 0\.3\.46/)).toBeTruthy();
    expect(screen.getAllByText("Legacy (keyed)").length).toBeGreaterThan(0);
    expect(screen.getByText("quiet")).toBeTruthy();
    expect(screen.getByText("Legacy endpoint events")).toBeTruthy();
  });

  it("counts behind installs and states the minimum and truncation", async () => {
    fleetResponse = () =>
      Response.json({
        ...FLEET,
        minimum_version: "0.3.0",
        truncated: true,
        installs: FLEET.installs.map((i) => ({
          ...i,
          version_status: "behind",
        })),
      });
    await renderWithApp(<HookFleetView org={anOrganization()} />);

    expect(await screen.findByText(/supports 0\.3\.0 and newer/)).toBeTruthy();
    expect(screen.getByText(/more installs than shown/)).toBeTruthy();
    const tile = screen.getByText("Behind or unsupported").parentElement;
    expect(tile?.textContent).toContain("2");
  });

  it("says so when the fleet cannot be read", async () => {
    fleetResponse = () => new Response("boom", { status: 500 });
    await renderWithApp(<HookFleetView org={anOrganization()} />);

    expect(await screen.findByText("Fleet unavailable")).toBeTruthy();
    expect(screen.getByText("Unable to load hooks")).toBeTruthy();
  });
});
