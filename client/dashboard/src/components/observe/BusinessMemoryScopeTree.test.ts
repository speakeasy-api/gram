import { describe, expect, it } from "vitest";
import {
  buildScopeTree,
  scopeSelectionToFilter,
  type ScopeSelection,
} from "./businessMemoryScopes";

describe("buildScopeTree", () => {
  it("groups the complete unique tag list by namespace", () => {
    const tree = buildScopeTree([
      { scope: "topic", memoryCount: 2 },
      { scope: "product", memoryCount: 3 },
      {
        scope: "product:github",
        parentScope: "product",
        memoryCount: 2,
      },
      {
        scope: "product:gitlab",
        parentScope: "product",
        memoryCount: 1,
      },
      {
        scope: "topic:tool-usage",
        parentScope: "topic",
        memoryCount: 2,
      },
    ]);

    expect(tree).toEqual([
      {
        namespace: "product",
        memoryCount: 3,
        children: [
          { label: "github", tag: "product:github", memoryCount: 2 },
          { label: "gitlab", tag: "product:gitlab", memoryCount: 1 },
        ],
      },
      {
        namespace: "topic",
        memoryCount: 2,
        children: [
          {
            label: "tool-usage",
            tag: "topic:tool-usage",
            memoryCount: 2,
          },
        ],
      },
    ]);
  });
});

describe("scopeSelectionToFilter", () => {
  it.each<{
    selection: ScopeSelection | null;
    filter: ReturnType<typeof scopeSelectionToFilter>;
  }>([
    { selection: null, filter: {} },
    {
      selection: { kind: "namespace", value: "product" },
      filter: { contentScopeNamespace: "product" },
    },
    {
      selection: { kind: "tag", value: "product:github" },
      filter: { contentScope: "product:github" },
    },
  ])("maps $selection to the server filter", ({ selection, filter }) => {
    expect(scopeSelectionToFilter(selection)).toEqual(filter);
  });
});
