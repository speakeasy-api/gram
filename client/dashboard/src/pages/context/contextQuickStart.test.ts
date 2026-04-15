import { describe, expect, it } from "vitest";

import { buildCorpusQuickStart } from "./contextQuickStart";

describe("buildCorpusQuickStart", () => {
  it("builds an actionable branch-agnostic quick start", () => {
    expect(
      buildCorpusQuickStart(
        "https://localhost:5173/v1/corpus/git/project-123",
        "default",
      ),
    ).toBe(`git clone https://localhost:5173/v1/corpus/git/project-123 default
cd default

# make your changes
git add .
git commit -m "Update context"
git push origin HEAD`);
  });

  it("falls back to the default directory name", () => {
    expect(
      buildCorpusQuickStart(
        "https://localhost:5173/v1/corpus/git/project-123",
        null,
      ),
    ).toContain("cd context-repo");
  });
});
