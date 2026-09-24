import { describe, expect, it } from "vitest";
import { serializeSupportMatrixUpdate } from "./request";
import type { Draft } from "./model";

function draftWithNote(note: string): Draft {
  return {
    mappings: {},
    accounts: {},
    references: {
      device: { session: { status: "supported", note, verify: false } },
    },
  };
}

describe("support matrix request size", () => {
  it("accepts exactly the server limit including the revision and envelope", () => {
    const revision = "revision-1";
    const overhead = new TextEncoder().encode(
      serializeSupportMatrixUpdate(revision, draftWithNote("")),
    ).length;
    const draft = draftWithNote("a".repeat(1024 * 1024 - overhead));
    expect(
      new TextEncoder().encode(serializeSupportMatrixUpdate(revision, draft))
        .length,
    ).toBe(1024 * 1024);
    expect(() => serializeSupportMatrixUpdate(revision + "a", draft)).toThrow(
      "1 MB save limit",
    );
  });

  it("rejects NUL characters in reference notes, coverage notes, and conditions", () => {
    expect(() =>
      serializeSupportMatrixUpdate(
        "revision-1",
        draftWithNote("before\0after"),
      ),
    ).toThrow("NUL");
    const draft: Draft = {
      references: {},
      accounts: {},
      mappings: {
        "device/platform": {
          applicability: "applicable",
          conditions: "",
          accounts: {},
          facts: {
            session: { status: "supported", note: "\0", verify: false },
          },
        },
      },
    };
    expect(() => serializeSupportMatrixUpdate("revision-1", draft)).toThrow(
      "NUL",
    );
    draft.mappings["device/platform"]!.facts = {};
    draft.mappings["device/platform"]!.conditions = "\0";
    expect(() => serializeSupportMatrixUpdate("revision-1", draft)).toThrow(
      "NUL",
    );
  });

  it.each(["é", "\u0001"])(
    "counts UTF-8 and JSON escaping for %j",
    (character) => {
      const draft = draftWithNote(character.repeat(600000));
      expect(() => serializeSupportMatrixUpdate("revision-1", draft)).toThrow(
        "1 MB save limit",
      );
    },
  );
});
