import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

describe("dashboard document", () => {
  it("disables browser translation before React mounts", () => {
    const html = readFileSync(
      path.resolve(import.meta.dirname, "../index.html"),
      "utf8",
    );
    const document = new DOMParser().parseFromString(html, "text/html");

    expect(document.documentElement.getAttribute("translate")).toBe("no");
  });
});
