import { describe, expect, it } from "vitest";

import { convertToolLimit, fuzzyMatch } from "./mcpAccessModel";
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

describe("fuzzyMatch", () => {
  it("matches subsequences, ignoring case and spaces", () => {
    expect(fuzzyMatch("gdrv", "Google Drive")).toBe(true);
    expect(fuzzyMatch("GOOG dr", "Google Drive")).toBe(true);
    expect(fuzzyMatch("xyz", "Google Drive")).toBe(false);
    expect(fuzzyMatch("", "anything")).toBe(true);
  });
});
