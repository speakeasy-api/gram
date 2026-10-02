import { describe, expect, it } from "vitest";
import type { ChatSessionLink } from "@gram/client/models/components/chatsessionlink.js";
import { groupSubsessions, summarizeLineage } from "./sessionLinks";
function edge(parentChatId: string, childChatId: string): ChatSessionLink {
  return {
    parentChatId,
    childChatId,
    parentCaptured: true,
    childCaptured: true,
    kind: "subagent",
    targetHarness: "claude-tag",
    createdAt: new Date(),
  };
}
describe("subsession grouping", () => {
  it("places children and grandchildren below their parent despite activity ordering", () => {
    const sessions = [
      { id: "grandchild" },
      { id: "unrelated" },
      { id: "child" },
      { id: "parent" },
    ];
    expect(
      groupSubsessions(sessions, [
        edge("parent", "child"),
        edge("child", "grandchild"),
      ]),
    ).toEqual([
      { session: { id: "unrelated" }, depth: 0 },
      { session: { id: "parent" }, depth: 0 },
      { session: { id: "child" }, depth: 1 },
      { session: { id: "grandchild" }, depth: 2 },
    ]);
  });
  it("retains all rows under missing or cyclic evidence", () => {
    const sessions = [{ id: "a" }, { id: "b" }];
    expect(
      groupSubsessions(sessions, [edge("a", "b"), edge("b", "a")]),
    ).toHaveLength(2);
    expect(groupSubsessions(sessions, [edge("missing", "a")])).toHaveLength(2);
  });
  it("does not label helpers as moves", () => {
    expect(
      summarizeLineage([edge("parent", "child")], "parent"),
    ).toBeUndefined();
  });
});
