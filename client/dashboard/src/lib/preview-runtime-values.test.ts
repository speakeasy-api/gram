import fs from "node:fs";

import { describe, expect, it } from "vitest";

import { blankPreviewRuntimeValues } from "./preview-runtime-values";

const indexHtml = fs.readFileSync("index.html", "utf8");

describe("preview runtime values", () => {
  it("leaves the placeholders in index.html for the entrypoint", () => {
    // The container entrypoint (41-preview-runtime-values.sh) substitutes
    // these at start, so the shipped HTML must still carry them.
    expect(indexHtml).toContain("${GRAM_GUTTERNOTE_SCRIPT}");
    expect(indexHtml).toContain("${GRAM_BRANCH}");
    expect(indexHtml).toContain("${GRAM_COMMIT_SHA}");
    expect(indexHtml).toContain("${GRAM_PR_NUMBER}");
  });

  it("blanks them for the dev server, which has no entrypoint", () => {
    const html = blankPreviewRuntimeValues(indexHtml);

    // ${GRAM_GUTTERNOTE_SCRIPT} sits in the body, so an unsubstituted
    // placeholder would render as visible text on every locally served page.
    expect(html).not.toContain("${GRAM_GUTTERNOTE_SCRIPT}");
    expect(html).not.toContain("${GRAM_BRANCH}");
    expect(html).not.toContain("${GRAM_COMMIT_SHA}");
    expect(html).not.toContain("${GRAM_PR_NUMBER}");
  });

  it("leaves other placeholders to their own plugin", () => {
    // ${GRAM_ADMIN_SERVER_URL} is substituted by the admin-server-url plugin.
    expect(blankPreviewRuntimeValues(indexHtml)).toContain(
      "${GRAM_ADMIN_SERVER_URL}",
    );
  });
});
