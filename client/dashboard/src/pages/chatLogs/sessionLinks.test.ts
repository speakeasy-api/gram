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
      groupSubsessions(sessions, [edge("a", "b"), edge("b", "a")])
        .map(({ session }) => session.id)
        .sort(),
    ).toEqual(["a", "b"]);
    expect(
      groupSubsessions(sessions, [edge("missing", "a")])
        .map(({ session }) => session.id)
        .sort(),
    ).toEqual(["a", "b"]);
  });
  it("keeps self loops, absent children and non-helper links flat", () => {
    const sessions = [{ id: "a" }, { id: "b" }];
    for (const link of [
      edge("a", "a"),
      edge("a", "missing"),
      { ...edge("a", "b"), kind: "move" },
    ])
      expect(groupSubsessions(sessions, [link])).toEqual(
        sessions.map((session) => ({ session, depth: 0 })),
      );
  });
  it("handles deep helper chains without recursion", () => {
    const sessions = Array.from({ length: 20000 }, (_, i) => ({
      id: String(i),
    }));
    const links = sessions
      .slice(1)
      .map((session, i) => edge(String(i), session.id));
    const grouped = groupSubsessions(sessions, links);
    expect(grouped).toHaveLength(sessions.length);
    expect(grouped.at(-1)).toEqual({
      session: sessions.at(-1),
      depth: sessions.length - 1,
    });
  });
  it("does not label helpers as moves", () => {
    expect(
      summarizeLineage([edge("parent", "child")], "parent"),
    ).toBeUndefined();
  });
});
