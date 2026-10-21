import { describe, expect, it } from "vitest";

import {
  providerFixLink,
  providerView,
  vouchedFor,
  strandedNotes,
  strandedServers,
  type ServerSignIn,
} from "./emaServiceView";

type Server = ServerSignIn & { name: string };

const okta = { kind: "external" as const, remoteSessionIssuerId: "okta" };
const speakeasy = { kind: "speakeasy" as const };
const oktaServer: Server = {
  name: "Tracker",
  signInIssuerId: "okta",
  signInIssuerName: "Okta",
};
const ownServer: Server = { name: "Wiki" };

describe("who the enabled providers can vouch for", () => {
  it("covers every server when nothing is saved", () => {
    expect(strandedServers(null, [oktaServer, ownServer])).toEqual([]);
  });

  it("lets only Speakeasy vouch on Speakeasy's own sign-in", () => {
    expect(vouchedFor([speakeasy], ownServer)).toBe(true);
    expect(vouchedFor([okta], ownServer)).toBe(false);
  });

  it("lets only the sign-in's own identity provider vouch on a federated server", () => {
    expect(vouchedFor([okta], oktaServer)).toBe(true);
    expect(vouchedFor([speakeasy], oktaServer)).toBe(false);
    expect(
      vouchedFor(
        [{ kind: "external", remoteSessionIssuerId: "entra" }],
        oktaServer,
      ),
    ).toBe(false);
  });

  it("says once per sign-in which servers are left out and how to fix it", () => {
    const another = { ...oktaServer, name: "Docs" };
    expect(
      strandedNotes(
        strandedServers([speakeasy], [oktaServer, another, ownServer]),
      ),
    ).toEqual([
      "Tracker and Docs can’t use Enterprise Managed Authorization, because they use Okta sign-in. Enable Okta, or go back to the default.",
    ]);
    expect(strandedNotes(strandedServers([okta], [ownServer]))).toEqual([
      "Wiki can’t use Enterprise Managed Authorization, because it uses Speakeasy sign-in. Enable Speakeasy, or go back to the default.",
    ]);
    expect(
      strandedNotes(strandedServers([speakeasy], [oktaServer]), {
        draft: true,
      }),
    ).toEqual([
      "Tracker would lose Enterprise Managed Authorization, because it uses Okta sign-in.",
    ]);
  });
});

describe("providerView", () => {
  it("sends a Speakeasy that needs a verified domain to the Single sign-on tab", () => {
    const view = providerView(
      { kind: "speakeasy", unavailableReason: "verified_domain_required" },
      { issuer: undefined, speakeasyIssuerUrl: undefined },
    );
    expect(view.usable).toBe(false);
    expect(view.unavailable).toBe("Needs a verified domain");
    expect(view.fix && providerFixLink(view.fix)).toEqual({
      href: "?tab=sso",
      label: "Verify a domain on the Single sign-on tab",
    });
  });
});

describe("unsupported providers", () => {
  it.each([
    { kind: "future_provider" },
    { kind: "future_provider", unavailableReason: "not_trusted_by_endpoint" },
    { kind: "external", unavailableReason: "future_reason" },
    { kind: "speakeasy", unavailableReason: "future_reason" },
  ])("keeps availability unknown without inventing a fix: %j", (unknown) => {
    const provider = providerView(
      unknown as Parameters<typeof providerView>[0],
      {
        issuer: undefined,
        speakeasyIssuerUrl: "https://example.com/issuer",
      },
    );
    expect(provider.usable).toBe(false);
    expect(provider.placeholder).toBe(false);
    expect(provider.unavailable).toContain("Unknown availability");
    expect(provider.fix).toBeUndefined();
    expect(provider.issuerUrl).toBeUndefined();
    expect(provider.unusedBySignIn).toBe(false);
    if (unknown.kind === "future_provider") {
      expect(provider.name).toBe("Unknown identity provider");
      expect(provider.key).not.toBe("external-default");
      expect(provider.key).not.toBe("speakeasy");
    }
  });
});
