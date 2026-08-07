import { describe, expect, it } from "vitest";

import {
  selectorsAfterToolBatch,
  selectorsAfterToolToggle,
} from "./toolSelectionTransition";

const readOnlyEverywhere = {
  resourceKind: "mcp" as const,
  resourceId: "*",
  disposition: "read_only" as const,
};

describe("selectorsAfterToolToggle", () => {
  it("drops disposition selectors when the first tool is picked", () => {
    const next = selectorsAfterToolToggle(
      [readOnlyEverywhere],
      "server-1",
      "get_issue",
    );
    expect(next).toEqual([
      { resourceKind: "mcp", resourceId: "server-1", tool: "get_issue" },
    ]);
  });

  it("toggles an existing tool selector off without resurrecting dispositions", () => {
    const next = selectorsAfterToolToggle(
      [
        readOnlyEverywhere,
        { resourceKind: "mcp", resourceId: "server-1", tool: "get_issue" },
      ],
      "server-1",
      "get_issue",
    );
    expect(next).toEqual([]);
  });
});

describe("selectorsAfterToolBatch", () => {
  it("selecting a batch replaces disposition selectors with tool selectors", () => {
    const next = selectorsAfterToolBatch(
      [readOnlyEverywhere],
      "server-1",
      ["a", "b"],
      true,
    );
    expect(next).toEqual([
      { resourceKind: "mcp", resourceId: "server-1", tool: "a" },
      { resourceKind: "mcp", resourceId: "server-1", tool: "b" },
    ]);
  });

  it("deselecting a batch also drops disposition selectors", () => {
    const next = selectorsAfterToolBatch(
      [
        readOnlyEverywhere,
        { resourceKind: "mcp", resourceId: "server-1", tool: "a" },
        { resourceKind: "mcp", resourceId: "server-1", tool: "keep" },
      ],
      "server-1",
      ["a"],
      false,
    );
    expect(next).toEqual([
      { resourceKind: "mcp", resourceId: "server-1", tool: "keep" },
    ]);
  });
});
