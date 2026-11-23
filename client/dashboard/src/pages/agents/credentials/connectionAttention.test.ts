// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  assignableOwnConnection,
  displayedConnections,
  missingSignInsSentence,
  serversNeedingConnection,
} from "./connectionAttention";
import type { ServerConnection, ServerSignIn } from "./ownerConnections";
import type { OwnerConnections } from "./useOwnerConnections";
import type { InventoryServer } from "./useServerInventory";

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
        connectedNames: ["Linear"],
      }),
    ).toBe("This server has 2 Linear sign-ins; 1 is connected.");
  });

  it("does not call a connected sign-in of another name the server's", () => {
    // A hosted server needs every sign-in on its issuer: a connected Stripe
    // registration is not a second Linear one.
    expect(
      missingSignInsSentence(linear, {
        status: "missing",
        signIns: [signIn("linear_one", "Linear")],
        connected: 1,
        connectedNames: ["Stripe"],
      }),
    ).toBe(
      "This server has 2 sign-ins; 1 is connected. Still needs a connection to Linear.",
    );
  });

  it("does not hide two missing sign-ins that share the server's name", () => {
    expect(
      missingSignInsSentence(
        linear,
        missing(signIn("linear_one", "Linear"), signIn("linear_two", "Linear")),
      ),
    ).toBe("Still needs a connection to 2 Linear sign-ins.");
  });

  it("names what is left when it is not the server's own service", () => {
    expect(
      missingSignInsSentence(linear, {
        status: "missing",
        signIns: [signIn("stripe", "Stripe")],
        connected: 2,
      }),
    ).toBe(
      "This server has 3 sign-ins; 2 are connected. Still needs a connection to Stripe.",
    );
  });

  it("adds nothing when the connection is not missing a sign-in", () => {
    expect(missingSignInsSentence(linear, undefined)).toBeNull();
    expect(missingSignInsSentence(linear, { status: "missing" })).toBeNull();
    expect(
      missingSignInsSentence(linear, { status: "ready", sessions: [] }),
    ).toBeNull();
  });
});

describe("a connection the viewer may not see", () => {
  const servers = [
    { id: "grain", name: "Grain", issuerId: "issuer_grain" },
    { id: "linear", name: "Linear", issuerId: "issuer_linear" },
  ] as InventoryServer[];
  const connections = (own: ServerConnection): OwnerConnections => ({
    byServer: new Map([
      ["grain", own],
      ["linear", own],
    ]),
    assignedByServer: new Map<string, ServerConnection>([
      ["grain", { status: "forbidden" }],
      ["linear", { status: "missing" }],
    ]),
    refetch: async () => {},
  });

  it("is shown as not visible and not counted as needing a sign-in", () => {
    const displayed = displayedConnections(
      connections({ status: "missing", connected: 0 }),
    );
    expect(displayed.get("grain")).toEqual({ status: "forbidden" });
    expect(
      serversNeedingConnection(servers, displayed).map((server) => server.id),
    ).toEqual(["linear"]);
  });

  it("is not replaced by the viewer's own connection, which they could not save", () => {
    const displayed = displayedConnections(
      connections({ status: "ready", sessions: [] }),
    );
    expect(displayed.get("grain")).toEqual({ status: "forbidden" });
  });
});

describe("an existing agent with no working connection", () => {
  const servers = [
    { id: "linear", name: "Linear", issuerId: "issuer_linear" },
  ] as InventoryServer[];
  const connections = (
    own: ServerConnection,
    assigned: ServerConnection,
  ): OwnerConnections => ({
    byServer: new Map([["linear", own]]),
    assignedByServer: new Map([["linear", assigned]]),
    refetch: async () => {},
  });
  const ready: ServerConnection = { status: "ready", sessions: [] };

  it.each([
    { status: "missing" } as ServerConnection,
    { status: "ended", sessions: [] } as ServerConnection,
  ])(
    "shows the $status assignment, not the editor's connection",
    (assigned) => {
      const view = connections(ready, assigned);
      const displayed = displayedConnections(view);
      expect(displayed.get("linear")).toEqual(assigned);
      expect(
        serversNeedingConnection(servers, displayed).map((server) => server.id),
      ).toEqual(["linear"]);
      expect(assignableOwnConnection(view, "linear")).toBe(true);
    },
  );

  it("offers the editor's connection only when it works", () => {
    expect(
      assignableOwnConnection(
        connections({ status: "checking" }, { status: "missing" }),
        "linear",
      ),
    ).toBe(false);
    expect(
      assignableOwnConnection(
        connections({ status: "missing" }, { status: "missing" }),
        "linear",
      ),
    ).toBe(false);
  });

  it("offers nothing where the agent's connection works or is not visible", () => {
    expect(assignableOwnConnection(connections(ready, ready), "linear")).toBe(
      false,
    );
    expect(
      assignableOwnConnection(
        connections(ready, { status: "forbidden" }),
        "linear",
      ),
    ).toBe(false);
  });

  it("offers nothing while an agent is being created", () => {
    const creating: OwnerConnections = {
      byServer: new Map([["linear", ready]]),
      refetch: async () => {},
    };
    expect(displayedConnections(creating).get("linear")).toEqual(ready);
    expect(assignableOwnConnection(creating, "linear")).toBe(false);
  });
});
