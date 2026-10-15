import { describe, expect, it } from "vitest";

import {
  orderVouchesFor,
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

describe("who an order can vouch for", () => {
  it("covers every server with no saved order", () => {
    expect(strandedServers(null, [oktaServer, ownServer])).toEqual([]);
  });

  it("lets only Speakeasy vouch on Speakeasy's own sign-in", () => {
    expect(orderVouchesFor([speakeasy], ownServer)).toBe(true);
    expect(orderVouchesFor([okta], ownServer)).toBe(false);
  });

  it("lets only the sign-in's own identity provider vouch on a federated server", () => {
    expect(orderVouchesFor([okta], oktaServer)).toBe(true);
    expect(orderVouchesFor([speakeasy], oktaServer)).toBe(false);
    expect(
      orderVouchesFor(
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
      "Tracker and Docs sign people in through Okta, so this order leaves them with nobody to vouch. Add Okta, or go back to the default.",
    ]);
    expect(strandedNotes(strandedServers([okta], [ownServer]))).toEqual([
      "Wiki uses Speakeasy’s own sign-in, so this order leaves it with nobody to vouch. Add Speakeasy, or go back to the default.",
    ]);
  });
});
