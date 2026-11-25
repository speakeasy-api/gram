// @vitest-environment node
import { unrecognized } from "@gram/client/types/unrecognized.js";
import { describe, expect, it } from "vitest";
import { accessLibrary, detection, scanTarget } from "./access-fixtures";
import {
  buildAccessRows,
  changedDecisions,
  countDecisions,
  effectiveDecision,
} from "./access-rows";

describe("buildAccessRows", () => {
  it("keeps a block on a tool the catalog no longer admits, and lets it be cleared", () => {
    const removed = scanTarget({
      id: "zed",
      displayName: "Zed",
      gatewayClient: {
        cimdVendorKeys: [],
        oauthClientIds: [],
        clientInfoNames: [],
      },
      access: {
        state: "blocked",
        decision: "blocked",
        enforceable: true,
        catalogStatus: "removed",
      },
    });
    const disabled = scanTarget({
      id: "goose",
      displayName: "Goose",
      access: {
        state: "allowed",
        decision: "approved",
        enforceable: true,
        catalogStatus: "documents_disabled",
      },
    });
    const rows = buildAccessRows([removed, disabled], []);
    expect(rows[0]).toMatchObject({
      stored: "blocked",
      enforceable: true,
      catalogNote:
        "Removed from the Speakeasy catalog. This block still applies.",
    });
    expect(rows[1]?.catalogNote).toBe(
      "Sign-in documents are turned off for this client. This decision still applies.",
    );
    expect(
      changedDecisions(rows, new Map([["zed", "unreviewed" as const]])),
    ).toEqual([
      {
        targetId: "zed",
        displayName: "Zed",
        decision: "unreviewed",
        rationale: undefined,
      },
    ]);
  });

  it("adds no note for a tool the catalog lists as usual", () => {
    expect(
      buildAccessRows(accessLibrary(), [])[0]?.catalogNote,
    ).toBeUndefined();
  });

  it("keeps an unknown stored decision out of editable and known decision counts", () => {
    const target = scanTarget({
      access: {
        state: "blocked",
        decision: unrecognized("future_decision"),
        enforceable: true,
      },
    });
    const rows = buildAccessRows([target], []);
    expect(rows[0]).toMatchObject({ stored: "unknown", enforceable: false });
    const staleDraft = new Map([[target.id, "approved" as const]]);
    expect(effectiveDecision(rows[0]!, staleDraft)).toBe("unknown");
    expect(countDecisions(rows, staleDraft)).toEqual({
      approved: 0,
      blocked: 0,
      unreviewed: 0,
      inventoryOnly: 0,
    });
    expect(
      changedDecisions(rows, new Map([[target.id, "approved" as const]])),
    ).toEqual([]);
  });

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
