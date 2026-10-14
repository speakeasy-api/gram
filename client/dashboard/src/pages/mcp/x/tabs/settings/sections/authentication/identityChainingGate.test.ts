import type { InternalSessionIssuer } from "@gram/client/models/components/internalsessionissuer.js";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { describe, expect, it } from "vitest";
import { identityChainingProviders } from "./identityChainingGate";

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
      speakeasy: false,
    });
  });

  it("shows for an organization issuer without external trust when Speakeasy's issuer is on", () => {
    expect(
      identityChainingProviders(orgIssuer, {
        ...rollouts,
        speakeasyIssuer: speakeasy("enabled"),
      }),
    ).toEqual({ external: false, speakeasy: true });
  });

  it("offers both when both can vouch", () => {
    expect(
      identityChainingProviders(trusting, {
        ...rollouts,
        speakeasyIssuer: speakeasy("signing_paused"),
      }),
    ).toEqual({ external: true, speakeasy: true });
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
});
