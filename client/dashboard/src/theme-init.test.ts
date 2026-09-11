import fs from "node:fs";

import { describe, expect, it } from "vitest";

import { PREFERRED_THEME_STORAGE_KEY } from "./lib/local-storage-keys";
import { LOGOUT_PRESERVE_WINDOW_NAME_PREFIX } from "./lib/logout-storage";

const indexHtml = fs.readFileSync("index.html", "utf8");
const scriptTagPattern = /<script\b([^>]*)>([\s\S]*?)<\/script>/g;
const scriptSrcAttributePattern = /(?:^|\s)src\s*=/i;

function getExecutableInlineBodies(html: string) {
  return [...html.matchAll(scriptTagPattern)]
    .filter(
      ([, attributes]) => !scriptSrcAttributePattern.test(attributes ?? ""),
    )
    .map(([, , body]) => body?.trim())
    .filter(Boolean);
}

describe("theme bootstrap", () => {
  it("loads from an external script without executable inline scripts", () => {
    expect(getExecutableInlineBodies(indexHtml)).toEqual([]);
    expect(indexHtml).toContain(
      '<script src="/src/theme-init.ts" vite-ignore></script>',
    );
  });

  it("duplicates the logout-preserve constants used by the module bundle", () => {
    const source = fs.readFileSync("src/theme-init.ts", "utf8");

    expect(source).toContain(
      `const PREFERRED_THEME_STORAGE_KEY = "${PREFERRED_THEME_STORAGE_KEY}"`,
    );
    expect(source).toContain(
      `const LOGOUT_PRESERVE_WINDOW_NAME_PREFIX = "${LOGOUT_PRESERVE_WINDOW_NAME_PREFIX}"`,
    );
  });

  it("does not treat data-src as an external script source", () => {
    const html =
      '<script data-src="/example.js">window.inlineRan = true;</script>';

    expect(getExecutableInlineBodies(html)).toEqual([
      "window.inlineRan = true;",
    ]);
  });
});
