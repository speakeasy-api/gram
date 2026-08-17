import { describe, expect, it } from "vitest";

import { impersonationUrl, organizationFeaturesUrl } from "./impersonation";

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

  it("keeps the redirect on the app's own origin whatever the slug holds", () => {
    // Nothing on the server side was found to rule this slug out. Unencoded it
    // makes the redirect `//host`, which is protocol-relative: resolved against
    // the app it is another origin, and the operator leaves Gram.
    const redirect = new URL(
      impersonationUrl("/evil.invalid")!,
    ).searchParams.get("redirect");

    expect(new URL(redirect!, "https://app.gram.test").origin).toBe(
      "https://app.gram.test",
    );
    expect(redirect).toBe("/%2Fevil.invalid");
  });

  it("lands on the organization itself and goes no further", () => {
    // The features row shares this builder. A destination leaking into the
    // plain link would drop the operator on a page nobody asked for.
    const redirect = new URL(
      impersonationUrl("acme-placeholder")!,
    ).searchParams.get("redirect");
    expect(redirect).toBe("/acme-placeholder");
  });
});

describe("organizationFeaturesUrl", () => {
  it("carries the slug and the dashboard's features address in `redirect`", () => {
    const redirect = new URL(
      organizationFeaturesUrl("acme-placeholder")!,
    ).searchParams.get("redirect");
    // The whole destination. The server reads only the first segment back as
    // the organization and the dashboard routes on the rest, so both halves
    // have to be right for the operator to land on this record's features.
    expect(redirect).toBe("/acme-placeholder/platform-admin/features");
  });

  it("targets the login endpoint on the configured app origin", () => {
    const url = new URL(organizationFeaturesUrl("acme-placeholder")!);

    expect(url.origin).toBe("https://app.gram.test");
    expect(url.pathname).toBe("/rpc/auth.login");
  });

  it("returns undefined for a blank slug", () => {
    expect(organizationFeaturesUrl("")).toBeUndefined();
  });
});
