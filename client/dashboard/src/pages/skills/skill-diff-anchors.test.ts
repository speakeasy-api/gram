import type { SkillEditSuggestionChange } from "@gram/client/models/components/skilleditsuggestionchange.js";
import { describe, expect, it } from "vitest";
import { changeAnchor } from "./skill-diff-anchors";

const current = `# Runbook

Announce the window.
Check the error budget.
Watch the canary.
Close the window.
`;

const proposed = `# Runbook

Announce the window.
Check the error budget and page the on-call.
Watch the canary.
Close the window and record the duration.
`;

function change(proposedDiff: string): SkillEditSuggestionChange {
  return {
    id: "change_a",
    suggestionId: "suggestion_a",
    proposedDiff,
    rationale: "why",
    appliesCleanly: true,
    feedbackCount: 0,
    feedbackSessionCount: 0,
    createdAt: new Date("2026-07-01T00:00:00Z"),
  } as SkillEditSuggestionChange;
}

describe("changeAnchor", () => {
  it("anchors a change to its added line in the proposed manifest", () => {
    const anchor = changeAnchor(
      change(
        "@@ -3,3 +3,3 @@\n Announce the window.\n-Check the error budget.\n+Check the error budget and page the on-call.\n Watch the canary.\n",
      ),
      current,
      proposed,
    );

    expect(anchor).toMatchObject({ side: "additions", line: 4 });
  });

  // A change is diffed against what the changes before it produce, so its own
  // hunk offsets are wrong for the rendered diff. Anchoring by text keeps a
  // later change on the right line.
  it("anchors a later change past the lines an earlier one shifted", () => {
    const anchor = changeAnchor(
      change(
        "@@ -4,3 +4,3 @@\n Watch the canary.\n-Close the window.\n+Close the window and record the duration.\n",
      ),
      current,
      proposed,
    );

    expect(anchor).toMatchObject({ side: "additions", line: 6 });
  });

  it("anchors a pure deletion to the line it removes from the current manifest", () => {
    const anchor = changeAnchor(
      change("@@ -4,3 +4,2 @@\n Announce the window.\n-Watch the canary.\n"),
      current,
      proposed,
    );

    expect(anchor).toMatchObject({ side: "deletions", line: 5 });
  });

  it("drops a change whose edited line is no longer in the manifest", () => {
    expect(
      changeAnchor(change("@@ -1,1 +1,1 @@\n+Gone.\n"), current, proposed),
    ).toBeNull();
    expect(changeAnchor(change(""), current, proposed)).toBeNull();
  });
});
