import { describe, expect, it } from "vitest";

import { removeToken, splitComposerSegments } from "./tool-mentions";

const TOOLS = {
  "team-slack__platform_slack_send_message": {},
  search_docs: {},
};
const SKILLS = ["latency-triage"];

describe("splitComposerSegments", () => {
  it("marks only the runs that resolve to a tool or a skill", () => {
    expect(
      splitComposerSegments("ask @search_docs about @nope", TOOLS, SKILLS),
    ).toEqual([
      { text: "ask ", kind: "text" },
      { text: "@search_docs", kind: "tool" },
      { text: " about @nope", kind: "text" },
    ]);
  });

  it("matches tool names containing hyphens", () => {
    const draft = "@team-slack__platform_slack_send_message";
    expect(splitComposerSegments(draft, TOOLS, SKILLS)).toEqual([
      { text: draft, kind: "tool" },
    ]);
  });

  it("marks a skill token", () => {
    expect(splitComposerSegments("/latency-triage now", TOOLS, SKILLS)).toEqual(
      [
        { text: "/latency-triage", kind: "skill" },
        { text: " now", kind: "text" },
      ],
    );
  });

  it("leaves a slash inside a URL alone", () => {
    const draft = "see https://example.com/latency-triage";
    expect(splitComposerSegments(draft, TOOLS, SKILLS)).toEqual([
      { text: draft, kind: "text" },
    ]);
  });
});

describe("removeToken", () => {
  it("drops the token and the space after it", () => {
    expect(
      removeToken("/latency-triage why is it slow", "/latency-triage"),
    ).toBe("why is it slow");
  });
});
