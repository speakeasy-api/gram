import fs from "node:fs";

import { describe, expect, it } from "vitest";

const indexHtml = fs.readFileSync("index.html", "utf8");

describe("theme bootstrap", () => {
  it("loads from an external script without executable inline scripts", () => {
    const scriptTags = [
      ...indexHtml.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g),
    ];
    const executableInlineBodies = scriptTags
      .filter(([, attributes]) => !/\bsrc=/.test(attributes ?? ""))
      .map(([, , body]) => body?.trim())
      .filter(Boolean);

    expect(executableInlineBodies).toEqual([]);
    expect(indexHtml).toContain(
      '<script src="/src/theme-init.ts" vite-ignore></script>',
    );
  });
});
