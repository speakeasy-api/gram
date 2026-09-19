import { describe, expect, it } from "vitest";
import { filenameFromDisposition } from "./contentDisposition";

describe("filenameFromDisposition", () => {
  it("prefers the RFC 5987 filename* form and decodes it", () => {
    expect(
      filenameFromDisposition(
        `attachment; filename="fallback.json"; filename*=UTF-8''speakeasy%20oin%20manifest.json`,
      ),
    ).toBe("speakeasy oin manifest.json");
  });

  it("reads the plain quoted filename", () => {
    expect(
      filenameFromDisposition(
        'attachment; filename="speakeasy-oin-xaa-manifest-2026-09-18.md"',
      ),
    ).toBe("speakeasy-oin-xaa-manifest-2026-09-18.md");
  });

  it("falls back to the plain form on a malformed filename*", () => {
    expect(
      filenameFromDisposition(
        `attachment; filename="plain.json"; filename*=UTF-8''%E0%A4%A`,
      ),
    ).toBe("plain.json");
    expect(filenameFromDisposition(null)).toBeUndefined();
  });
});
