import { describe, expect, it } from "vitest";
import {
  draftToSchedule,
  draftToScope,
  nextScheduleBoundaryDelay,
  scopeLabel,
  serverDiff,
  unicodeLength,
  validateDraft,
  type EditorDraft,
} from "./killswitch-view-model";

const validDraft = (): EditorDraft => ({
  userId: "user-1",
  capabilityKey: "mcp_tool_calls",
  scopeType: "selected_servers",
  serverIds: ["server-b", "server-a", "server-a"],
  startType: "now",
  startsAt: "",
  endType: "until_lifted",
  endsAt: "",
  externalNote: "  Access paused.\nContact support.  ",
  internalNote: "  Incident response  ",
});

describe("killswitch view model", () => {
  it("requires principal, capability, an explicit scope, and both notes", () => {
    const errors = validateDraft({
      ...validDraft(),
      userId: "",
      capabilityKey: "",
      scopeType: "",
      externalNote: "  ",
      internalNote: "",
    });
    expect(errors).toMatchObject({
      userId: expect.any(String),
      capabilityKey: expect.any(String),
      scopeType: expect.any(String),
      externalNote: expect.any(String),
      internalNote: expect.any(String),
    });
  });

  it("requires one server for selected scope and canonicalizes the set", () => {
    expect(
      validateDraft({ ...validDraft(), serverIds: [] }).serverIds,
    ).toBeDefined();
    expect(draftToScope(validDraft())).toEqual({
      type: "selected_servers",
      serverIds: ["server-a", "server-b"],
    });
    expect(draftToScope({ ...validDraft(), scopeType: "all_servers" })).toEqual(
      {
        type: "all_servers",
      },
    );
  });

  it("validates future starts and an end after the effective start", () => {
    const now = new Date(2030, 0, 1, 12);
    const errors = validateDraft(
      {
        ...validDraft(),
        startType: "scheduled",
        startsAt: "2030-01-01T11:00",
        endType: "bounded",
        endsAt: "2030-01-01T10:00",
      },
      now,
    );
    expect(errors.startsAt).toContain("future");
    expect(errors.endsAt).toContain("after");
  });

  it("converts local datetime inputs to Date values for UTC API serialization", () => {
    const schedule = draftToSchedule({
      ...validDraft(),
      startType: "scheduled",
      startsAt: "2030-06-10T09:30",
      endType: "bounded",
      endsAt: "2030-06-11T09:30",
    });
    expect(schedule.start).toBe("scheduled");
    expect(schedule.end).toBe("bounded");
    if (schedule.start === "scheduled" && schedule.end === "bounded") {
      expect(schedule.startsAt).toBeInstanceOf(Date);
      expect(schedule.endsAt.getTime()).toBeGreaterThan(
        schedule.startsAt.getTime(),
      );
    }
  });

  it.each([
    ["all_servers", "now", "until_lifted"],
    ["all_servers", "now", "bounded"],
    ["all_servers", "scheduled", "until_lifted"],
    ["all_servers", "scheduled", "bounded"],
    ["selected_servers", "now", "until_lifted"],
    ["selected_servers", "now", "bounded"],
    ["selected_servers", "scheduled", "until_lifted"],
    ["selected_servers", "scheduled", "bounded"],
  ] as const)(
    "builds the %s × %s/%s API payload",
    (scopeType, startType, endType) => {
      const draft: EditorDraft = {
        ...validDraft(),
        scopeType,
        startType,
        startsAt: "2030-06-10T09:30",
        endType,
        endsAt: "2030-06-11T09:30",
      };
      const scope = draftToScope(draft);
      const schedule = draftToSchedule(draft);
      expect(scope.type).toBe(scopeType);
      expect(schedule.start).toBe(startType);
      expect(schedule.end).toBe(endType);
      if (scope.type === "selected_servers") {
        expect(scope.serverIds).toEqual(["server-a", "server-b"]);
      }
      if (schedule.start === "scheduled") {
        expect(schedule.startsAt).toBeInstanceOf(Date);
      }
      if (schedule.end === "bounded") {
        expect(schedule.endsAt).toBeInstanceOf(Date);
      }
    },
  );

  it("schedules an API refresh just after the nearest status boundary", () => {
    const now = new Date("2030-01-01T00:00:00Z").getTime();
    expect(
      nextScheduleBoundaryDelay(
        [
          {
            start: "scheduled",
            startsAt: new Date(now + 2_000),
            end: "bounded",
            endsAt: new Date(now + 4_000),
          },
          { start: "now", end: "bounded", endsAt: new Date(now + 3_000) },
        ],
        now,
      ),
    ).toBe(2_100);
  });

  it("mirrors server note control, trim-set, and Unicode validation", () => {
    expect(unicodeLength("🙂")).toBe(1);
    for (const control of ["\u0000", "\u001f", "\u007f", "\u0085", "\u009f"]) {
      expect(
        validateDraft({ ...validDraft(), externalNote: `${control} note ` })
          .externalNote,
      ).toContain("control");
    }
    expect(
      validateDraft({ ...validDraft(), externalNote: "a\tb\nc\rd" })
        .externalNote,
    ).toBeUndefined();
    expect(
      validateDraft({ ...validDraft(), externalNote: "\u200bnote\u200b" })
        .externalNote,
    ).toBeUndefined();
    expect(
      validateDraft({ ...validDraft(), externalNote: "\u00a0\u3000" })
        .externalNote,
    ).toContain("required");
    expect(
      validateDraft({ ...validDraft(), externalNote: "🙂".repeat(501) })
        .externalNote,
    ).toContain("500");
  });

  it("counts transmitted leading and trailing whitespace at note limits", () => {
    const atLimits = validateDraft({
      ...validDraft(),
      externalNote: ` ${"e".repeat(498)} `,
      internalNote: ` ${"i".repeat(3998)} `,
    });
    expect(atLimits.externalNote).toBeUndefined();
    expect(atLimits.internalNote).toBeUndefined();

    const overLimits = validateDraft({
      ...validDraft(),
      externalNote: ` ${"e".repeat(499)} `,
      internalNote: ` ${"i".repeat(3999)} `,
    });
    expect(overLimits.externalNote).toContain("500");
    expect(overLimits.internalNote).toContain("4000");
  });

  it("preserves deleted-server fallbacks and complete selected-resource diffs", () => {
    expect(
      scopeLabel({ type: "selected_servers", serverIds: ["gone"] }, new Map()),
    ).toBe("Deleted MCP server");
    expect(
      scopeLabel(
        { type: "selected_servers", serverIds: ["a", "gone", "c"] },
        new Map([["a", "Server A"]]),
      ),
    ).toBe("Server A, Deleted MCP server +1");
    expect(
      serverDiff(
        { type: "selected_servers", serverIds: ["a", "b", "c"] },
        { type: "selected_servers", serverIds: ["a", "d"] },
      ),
    ).toEqual({ added: ["d"], unchanged: ["a"], removed: ["b", "c"] });
    expect(
      serverDiff(
        { type: "all_servers" },
        { type: "selected_servers", serverIds: ["a"] },
      ),
    ).toBeNull();
    expect(
      serverDiff(
        { type: "all_servers" },
        { type: "selected_servers", serverIds: ["a"] },
        ["a", "b"],
      ),
    ).toEqual({ added: [], unchanged: ["a"], removed: ["b"] });
  });
});
