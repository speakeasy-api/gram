import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { ConnectionGate } from "./ConnectionGate";

afterEach(cleanup);

function show(
  connection: OktaIdentityProviderConnection,
  requires: "verified" | "checked",
) {
  render(
    <MemoryRouter>
      <ConnectionGate
        connection={connection}
        requires={requires}
        icon="plug"
        purpose="to continue"
      >
        <div>Tab content</div>
      </ConnectionGate>
    </MemoryRouter>,
  );
}

describe("ConnectionGate", () => {
  it.each(["verified", "checked"] as const)(
    "sends a pending connection back to setup when %s is required",
    (requires) => {
      show({ status: "pending" } as OktaIdentityProviderConnection, requires);
      expect(
        screen
          .getByRole("link", { name: "Go to Okta setup" })
          .getAttribute("href"),
      ).toBe("/?tab=enterprise-managed-auth&provider=okta&view=setup");
      expect(screen.queryByText("Tab content")).toBeNull();
    },
  );
  const degraded = {
    status: "degraded",
    missingScopes: ["okta.users.read", "okta.groups.read"],
    verificationReasons: ["missing_scope"],
  } as unknown as OktaIdentityProviderConnection;

  it("explains the missing scopes when the snapshot needs a clean verification", () => {
    show(degraded, "verified");
    expect(screen.getByText("The connection needs attention")).toBeTruthy();
    expect(
      screen.getByText(/okta.users.read, okta.groups.read/).textContent,
    ).toContain("re-verify on the Okta Setup tab");
    expect(
      screen
        .getByRole("link", { name: "Re-verify the connection" })
        .getAttribute("href"),
    ).toBe("/?tab=enterprise-managed-auth&provider=okta&view=setup#connection");
  });

  it.each(["dpop_not_bound", "read_failed:okta.apps.read"] as const)(
    "does not ask for scope grants when only %s failed",
    (reason) => {
      show(
        { ...degraded, missingScopes: [], verificationReasons: [reason] },
        "verified",
      );
      expect(
        screen.getByText(/Resolve the verification issues above in Okta/),
      ).toBeTruthy();
      expect(screen.queryByText(/Grant the missing permissions/)).toBeNull();
    },
  );

  it("lets readiness through on a degraded connection", () => {
    show(degraded, "checked");
    expect(screen.getByText("Tab content")).toBeTruthy();
  });

  it("renders the tab for a verified connection", () => {
    show({ status: "verified" } as OktaIdentityProviderConnection, "verified");
    expect(screen.getByText("Tab content")).toBeTruthy();
  });
});
