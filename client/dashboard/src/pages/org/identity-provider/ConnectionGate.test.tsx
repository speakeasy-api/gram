import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { ConnectionGate } from "./ConnectionGate";

afterEach(cleanup);

describe("ConnectionGate", () => {
  it.each([undefined, "pending", "revoked"] as const)(
    "offers a Connection link for %s",
    (status) => {
      render(
        <MemoryRouter>
          <ConnectionGate
            connection={
              status
                ? ({ status } as OktaIdentityProviderConnection)
                : undefined
            }
            rolloutEnabled
            icon="plug"
            purpose="to continue"
          />
        </MemoryRouter>,
      );
      expect(
        screen
          .getByRole("link", { name: "Go to Okta setup" })
          .getAttribute("href"),
      ).toBe("/?tab=enterprise-managed-auth&provider=okta&view=setup");
    },
  );
  const degraded = {
    status: "degraded",
    missingScopes: ["okta.users.read", "okta.groups.read"],
    verificationReasons: ["missing_scope"],
  } as unknown as OktaIdentityProviderConnection;

  it("explains the missing scopes when the snapshot needs a clean verification", () => {
    render(
      <MemoryRouter>
        <ConnectionGate
          connection={degraded}
          rolloutEnabled
          icon="plug"
          purpose="to continue"
          requires="verified"
        />
      </MemoryRouter>,
    );
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
      render(
        <MemoryRouter>
          <ConnectionGate
            connection={{
              ...degraded,
              missingScopes: [],
              verificationReasons: [reason],
            }}
            rolloutEnabled
            icon="plug"
            purpose="to continue"
          />
        </MemoryRouter>,
      );
      expect(
        screen.getByText(/Resolve the verification issues above in Okta/),
      ).toBeTruthy();
      expect(screen.queryByText(/Grant the missing permissions/)).toBeNull();
    },
  );

  it("lets readiness through on a degraded connection", () => {
    const { container } = render(
      <MemoryRouter>
        <ConnectionGate
          connection={degraded}
          rolloutEnabled
          icon="plug"
          purpose="to continue"
          requires="checked"
        />
      </MemoryRouter>,
    );
    expect(container.textContent).toBe("");
  });

  it("does not offer setup when rollout is disabled", () => {
    render(
      <MemoryRouter>
        <ConnectionGate
          connection={undefined}
          rolloutEnabled={false}
          icon="plug"
          purpose="to continue"
        />
      </MemoryRouter>,
    );
    expect(screen.queryByRole("link")).toBeNull();
  });
});
