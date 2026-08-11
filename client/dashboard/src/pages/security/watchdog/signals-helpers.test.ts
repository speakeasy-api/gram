import { describe, expect, it } from "vitest";
import type { RiskSignal } from "@gram/client/models/components/risksignal.js";
import {
  filterSignalsByCategory,
  filterSignalsBySeverity,
  groupSignals,
  toggleFilterValue,
  trendPercent,
} from "./signals-helpers";

function signal(overrides: Partial<RiskSignal>): RiskSignal {
  return {
    key: "rule:secret.github_pat",
    ruleId: "secret.github_pat",
    category: "secrets",
    description: "",
    detectionSources: [],
    apps: [],
    severity: "high",
    riskScore: 7.5,
    findings: 10,
    previousFindings: 5,
    users: 2,
    teams: 0,
    firstSeen: new Date("2026-08-01T00:00:00Z"),
    lastSeen: new Date("2026-08-02T00:00:00Z"),
    topUsers: [],
    sparkline: [],
    ...overrides,
  };
}

describe("trendPercent", () => {
  it("computes window-over-window growth", () => {
    expect(trendPercent(3, 1)).toBeCloseTo(200);
    expect(trendPercent(1, 4)).toBeCloseTo(-75);
    expect(trendPercent(5, 5)).toBeCloseTo(0);
  });

  it("returns null without a previous baseline", () => {
    expect(trendPercent(10, 0)).toBeNull();
  });
});

describe("groupSignals", () => {
  const signals = [
    signal({ key: "a", severity: "critical", category: "secrets" }),
    signal({ key: "b", severity: "medium", category: "pii" }),
    signal({ key: "c", severity: "critical", category: "pii" }),
  ];

  it("returns no groups for an empty list", () => {
    expect(groupSignals([], "severity")).toEqual([]);
    expect(groupSignals([], "category")).toEqual([]);
  });

  it("sections by severity in band order without reordering rows", () => {
    const groups = groupSignals(signals, "severity");
    expect(groups.map((g) => g.key)).toEqual(["critical", "medium"]);
    expect(groups[0]!.signals.map((s) => s.key)).toEqual(["a", "c"]);
  });

  it("sections by category in first-appearance order", () => {
    const groups = groupSignals(signals, "category");
    expect(groups.map((g) => g.key)).toEqual(["secrets", "pii"]);
    expect(groups[1]!.signals.map((s) => s.key)).toEqual(["b", "c"]);
  });

  it("sections by dominant team with unattributed last", () => {
    const teamSignals = [
      signal({
        key: "a",
        topUsers: [
          {
            email: "x@a.com",
            externalUserId: "x@a.com",
            team: "",
            findings: 5,
          },
          {
            email: "y@a.com",
            externalUserId: "y@a.com",
            team: "Platform",
            findings: 2,
          },
        ],
      }),
      signal({ key: "b", topUsers: [] }),
      signal({
        key: "c",
        topUsers: [
          {
            email: "z@a.com",
            externalUserId: "z@a.com",
            team: "Sales",
            findings: 1,
          },
        ],
      }),
    ];
    const groups = groupSignals(teamSignals, "team");
    expect(groups.map((g) => g.key)).toEqual(["Platform", "Sales", ""]);
    expect(groups[2]!.signals.map((s) => s.key)).toEqual(["b"]);
  });

  it("sections by first observed app with unattributed last", () => {
    const appSignals = [
      signal({ key: "a", apps: ["codex", "cursor"] }),
      signal({ key: "b", apps: [] }),
      signal({ key: "c", apps: ["cursor"] }),
    ];
    const groups = groupSignals(appSignals, "app");
    expect(groups.map((g) => g.key)).toEqual(["codex", "cursor", ""]);
  });
});

describe("filterSignalsBySeverity", () => {
  const signals = [
    signal({ key: "a", severity: "critical" }),
    signal({ key: "b", severity: "low" }),
  ];

  it("passes everything through with no selection", () => {
    expect(filterSignalsBySeverity(signals, [])).toHaveLength(2);
  });

  it("keeps only the selected severities", () => {
    const filtered = filterSignalsBySeverity(signals, ["critical"]);
    expect(filtered.map((s) => s.key)).toEqual(["a"]);
  });
});

describe("filterSignalsByCategory", () => {
  const signals = [
    signal({ key: "a", category: "secrets" }),
    signal({ key: "b", category: "pii" }),
  ];

  it("passes everything through with no selection", () => {
    expect(filterSignalsByCategory(signals, [])).toHaveLength(2);
  });

  it("keeps only the selected categories", () => {
    const filtered = filterSignalsByCategory(signals, ["pii"]);
    expect(filtered.map((s) => s.key)).toEqual(["b"]);
  });
});

describe("toggleFilterValue", () => {
  it("adds a missing value and removes a present one", () => {
    expect(toggleFilterValue([], "secrets")).toEqual(["secrets"]);
    expect(toggleFilterValue(["secrets", "pii"], "secrets")).toEqual(["pii"]);
  });
});
