import { describe, expect, it } from "vitest";
import {
  buildScopeTree,
  memoryMatchesScope,
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

describe("memoryMatchesScope", () => {
  const taggedMemory = {
    contentScope: ["product:github", "topic:tool-usage"],
  } as Parameters<typeof memoryMatchesScope>[0];

  it.each<{
    selection: ScopeSelection | null;
    matches: boolean;
  }>([
    { selection: null, matches: true },
    {
      selection: { kind: "namespace", value: "product" },
      matches: true,
    },
    {
      selection: { kind: "namespace", value: "topic" },
      matches: true,
    },
    {
      selection: { kind: "tag", value: "product:github" },
      matches: true,
    },
    {
      selection: { kind: "tag", value: "product:gitlab" },
      matches: false,
    },
  ])("matches multi-tag memories for $selection", ({ selection, matches }) => {
    expect(memoryMatchesScope(taggedMemory, selection)).toBe(matches);
  });
});
