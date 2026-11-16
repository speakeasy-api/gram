// @vitest-environment node
import { describe, expect, it } from "vitest";
import { missingSignInsSentence } from "./connectionAttention";
import type { ServerConnection, ServerSignIn } from "./ownerConnections";

const linear = { name: "Linear" };

function missing(...signIns: ServerSignIn[]): ServerConnection {
  return { status: "missing", signIns, connected: 0 };
}

function signIn(id: string, name?: string): ServerSignIn {
  return { id, credentialOwner: "subject", name };
}

describe("missingSignInsSentence", () => {
  it("names the other services a server still needs", () => {
    expect(
      missingSignInsSentence(
        linear,
        missing(
          signIn("stripe", "Stripe"),
          signIn("planetscale", "PlanetScale"),
          signIn("vercel", "Vercel"),
        ),
      ),
    ).toBe("Still needs a connection to Stripe, PlanetScale, and Vercel.");
  });

  it("counts a sign-in it cannot name rather than dropping it", () => {
    expect(
      missingSignInsSentence(
        linear,
        missing(signIn("stripe", "Stripe"), signIn("unknown")),
      ),
    ).toBe("Still needs a connection to Stripe and 1 other sign-in.");
    expect(
      missingSignInsSentence(linear, missing(signIn("a"), signIn("b"))),
    ).toBe("Still needs 2 more sign-ins.");
  });

  it("adds nothing when only the server's own sign-in is missing", () => {
    expect(
      missingSignInsSentence(linear, missing(signIn("linear", "Linear"))),
    ).toBeNull();
  });

  it("says how many sign-ins are connected once one has succeeded", () => {
    // Two registrations named after the server, one connected: without the
    // count the row would read as if the sign-in just made had not counted.
    expect(
      missingSignInsSentence(linear, {
        status: "missing",
        signIns: [signIn("linear_two", "Linear")],
        connected: 1,
      }),
    ).toBe("1 of 2 sign-ins connected. Still needs a connection to Linear.");
  });

  it("does not hide two missing sign-ins that share the server's name", () => {
    expect(
      missingSignInsSentence(
        linear,
        missing(signIn("linear_one", "Linear"), signIn("linear_two", "Linear")),
      ),
    ).toBe("Still needs a connection to 2 Linear sign-ins.");
  });

  it("adds nothing when the connection is not missing a sign-in", () => {
    expect(missingSignInsSentence(linear, undefined)).toBeNull();
    expect(missingSignInsSentence(linear, { status: "missing" })).toBeNull();
    expect(
      missingSignInsSentence(linear, { status: "ready", sessions: [] }),
    ).toBeNull();
  });
});
