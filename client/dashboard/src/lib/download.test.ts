import { describe, expect, it } from "vitest";

import { filenameFromContentDisposition, headerValue } from "./download";

describe("filenameFromContentDisposition", () => {
  it("reads quoted and bare filenames", () => {
    expect(
      filenameFromContentDisposition(
        'attachment; filename="xaa-checklist.csv"',
        "fallback.csv",
      ),
    ).toBe("xaa-checklist.csv");
    expect(
      filenameFromContentDisposition(
        "attachment; filename=xaa-checklist.md",
        "fallback.md",
      ),
    ).toBe("xaa-checklist.md");
  });

  it("prefers the RFC 5987 form and decodes it", () => {
    expect(
      filenameFromContentDisposition(
        "attachment; filename=\"plain.csv\"; filename*=UTF-8''cross%20app.csv",
        "fallback.csv",
      ),
    ).toBe("cross app.csv");
  });

  it("reads the charset and language case-insensitively", () => {
    expect(
      filenameFromContentDisposition(
        "attachment; filename*=utf-8'en'cross%20app.csv",
        "fallback.csv",
      ),
    ).toBe("cross app.csv");
  });

  it("ignores a non-UTF-8 extended filename and never reads filename* as filename", () => {
    expect(
      filenameFromContentDisposition(
        "attachment; filename*=iso-8859-1''latin.csv",
        "fallback.csv",
      ),
    ).toBe("fallback.csv");
    expect(
      filenameFromContentDisposition(
        "attachment; filename*=iso-8859-1''latin.csv; filename=\"plain.csv\"",
        "fallback.csv",
      ),
    ).toBe("plain.csv");
  });

  it("falls back when the header is missing or empty", () => {
    expect(filenameFromContentDisposition(undefined, "fallback.csv")).toBe(
      "fallback.csv",
    );
    expect(filenameFromContentDisposition(null, "fallback.csv")).toBe(
      "fallback.csv",
    );
    expect(filenameFromContentDisposition("attachment", "fallback.csv")).toBe(
      "fallback.csv",
    );
  });
});

describe("headerValue", () => {
  it("looks headers up case-insensitively and takes the first value", () => {
    const headers = { "Content-Disposition": ["a.csv", "b.csv"] };
    expect(headerValue(headers, "content-disposition")).toBe("a.csv");
    expect(headerValue(headers, "X-Missing")).toBeUndefined();
  });
});
