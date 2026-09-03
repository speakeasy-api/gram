import { describe, expect, it } from "vitest";

import { getAdminServerUrl, replaceAdminServerUrl } from "./admin-server-url";

describe("getAdminServerUrl", () => {
  it("rejects loopback HTTP outside development", () => {
    expect(getAdminServerUrl("http://localhost:8080", false)).toBeNull();
  });

  it.each([
    "https://user@admin.example.invalid",
    "https://:password@admin.example.invalid",
    "http://user@localhost:8080",
    "http://:password@localhost:8080",
  ])("rejects URL userinfo in %s", (value) => {
    expect(getAdminServerUrl(value, true)).toBeNull();
  });
});

describe("replaceAdminServerUrl", () => {
  it("escapes HTML while inserting dollar signs literally", () => {
    const value = 'https://admin.example.invalid/$&-$1-$$?label="<admin>"';
    const html = replaceAdminServerUrl(
      '<meta name="admin-url" content="${GRAM_ADMIN_SERVER_URL}">',
      value,
    );

    expect(html).toBe(
      '<meta name="admin-url" content="https://admin.example.invalid/$&amp;-$1-$$?label=&quot;&lt;admin&gt;&quot;">',
    );

    const document = new DOMParser().parseFromString(html, "text/html");
    expect(
      document.querySelector<HTMLMetaElement>('meta[name="admin-url"]')
        ?.content,
    ).toBe(value);
  });
});
