import { describe, expect, it } from "vitest";

import { getAdminServerUrl, replaceAdminServerUrl } from "./admin-server-url";

describe("getAdminServerUrl", () => {
  it("rejects loopback HTTP outside development", () => {
    expect(getAdminServerUrl("http://localhost:8080", false)).toBeNull();
  });
});

describe("replaceAdminServerUrl", () => {
  it("inserts dollar signs literally", () => {
    expect(
      replaceAdminServerUrl(
        '<meta content="${GRAM_ADMIN_SERVER_URL}">',
        "https://admin.example.invalid/$&-$1-$$",
      ),
    ).toBe('<meta content="https://admin.example.invalid/$&-$1-$$">');
  });
});
