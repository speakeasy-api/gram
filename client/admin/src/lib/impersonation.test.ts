import { describe, expect, it } from "vitest";

import { impersonationUrl } from "./impersonation";

describe("impersonationUrl", () => {
  it("carries the slug as the first path segment of `redirect`", () => {
    const href = impersonationUrl("acme-placeholder");
    expect(href).toBeDefined();

    const redirect = new URL(href!).searchParams.get("redirect");
    // Pinned from the Go side too, by org_slug_destination_test.go.
    const slug = new URL(
      redirect!,
      "https://placeholder.invalid",
    ).pathname.split("/")[1];
    expect(slug).toBe("acme-placeholder");
  });

  it("targets the login endpoint on the configured app origin", () => {
    const url = new URL(impersonationUrl("acme-placeholder")!);

    expect(url.origin).toBe("https://app.gram.test");
    expect(url.pathname).toBe("/rpc/auth.login");
  });

  it("returns undefined for a blank slug", () => {
    expect(impersonationUrl("")).toBeUndefined();
  });
});
