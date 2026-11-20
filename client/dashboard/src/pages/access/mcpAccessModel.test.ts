// @vitest-environment node
import { unrecognized } from "@gram/client/types/unrecognized.js";
import { describe, expect, it } from "vitest";

import {
  convertToolLimit,
  fuzzyMatch,
  limitAfterToggle,
  toolLimitReaches,
  toggleDisposition,
  dispositionCopy,
} from "./mcpAccessModel";
import type { ServerTool } from "./serverMerge";

describe("tool limits", () => {
  const tools: ServerTool[] = [
    {
      id: "1",
      name: "search",
      type: "http",
      annotations: { readOnlyHint: true },
    },
    {
      id: "2",
      name: "delete",
      type: "http",
      annotations: { destructiveHint: true },
    },
    { id: "3", name: "plain", type: "http" },
  ];

  it("carries access across kinds where the tools allow", () => {
    expect(
      convertToolLimit(
        { kind: "annotations", dispositions: ["read_only"] },
        "tools",
        tools,
      ),
    ).toEqual({ kind: "tools", tools: ["search"] });
    expect(
      convertToolLimit(
        { kind: "tools", tools: ["delete"] },
        "annotations",
        tools,
      ),
    ).toEqual({ kind: "annotations", dispositions: ["destructive"] });
    expect(convertToolLimit({ kind: "all" }, "tools", tools)).toEqual({
      kind: "tools",
      tools: ["search", "delete", "plain"],
    });
  });
});

describe("ticking tools on a rule limited to kinds", () => {
  const readOnly = {
    kind: "annotations",
    dispositions: ["read_only"],
  } as const;

  it("keeps the kinds and names the ticked tool beside them", () => {
    // Read-only tools stay covered, including ones added later.
    expect(
      limitAfterToggle(readOnly, "delete", ["search", "delete"], 3),
    ).toEqual({
      kind: "annotations",
      dispositions: ["read_only"],
      tools: ["delete"],
    });
  });

  it("drops a named tool when it is unticked", () => {
    expect(
      limitAfterToggle(
        { ...readOnly, tools: ["delete"] },
        "delete",
        ["search"],
        3,
      ),
    ).toEqual({ kind: "annotations", dispositions: ["read_only"], tools: [] });
  });

  it("turns into a tool list when a tool a kind covers is unticked", () => {
    // A kind cannot leave one of its tools out.
    expect(limitAfterToggle(readOnly, "search", [], 3)).toEqual({
      kind: "tools",
      tools: [],
    });
  });

  it("becomes every tool when the last tool is ticked", () => {
    expect(
      limitAfterToggle(readOnly, "plain", ["search", "delete", "plain"], 3),
    ).toEqual({ kind: "all" });
  });

  it("reaches the kinds and the named tools", () => {
    const limit = { ...readOnly, tools: ["delete"] };
    const tool = (name: string, annotations?: ServerTool["annotations"]) =>
      ({ id: name, name, type: "http", annotations }) as ServerTool;
    expect(
      toolLimitReaches(limit, tool("search", { readOnlyHint: true })),
    ).toBe(true);
    expect(
      toolLimitReaches(limit, tool("delete", { destructiveHint: true })),
    ).toBe(true);
    expect(toolLimitReaches(limit, tool("plain"))).toBe(false);
  });
});

describe("fuzzyMatch", () => {
  it("matches subsequences, ignoring case and spaces", () => {
    expect(fuzzyMatch("gdrv", "Google Drive")).toBe(true);
    expect(fuzzyMatch("GOOG dr", "Google Drive")).toBe(true);
    expect(fuzzyMatch("xyz", "Google Drive")).toBe(false);
    expect(fuzzyMatch("", "anything")).toBe(true);
  });
});

describe("unknown tool annotations", () => {
  it("keeps unknown annotations when toggling a known one", () => {
    const future = unrecognized("future_annotation");
    expect(toggleDisposition([future], "read_only", true)).toEqual([
      future,
      "read_only",
    ]);
    expect(
      toggleDisposition([future, "read_only"], "read_only", false),
    ).toEqual([future]);
  });
  it.each(["future_annotation", "__proto__"])(
    "renders %s neutrally",
    (value) => {
      expect(dispositionCopy(unrecognized(value)).label).toBe(
        "Unknown annotation",
      );
    },
  );
});
