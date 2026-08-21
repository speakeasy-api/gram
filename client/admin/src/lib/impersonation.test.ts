import { describe, expect, it } from "vitest";

import { organizationFeaturesUrl } from "./impersonation";

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
