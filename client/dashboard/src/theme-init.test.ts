import fs from "node:fs";

import { describe, expect, it } from "vitest";

import {
  PREFERRED_THEME_STORAGE_KEY,
  PROJECT_FAVORITES_STORAGE_PREFIX,
} from "./lib/local-storage-keys";
import { LOGOUT_PRESERVE_WINDOW_NAME_PREFIX } from "./lib/logout-storage";

function declaredStringConst(source: string, name: string): string | undefined {
  const match = source.match(
    new RegExp(String.raw`const ${name}\s*=\s*["']([^"']+)["']`),
  );
  return match?.[1];
}

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

    expect(declaredStringConst(source, "PREFERRED_THEME_STORAGE_KEY")).toBe(
      PREFERRED_THEME_STORAGE_KEY,
    );
    expect(
      declaredStringConst(source, "PROJECT_FAVORITES_STORAGE_PREFIX"),
    ).toBe(PROJECT_FAVORITES_STORAGE_PREFIX);
    expect(
      declaredStringConst(source, "LOGOUT_PRESERVE_WINDOW_NAME_PREFIX"),
    ).toBe(LOGOUT_PRESERVE_WINDOW_NAME_PREFIX);
    expect(source).toContain("shouldRestorePreservedKey");
  });

  it("does not treat data-src as an external script source", () => {
    const html =
      '<script data-src="/example.js">window.inlineRan = true;</script>';

    expect(getExecutableInlineBodies(html)).toEqual([
      "window.inlineRan = true;",
    ]);
  });
});
