import { describe, expect, it } from "vitest";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import {
  buildSpanRanges,
  byteOffsetsToIndices,
  findingByteSpans,
  layoutPayload,
  STORED_PAYLOAD_MAX_BYTES,
} from "./payload-spans";

const bytes = (s: string) => new TextEncoder().encode(s).length;

describe("byteOffsetsToIndices", () => {
  it("is the identity for ASCII", () => {
    const m = byteOffsetsToIndices("hello", [0, 2, 5]);
    expect([...m.entries()]).toEqual([
      [0, 0],
      [2, 2],
      [5, 5],
    ]);
  });

  it("accounts for 2-, 3- and 4-byte characters", () => {
    // é (2 bytes), € (3 bytes), 😀 (4 bytes, 2 UTF-16 units)
    const text = "aé€😀b";
    const offsets = [0, 1, 3, 6, 10, 11];
    const m = byteOffsetsToIndices(text, offsets);
    expect(m.get(1)).toBe(1); // before é
    expect(m.get(3)).toBe(2); // before €
    expect(m.get(6)).toBe(3); // before 😀
    expect(m.get(10)).toBe(5); // before b
    expect(m.get(11)).toBe(6); // end
  });

  it("drops offsets past the end and negative offsets", () => {
    const m = byteOffsetsToIndices("é", [2, 3, -1]);
    expect(m.get(2)).toBe(1);
    expect(m.has(3)).toBe(false);
    expect(m.has(-1)).toBe(false);
  });

  it("rounds an offset inside a multibyte character up", () => {
    expect(byteOffsetsToIndices("€x", [1]).get(1)).toBe(1);
  });
});

describe("buildSpanRanges", () => {
  it("maps byte spans after multibyte text to the right substring", () => {
    const payload = '{"note":"café ☕","key":"AKIA123"}';
    const startByte = bytes(payload.slice(0, payload.indexOf("AKIA")));
    const { ranges, complete } = buildSpanRanges(payload, [
      { id: "f1", startByte, endByte: startByte + 7 },
    ]);
    expect(complete).toBe(true);
    expect(ranges).toHaveLength(1);
    expect(payload.slice(ranges[0]!.start, ranges[0]!.end)).toBe("AKIA123");
  });

  it("skips spans outside the payload and empty spans", () => {
    const { ranges, complete } = buildSpanRanges("short", [
      { id: "a", startByte: 2, endByte: 40 },
      { id: "b", startByte: 3, endByte: 3 },
      { id: "c", startByte: 0, endByte: 2 },
    ]);
    expect(ranges).toEqual([{ start: 0, end: 2, ids: ["c"] }]);
    expect(complete).toBe(false);
  });

  it("reports spans past a payload stored at the cap as truncated", () => {
    // é straddled the cap, so the server cut one byte short of it.
    const payload = "a".repeat(STORED_PAYLOAD_MAX_BYTES - 1);
    const { ranges, complete, truncatedIds } = buildSpanRanges(payload, [
      { id: "past", startByte: 10, endByte: STORED_PAYLOAD_MAX_BYTES + 5 },
      { id: "inside", startByte: 0, endByte: 4 },
    ]);
    expect(truncatedIds).toEqual(["past"]);
    expect(complete).toBe(false);
    expect(ranges).toEqual([{ start: 0, end: 4, ids: ["inside"] }]);
  });

  it("does not report truncation for a payload under the cap", () => {
    const { complete, truncatedIds } = buildSpanRanges("short", [
      { id: "a", startByte: 2, endByte: 40 },
    ]);
    expect(complete).toBe(false);
    expect(truncatedIds).toEqual([]);
  });

  it("is incomplete when a span indexes another string", () => {
    const { ranges, complete, truncatedIds } = buildSpanRanges("tool args", [
      { id: "name", startByte: 0, endByte: 4, offPayload: true },
    ]);
    expect(ranges).toEqual([]);
    expect(complete).toBe(false);
    expect(truncatedIds).toEqual([]);
  });

  it("stays complete when only empty spans are dropped", () => {
    const { complete } = buildSpanRanges("short", [
      { id: "b", startByte: 3, endByte: 3 },
    ]);
    expect(complete).toBe(true);
  });

  it("merges overlapping spans and keeps every finding id", () => {
    const { ranges } = buildSpanRanges("0123456789", [
      { id: "a", startByte: 1, endByte: 5 },
      { id: "b", startByte: 3, endByte: 7 },
      { id: "c", startByte: 7, endByte: 9 },
    ]);
    expect(ranges).toEqual([
      { start: 1, end: 7, ids: ["a", "b"] },
      { start: 7, end: 9, ids: ["c"] },
    ]);
  });
});

