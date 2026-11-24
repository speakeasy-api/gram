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
  toolDispositionAnnotations,
  type ToolLimit,
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
  const readOnly: Extract<ToolLimit, { kind: "annotations" }> = {
    kind: "annotations",
    dispositions: ["read_only"],
  };
  const tools = kindTestTools();

  it("keeps the kinds and names the ticked tool beside them", () => {
    expect(
      limitAfterToggle(readOnly, tools, "delete", ["search", "delete"]),
    ).toEqual({
      kind: "annotations",
      dispositions: ["read_only"],
      tools: ["delete"],
    });
  });

  it("drops a named tool when it is unticked", () => {
    expect(
      limitAfterToggle({ ...readOnly, tools: ["delete"] }, tools, "delete", [
        "search",
      ]),
    ).toEqual({ kind: "annotations", dispositions: ["read_only"], tools: [] });
  });

  it("turns into a tool list when a tool a kind covers is unticked", () => {
    expect(limitAfterToggle(readOnly, tools, "search", [])).toEqual({
      kind: "tools",
      tools: [],
    });
  });

  it("turns into a tool list when a named tool a kind also covers is unticked", () => {
    expect(
      limitAfterToggle({ ...readOnly, tools: ["search"] }, tools, "search", []),
    ).toEqual({ kind: "tools", tools: [] });
  });

  it("keeps the kinds when the last tool is ticked", () => {
    expect(
      limitAfterToggle({ ...readOnly, tools: ["delete"] }, tools, "plain", [
        "search",
        "delete",
        "plain",
      ]),
    ).toEqual({
      kind: "annotations",
      dispositions: ["read_only"],
      tools: ["delete", "plain"],
    });
  });

  it("becomes every tool when the last tool of a tool list is ticked", () => {
    expect(
      limitAfterToggle(
        { kind: "tools", tools: ["search", "delete"] },
        tools,
        "plain",
        ["search", "delete", "plain"],
      ),
    ).toEqual({ kind: "all" });
  });

  it("reaches the kinds and the named tools", () => {
    const limit = { ...readOnly, tools: ["delete"] };
    const reached = tools
      .filter((tool) => toolLimitReaches(limit, tool))
      .map((tool) => tool.name);
    expect(reached).toEqual(["search", "delete"]);
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

describe("toolDispositionAnnotations", () => {
  it("counts a tool under its one disposition only", () => {
    // The server classifies a read-only and idempotent tool as read-only, so
    // an idempotent rule does not cover it.
    expect(
      toolDispositionAnnotations({
        annotations: { readOnlyHint: true, idempotentHint: true },
      }),
    ).toEqual(["read_only"]);
    expect(
      toolDispositionAnnotations({
        annotations: { readOnlyHint: true, destructiveHint: true },
      }),
    ).toEqual(["destructive"]);
    expect(
      toolDispositionAnnotations({
        annotations: { idempotentHint: true, openWorldHint: true },
      }),
    ).toEqual(["idempotent"]);
  });

  it("gives an unannotated tool none of the four annotations", () => {
    expect(toolDispositionAnnotations({ annotations: {} })).toEqual([]);
    expect(toolDispositionAnnotations({})).toEqual([]);
  });
});

function kindTestTools(): ServerTool[] {
  return [
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
}
