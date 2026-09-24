import { describe, expect, it } from "vitest";

import type { Snapshot } from "@/pages/coverage/model";

import type { CoverageCell } from "./coverageApi";
import { cellKey, indexCells, methodFootprints } from "./coverageModel";

function cell(
  capability: CoverageCell["capability"],
  surface: CoverageCell["surface"],
  status: CoverageCell["status"],
): CoverageCell {
  return { capability, surface, status, value: 0, detail: "", last_seen: "" };
}

const snapshot: Snapshot = {
  methods: [
    { id: "hooks", name: "Hooks", vendor: "Anthropic", plans: "", facts: {} },
  ],
  products: [
    {
      id: "claude-code-cli",
      name: "Claude Code · CLI",
      vendor: "Anthropic",
      family: "Claude Code",
      surface: "CLI",
    },
    {
      id: "cursor-ide",
      name: "Cursor · IDE",
      vendor: "Cursor",
      family: "Cursor",
      surface: "IDE",
    },
  ],
  capabilities: [
    { id: "session", name: "Session tracking", group: "Observability" },
  ],
  draft: {
    mappings: {
      "hooks/claude-code-cli": {
        applicability: "applicable",
        conditions: "",
        facts: { session: { status: "supported", note: "", verify: false } },
      },
      "hooks/cursor-ide": {
        applicability: "applicable",
        conditions: "",
        facts: { session: { status: "supported", note: "", verify: false } },
      },
    },
    references: {},
  },
  revision: "r1",
};

describe("coverage model", () => {
  it("folds catalog product families onto surfaces", () => {
    const [method] = methodFootprints(snapshot, new Map(), true);

    expect(method?.footprint).toContain(cellKey("session", "claude_code"));
    expect(method?.footprint).toContain(cellKey("session", "cursor"));
  });

  it("counts only unobserved claims as gaps", () => {
    const cells = indexCells([cell("session", "claude_code", "observed")]);
    const [method] = methodFootprints(snapshot, cells, true);

    expect(method?.footprint.size).toBe(2);
    expect(method?.gaps.size).toBe(1);
    expect(method?.gaps.has(cellKey("session", "cursor"))).toBe(true);
  });

  it("leaves methods unranked until coverage has loaded", () => {
    // Against an empty map every claim looks like a gap, so ranking then would
    // order by claim count rather than by what this org is missing.
    const unranked = methodFootprints(snapshot, new Map(), false);
    expect(unranked).toHaveLength(1);
    expect(unranked[0]?.gaps.size).toBe(2);
  });

  it("omits methods that claim nothing", () => {
    const empty: Snapshot = {
      ...snapshot,
      draft: { mappings: {}, references: {} },
    };
    expect(methodFootprints(empty, new Map(), true)).toHaveLength(0);
  });
});
