import { describe, expect, it } from "vitest";
import {
  argsToString,
  buildDisplayItems,
  displayItemRows,
  type DisplayItem,
  type ToolRow,
  type TranscriptRow,
} from "./transcript";

describe("argsToString", () => {
  it("omits blank argument payloads instead of rendering an empty section", () => {
    expect(argsToString("")).toBeUndefined();
    expect(argsToString("  \n")).toBeUndefined();
  });

  it("preserves non-empty strings and serializes objects", () => {
    expect(argsToString("{}")).toBe("{}");
    expect(argsToString({ query: "status" })).toBe('{\n  "query": "status"\n}');
  });
});

function toolRow(id: string, seq: number, generation = 1): ToolRow {
  return {
    kind: "tool",
    id,
    toolCall: { id, type: "function", function: { name: id, arguments: "{}" } },
    callMessage: { id: `${id}-call`, seq, role: "assistant" } as never,
    resultMessage: { id: `${id}-result`, seq: seq + 1, role: "tool" } as never,
    generation,
  };
}

function messageRow(id: string, seq: number, generation = 1): TranscriptRow {
  return {
    kind: "message",
    id,
    entryType: "assistant",
    message: {
      id,
      seq,
      role: "assistant",
      content: "hello",
      createdAt: new Date(0),
    } as never,
    generation,
  };
}

const types = (items: DisplayItem[]) => items.map((i) => i.type);
const group = (items: DisplayItem[]) =>
  items.find((i) => i.type === "toolGroup") as Extract<
    DisplayItem,
    { type: "toolGroup" }
  >;

describe("buildDisplayItems — consecutive tool grouping", () => {
  it("folds a run of adjacent tool rows into one group", () => {
    const items = buildDisplayItems({
      rows: [toolRow("a", 1), toolRow("b", 3), toolRow("c", 5)],
    });
    expect(types(items).filter((t) => t === "row")).toHaveLength(0);
    expect(group(items).rows.map((r) => r.id)).toEqual(["a", "b", "c"]);
  });

  it("leaves a lone tool row ungrouped — it is already its own card", () => {
    const items = buildDisplayItems({ rows: [toolRow("a", 1)] });
    expect(types(items)).not.toContain("toolGroup");
    expect(types(items)).toContain("row");
  });

  it("breaks a run at a non-tool row, so groups never span a turn", () => {
    const items = buildDisplayItems({
      rows: [
        toolRow("a", 1),
        toolRow("b", 3),
        messageRow("m", 5),
        toolRow("c", 7),
        toolRow("d", 9),
      ],
    });
    const groups = items.filter((i) => i.type === "toolGroup");
    expect(groups).toHaveLength(2);
    expect(displayItemRows(groups[0]!).map((r) => r.id)).toEqual(["a", "b"]);
    expect(displayItemRows(groups[1]!).map((r) => r.id)).toEqual(["c", "d"]);
  });

  it("never hides a server gap inside a group", () => {
    // A gap between two tool rows must stay visible as its own affordance
    // rather than being swallowed into a collapsed run.
    const items = buildDisplayItems({
      rows: [toolRow("a", 1), toolRow("b", 3), toolRow("c", 5)],
      gaps: new Set([3]),
    });
    expect(types(items)).toContain("serverGap");
    const gapIdx = items.findIndex((i) => i.type === "serverGap");
    const groups = items.filter((i) => i.type === "toolGroup");
    // The gap splits the run; nothing after it is folded in with what precedes.
    expect(groups.every((g) => items.indexOf(g) !== gapIdx)).toBe(true);
    for (const g of groups) {
      expect(displayItemRows(g).length).toBeGreaterThanOrEqual(2);
    }
  });
});

describe("displayItemRows", () => {
  it("exposes every row of a group, so a finding inside one is locatable", () => {
    const items = buildDisplayItems({
      rows: [toolRow("a", 1), toolRow("b", 3)],
    });
    expect(displayItemRows(group(items)).map((r) => r.id)).toEqual(["a", "b"]);
  });

  it("returns nothing for structural items", () => {
    const items = buildDisplayItems({
      rows: [messageRow("m", 1)],
      hasMoreBefore: true,
    });
    const loadMore = items.find((i) => i.type === "loadMore")!;
    expect(displayItemRows(loadMore)).toEqual([]);
  });
});
