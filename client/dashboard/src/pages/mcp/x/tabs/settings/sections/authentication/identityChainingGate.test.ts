import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { describe, expect, it } from "vitest";
import {
  chainingVouchers,
  identityChainingProviders,
  trustsExternalIdp,
} from "./identityChainingGate";

const orgIssuer = { id: "usi", projectId: "" } as UserSessionIssuer;
const trusting = {
  ...orgIssuer,
  trustedRemoteSessionIssuerId: "okta",
  trustedRemoteSessionClientId: "okta-client",
} as UserSessionIssuer;
const speakeasy = (state: InternalSessionIssuer["state"]) =>
  ({
    state,
    canEnable: true,
    keys: [],
    kmsCleanupPending: 0,
  }) as InternalSessionIssuer;
const rollouts = {
  externalRollout: true,
  speakeasyRollout: true,
  speakeasyIssuer: undefined,
};

describe("identityChainingProviders", () => {
  it("shows for an issuer that trusts an identity provider sign-in client", () => {
    expect(identityChainingProviders(trusting, rollouts)).toEqual({
      external: true,
    });
  });

  it("shows for an organization issuer without external trust when Speakeasy's issuer is on", () => {
    expect(
      identityChainingProviders(orgIssuer, {
        ...rollouts,
        speakeasyIssuer: speakeasy("enabled"),
      }),
    ).toEqual({ external: false });
  });

  it("uses the identity provider when the sign-in trusts one, even with Speakeasy on", () => {
    expect(
      identityChainingProviders(trusting, {
        ...rollouts,
        speakeasyIssuer: speakeasy("signing_paused"),
      }),
    ).toEqual({ external: true });
  });

  it.each([
    ["no external trust and Speakeasy off", orgIssuer, rollouts],
    [
      "Speakeasy not set up",
      orgIssuer,
      { ...rollouts, speakeasyIssuer: speakeasy("setup_required") },
    ],
    [
      "Speakeasy not rolled out",
      orgIssuer,
      { ...rollouts, speakeasyIssuer: speakeasy("unavailable") },
    ],
    [
      "the provider order rollout off",
      orgIssuer,
      {
        ...rollouts,
        speakeasyRollout: false,
        speakeasyIssuer: speakeasy("enabled"),
      },
    ],
    [
      "the external rollout off",
      trusting,
      { ...rollouts, externalRollout: false },
    ],
    [
      "the external rollout off, even with Speakeasy on",
      trusting,
      {
        ...rollouts,
        externalRollout: false,
        speakeasyIssuer: speakeasy("enabled"),
      },
    ],
    [
      "a trusted issuer without a sign-in client",
      {
        ...orgIssuer,
        trustedRemoteSessionIssuerId: "okta",
      } as UserSessionIssuer,
      rollouts,
    ],
    [
      "a project issuer",
      { ...trusting, projectId: "project" } as UserSessionIssuer,
      { ...rollouts, speakeasyIssuer: speakeasy("enabled") },
    ],
    [
      "no issuer",
      undefined,
      { ...rollouts, speakeasyIssuer: speakeasy("enabled") },
    ],
  ])("hides with %s", (_name, issuer, options) => {
    expect(identityChainingProviders(issuer, options)).toBeUndefined();
  });

  it("lets Speakeasy vouch for a trusted issuer without a sign-in client, as the server does", () => {
    expect(
      identityChainingProviders(
        {
          ...orgIssuer,
          trustedRemoteSessionIssuerId: "okta",
        } as UserSessionIssuer,
        { ...rollouts, speakeasyIssuer: speakeasy("enabled") },
      ),
    ).toEqual({ external: false });
  });

  it("lets Speakeasy vouch when its issuer couldn't be read", () => {
    expect(
      identityChainingProviders(orgIssuer, {
        ...rollouts,
        speakeasyUnknown: true,
      }),
    ).toEqual({ external: false });
  });

  it.each(["keyless", "disabled", "signing_paused"] as const)(
    "keeps a %s Speakeasy issuer's bindings manageable",
    (state) => {
      expect(
        identityChainingProviders(orgIssuer, {
          ...rollouts,
          speakeasyIssuer: speakeasy(state),
        }),
      ).toEqual({ external: false });
    },
  );
});

describe("trustsExternalIdp", () => {
  it("counts a trusted issuer or client as external trust", () => {
    expect(trustsExternalIdp(orgIssuer)).toBe(false);
    expect(trustsExternalIdp(trusting)).toBe(true);
    expect(
      trustsExternalIdp({
        ...orgIssuer,
        trustedRemoteSessionIssuerId: "okta",
      } as UserSessionIssuer),
    ).toBe(true);
    expect(
      trustsExternalIdp({
        ...orgIssuer,
        trustedRemoteSessionClientId: "okta-client",
      } as UserSessionIssuer),
    ).toBe(true);
  });
});

describe("chainingVouchers", () => {
  const projectIssuer = {
    id: "usi",
    projectId: "project",
  } as UserSessionIssuer;

  it("applies the gate's checks to a project sign-in", () => {
    expect(
      chainingVouchers(projectIssuer, {
        ...rollouts,
        speakeasyIssuer: speakeasy("enabled"),
      }),
    ).toEqual({ external: false });
    expect(
      chainingVouchers(
        { ...projectIssuer, trustedRemoteSessionIssuerId: "okta" },
        { ...rollouts, speakeasyIssuer: speakeasy("setup_required") },
      ),
    ).toBeUndefined();
  });
});
