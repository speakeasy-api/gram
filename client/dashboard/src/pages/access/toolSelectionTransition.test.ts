// @vitest-environment node
import { describe, expect, it } from "vitest";

import {
  selectorsAfterAnnotationToggle,
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

describe("selectorsAfterAnnotationToggle", () => {
  const unannotatedOnServer = {
    resourceKind: "mcp" as const,
    resourceId: "server-1",
    projectId: "project-1",
    disposition: "unannotated" as const,
  };

  it("keeps an unannotated rule when another annotation is ticked", () => {
    const next = selectorsAfterAnnotationToggle(
      [readOnlyEverywhere, unannotatedOnServer],
      ["read_only", "destructive"],
    );
    expect(next).toEqual([
      readOnlyEverywhere,
      { resourceKind: "mcp", resourceId: "*", disposition: "destructive" },
      unannotatedOnServer,
    ]);
  });

  it("keeps an unannotated rule when every annotation is unticked", () => {
    expect(
      selectorsAfterAnnotationToggle(
        [readOnlyEverywhere, unannotatedOnServer],
        [],
      ),
    ).toEqual([unannotatedOnServer]);
  });

  it("drops an annotation the panel shows when it is unticked", () => {
    expect(
      selectorsAfterAnnotationToggle([readOnlyEverywhere], ["idempotent"]),
    ).toEqual([
      { resourceKind: "mcp", resourceId: "*", disposition: "idempotent" },
    ]);
  });
});
