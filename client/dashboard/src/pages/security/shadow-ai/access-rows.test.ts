import { describe, expect, it } from "vitest";
import { accessLibrary, detection, scanTarget } from "./access-fixtures";
import {
  buildAccessRows,
  changedDecisions,
  countDecisions,
  effectiveDecision,
} from "./access-rows";

describe("buildAccessRows", () => {
  it("lists every library target, with its own decision and the detection as evidence", () => {
    const rows = buildAccessRows(
      [
        scanTarget({
          access: {
            state: "blocked",
            decision: "blocked",
            enforceable: true,
            rationale: "not approved",
          },
        }),
        ...accessLibrary().slice(1),
      ],
      [detection()],
    );

    expect(rows.map((row) => row.target.id)).toEqual([
      "cursor",
      "acme-tool",
      "ollama",
    ]);
    expect(rows[0]).toMatchObject({
      stored: "blocked",
      rationale: "not approved",
      enforceable: true,
    });
    expect(rows[0]?.detection?.userCount).toBe(4);
    expect(rows[1]).toMatchObject({
      stored: "unreviewed",
      enforceable: true,
      detection: undefined,
    });
    expect(rows[2]?.enforceable).toBe(false);
  });
});

describe("changedDecisions", () => {
  const rows = buildAccessRows(accessLibrary(), []);

  it("writes only what differs from the decision on file, for enforceable targets", () => {
    const draft = new Map([
      ["cursor", "blocked" as const],
      ["acme-tool", "unreviewed" as const],
      ["ollama", "blocked" as const],
    ]);

    expect(changedDecisions(rows, draft)).toEqual([
      {
        targetId: "cursor",
        displayName: "Cursor",
        decision: "blocked",
        rationale: undefined,
      },
    ]);
  });

  it("shows the draft over the stored decision", () => {
    const draft = new Map([["cursor", "approved" as const]]);
    expect(effectiveDecision(rows[0]!, draft)).toBe("approved");
    expect(effectiveDecision(rows[1]!, draft)).toBe("unreviewed");
  });
});

describe("countDecisions", () => {
  it("counts the effective decisions and the inventory-only targets apart", () => {
    const rows = buildAccessRows(accessLibrary(), []);
    const draft = new Map([["cursor", "blocked" as const]]);

    expect(countDecisions(rows, draft)).toEqual({
      approved: 0,
      blocked: 1,
      unreviewed: 1,
      inventoryOnly: 1,
    });
  });
});