describe("findingByteSpans", () => {
  const base = {
    id: "f",
    policyId: "p",
    policyVersion: 1,
    createdAt: new Date(0),
    source: "gitleaks",
  } satisfies RiskResult;

  it("prefers spans[] over start/end", () => {
    expect(
      findingByteSpans({
        ...base,
        startPos: 0,
        endPos: 9,
        spans: [{ match: "", startPos: 2, endPos: 4 }],
      }),
    ).toEqual([{ id: "f", startByte: 2, endByte: 4, offPayload: false }]);
  });

  it("marks spans outside the payload fields as off-payload", () => {
    expect(
      findingByteSpans({
        ...base,
        spans: [
          { match: "", field: "tool.args", startPos: 0, endPos: 2 },
          { match: "", field: "tool.name", startPos: 0, endPos: 4 },
          {
            match: "",
            field: "tool.args",
            path: "cmd",
            startPos: 1,
            endPos: 3,
          },
        ],
      }),
    ).toEqual([
      { id: "f", startByte: 0, endByte: 2, offPayload: false },
      { id: "f", startByte: 0, endByte: 4, offPayload: true },
      { id: "f", startByte: 1, endByte: 3, offPayload: true },
    ]);
  });

  it("falls back to start/end", () => {
    expect(findingByteSpans({ ...base, startPos: 1, endPos: 3 })).toEqual([
      { id: "f", startByte: 1, endByte: 3 },
    ]);
  });

  it("returns nothing without positions", () => {
    expect(findingByteSpans(base)).toEqual([]);
  });
});

describe("layoutPayload", () => {
  const joined = (lines: ReturnType<typeof layoutPayload>) =>
    lines.map((l) => l.map((s) => s.text).join(""));

  it("pretty-prints compact JSON with syntax kinds", () => {
    const lines = layoutPayload('{"q":"x","n":1,"e":[]}', []);
    expect(joined(lines)).toEqual([
      "{",
      '  "q": "x",',
      '  "n": 1,',
      '  "e": []',
      "}",
    ]);
    const kinds = lines[1]!.map((s) => s.kind);
    expect(kinds).toEqual(["text", "key", "punc", "text", "str", "punc"]);
  });

  it("keeps ranges on raw indices and marks where each range opens", () => {
    const payload = '{"query":"mail me@x.io now"}';
    const start = payload.indexOf("me@x.io");
    const lines = layoutPayload(payload, [
      { start, end: start + 7, ids: ["f1"] },
    ]);
    const hit = lines.flat().find((s) => s.range === 0);
    expect(hit).toMatchObject({ text: "me@x.io", rangeStart: true });
  });

  it("opens a range that starts in reflowed JSON whitespace", () => {
    const payload = '{"a": "x"}';
    const start = payload.indexOf(" ");
    const lines = layoutPayload(payload, [
      { start, end: payload.indexOf("}"), ids: ["f1"] },
    ]);
    const hits = lines.flat().filter((s) => s.range === 0);
    expect(hits[0]).toMatchObject({ text: '"x"', rangeStart: true });
    expect(hits.filter((s) => s.rangeStart)).toHaveLength(1);
  });

  it("renders non-JSON as plain lines and splits a range across them", () => {
    const payload = "first line\nsecret\nvalue";
    const start = payload.indexOf("secret");
    const lines = layoutPayload(payload, [
      { start, end: payload.length, ids: ["f1"] },
    ]);
    expect(joined(lines)).toEqual(["first line", "secret", "value"]);
    expect(lines[1]![0]).toMatchObject({ range: 0, rangeStart: true });
    expect(lines[2]![0]).toMatchObject({ range: 0, rangeStart: false });
    expect(lines[2]![0]!.kind).toBe("text");
  });
});
