import { describe, expect, it } from "vitest";
import { serializeSupportMatrixUpdate } from "./request";
import type { Draft } from "./model";

function draftWithNote(note: string): Draft {
  return {
    mappings: {},
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
