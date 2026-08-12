import { describe, expect, it } from "vitest";

import { normalizeSetupGuideMarkdown } from "./setupGuideMarkdown";

describe("normalizeSetupGuideMarkdown", () => {
  it("strips the frontmatter block and the duplicate title heading", () => {
    const result = normalizeSetupGuideMarkdown(
      "---\nsetup_version: 1\n---\n\n# Box\n\nUse a Box Admin account.\n",
    );

    expect(result).toBe("Use a Box Admin account.\n");
  });

  it("keeps a thematic break that appears later in the guide", () => {
    const result = normalizeSetupGuideMarkdown(
      "---\nsetup_version: 1\n---\n\n# Box\n\nFirst step.\n\n---\n\nSecond step.\n",
    );

    expect(result).toBe("First step.\n\n---\n\nSecond step.\n");
  });

  it("leaves a guide alone that has neither frontmatter nor a title heading", () => {
    expect(normalizeSetupGuideMarkdown("## Step one\n\nDo the thing.\n")).toBe(
      "## Step one\n\nDo the thing.\n",
    );
  });

  it("strips only the leading heading, not every H1", () => {
    const result = normalizeSetupGuideMarkdown(
      "# Box\n\nIntro.\n\n# Appendix\n",
    );

    expect(result).toBe("Intro.\n\n# Appendix\n");
  });

  it("does not mistake a hashtag in prose for the title heading", () => {
    expect(normalizeSetupGuideMarkdown("#hashtag is not a heading\n")).toBe(
      "#hashtag is not a heading\n",
    );
  });
});
