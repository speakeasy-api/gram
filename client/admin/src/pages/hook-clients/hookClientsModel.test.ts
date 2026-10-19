import { describe, expect, it } from "vitest";

import {
  cacheAgeLabel,
  clientSeries,
  inferenceRows,
  surfaceSeries,
  compareVersions,
  latestShares,
  sessionRows,
  versionColor,
  windowDays,
  type HookClients,
} from "./hookClientsModel";

const data: HookClients = {
  window_days: 2,
  queried_at: "2026-10-19T11:00:00Z",
  from_cache: false,
  organizations: [{ id: "org-a", name: "Acme", slug: "acme" }],
  sessions: [
    {
      organization_id: "org-a",
      day: "2026-10-18",
      client: "",
      machines: 4,
      sessions: 6,
      machines_all: 4,
    },
    {
      organization_id: "org-a",
      day: "2026-10-18",
      client: "curl",
      machines: 3,
      sessions: 4,
      machines_all: 3,
    },
    {
      organization_id: "org-a",
      day: "2026-10-18",
      client: "0.3.9",
      machines: 1,
      sessions: 2,
      machines_all: 1,
    },
    {
      organization_id: "org-a",
      day: "2026-10-19",
      client: "",
      machines: 2,
      sessions: 2,
      machines_all: 5,
    },
    {
      organization_id: "org-a",
      day: "2026-10-19",
      client: "curl",
      machines: 0,
      sessions: 0,
      machines_all: 2,
    },
    {
      organization_id: "org-a",
      day: "2026-10-19",
      client: "0.3.31",
      machines: 2,
      sessions: 2,
      machines_all: 3,
    },
    {
      organization_id: "org-b",
      day: "2026-10-19",
      client: "",
      machines: 0,
      sessions: 0,
      machines_all: 1,
    },
    {
      organization_id: "org-b",
      day: "2026-10-19",
      client: "binary",
      machines: 0,
      sessions: 0,
      machines_all: 1,
    },
  ],
  inference: [
    {
      organization_id: "org-a",
      day: "2026-10-19",
      surface: "",
      users: 3,
      events: 10,
      sessions: 4,
    },
    {
      organization_id: "org-a",
      day: "2026-10-19",
      surface: "claude-code",
      users: 2,
      events: 7,
      sessions: 3,
    },
    {
      organization_id: "org-a",
      day: "2026-10-19",
      surface: "claude-chat",
      users: 1,
      events: 3,
      sessions: 1,
    },
    {
      organization_id: "org-c",
      day: "2026-10-18",
      surface: "",
      users: 1,
      events: 20,
      sessions: 1,
    },
    {
      organization_id: "org-c",
      day: "2026-10-18",
      surface: "claude-chat",
      users: 1,
      events: 20,
      sessions: 1,
    },
  ],
};

describe("compareVersions", () => {
  it("orders numerically, not lexically", () => {
    expect(["0.3.31", "0.3.9", "0.10.0"].sort(compareVersions)).toEqual([
      "0.3.9",
      "0.3.31",
      "0.10.0",
    ]);
  });
});

describe("clientSeries", () => {
  it("puts curl first and the newest version last", () => {
    const series = clientSeries(data.sessions);
    expect(series.map((s) => s.key)).toEqual([
      "curl",
      "binary",
      "0.3.9",
      "0.3.31",
    ]);
    expect(series[0]?.color).toBe(versionColor(0));
    expect(series[3]?.color).toBe(versionColor(1));
  });
});

describe("windowDays", () => {
  it("ends on the UTC day the grid was read", () => {
    expect(windowDays("2026-10-19T00:30:00Z", 3)).toEqual([
      "2026-10-17",
      "2026-10-18",
      "2026-10-19",
    ]);
  });
});

describe("sessionRows", () => {
  it("names known orgs, sorts by sessions and marks stale days", () => {
    const rows = sessionRows(data);
    expect(rows.map((r) => r.name)).toEqual(["Acme", "org-b"]);
    expect(rows[0]?.days.get("2026-10-18")?.parts.get("curl")).toBe(3);
    expect(rows[1]?.detail).toBe("organization not found");
    expect(rows[1]?.days.get("2026-10-19")?.stale).toBe(true);
  });
});

describe("latestShares", () => {
  it("reports the newest version and curl shares on the latest day", () => {
    const shares = latestShares(data, "org-a", clientSeries(data.sessions));
    expect(shares).toEqual({
      day: "2026-10-19",
      latestVersion: "0.3.31",
      latestShare: 1,
      curlShare: 0,
      curlShareAll: 0.4,
    });
  });
});

describe("cacheAgeLabel", () => {
  it("reads in minutes, then hours", () => {
    const at = "2026-10-19T11:00:00Z";
    const base = new Date(at).getTime();
    expect(cacheAgeLabel(at, base + 20_000)).toBe("just now");
    expect(cacheAgeLabel(at, base + 12 * 60_000)).toBe("12 min ago");
    expect(cacheAgeLabel(at, base + 75 * 60_000)).toBe("1 h 15 min ago");
  });
});

describe("surfaceSeries", () => {
  it("orders apps by messages across the window", () => {
    expect(surfaceSeries(data.inference).map((s) => s.key)).toEqual([
      "claude-chat",
      "claude-code",
    ]);
  });
});

describe("inferenceRows", () => {
  it("sorts orgs by messages and labels days with users", () => {
    const rows = inferenceRows(data);
    expect(rows.map((r) => r.id)).toEqual(["org-c", "org-a"]);
    const day = rows[1]?.days.get("2026-10-19");
    expect(day?.label).toBe(3);
    expect(day?.parts.get("claude-code")).toBe(7);
    expect(day?.parts.get("claude-chat")).toBe(3);
  });
});
