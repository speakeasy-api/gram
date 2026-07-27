import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import SkillManifestSource from "./SkillManifestSource";
import type { SkillDiffAnchor } from "./skill-diff-anchors";

const manifest = `---
name: incident-review
---

Write a blameless narrative.
Produce a five-whys section.
List action items.
`;

function renderSource(anchors: SkillDiffAnchor[]) {
  return render(
    <SkillManifestSource
      content={manifest}
      anchors={anchors}
      renderGutter={() => <span>marker</span>}
      renderAnchor={() => <span>comment</span>}
    />,
  );
}

describe("SkillManifestSource", () => {
  afterEach(cleanup);

  it("numbers every manifest line including frontmatter", () => {
    renderSource([]);

    expect(screen.getByText("1")).toBeTruthy();
    expect(screen.getByText("name: incident-review")).toBeTruthy();
    expect(screen.getByText("7")).toBeTruthy();
    expect(screen.queryByText("marker")).toBeNull();
  });

  it("shows an insertion in place, after the line it follows", () => {
    renderSource([
      {
        line: 5,
        hunks: [{ anchorLine: 5, removed: [], added: ["Quantify impact."] }],
      },
    ]);

    expect(screen.getByText("Quantify impact.")).toBeTruthy();
    expect(screen.getByText("Write a blameless narrative.")).toBeTruthy();
    expect(screen.getByText("marker")).toBeTruthy();
    expect(screen.getByText("comment")).toBeTruthy();
  });

  it("strikes the replaced line and shows its replacement after it", () => {
    renderSource([
      {
        line: 6,
        hunks: [
          {
            anchorLine: 6,
            removed: ["Produce a five-whys section."],
            added: ["Produce a five-whys root-cause section."],
          },
        ],
      },
    ]);

    const removedRow = screen.getByText(
      "Produce a five-whys section.",
    ).parentElement;
    expect(removedRow?.className).toContain("bg-destructive/10");
    expect(
      screen.getByText("Produce a five-whys root-cause section."),
    ).toBeTruthy();
  });
});
