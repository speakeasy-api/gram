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

  it("marks a reference that follows punctuation", () => {
    expect(splitComposerSegments("ask (@search_docs)", TOOLS, SKILLS)).toEqual([
      { text: "ask (", kind: "text" },
      { text: "@search_docs", kind: "tool" },
      { text: ")", kind: "text" },
    ]);
  });

  it("leaves an email address alone", () => {
    const draft = "mail someone@search_docs about it";
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

describe("splitComposerSegments and URLs", () => {
  it("leaves a reference-shaped URL query value alone", () => {
    const draft = "see https://example.com/docs?next=/latency-triage now";
    expect(splitComposerSegments(draft, TOOLS, SKILLS)).toEqual([
      { text: draft, kind: "text" },
    ]);
  });

  it("still marks a reference after ordinary punctuation", () => {
    expect(splitComposerSegments("(@search_docs)", TOOLS, SKILLS)).toEqual([
      { text: "(", kind: "text" },
      { text: "@search_docs", kind: "tool" },
      { text: ")", kind: "text" },
    ]);
  });
});

describe("splitComposerSegments and other URI forms", () => {
  it("ignores a reference inside a protocol-relative URL", () => {
    const draft = "//cdn.example.com/x?to=/latency-triage";
    expect(splitComposerSegments(draft, TOOLS, SKILLS)).toEqual([
      { text: draft, kind: "text" },
    ]);
  });

  it("ignores a reference inside a non-hierarchical URI", () => {
    const draft = "mailto:someone@example.test?body=/latency-triage";
    expect(splitComposerSegments(draft, TOOLS, SKILLS)).toEqual([
      { text: draft, kind: "text" },
    ]);
  });
});
