import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { IdentityProviderCapabilityRead } from "@gram/client/models/components/identityprovidercapabilityread.js";
import { IdentityProviderCapabilities } from "./identity-provider-capabilities";

function read(
  overrides: Partial<IdentityProviderCapabilityRead> = {},
): IdentityProviderCapabilityRead {
  return {
    capability: "directory_read",
    resource: "groups",
    ok: true,
    ...overrides,
  };
}

afterEach(cleanup);

describe("IdentityProviderCapabilities", () => {
  it("names every capability the server can report", () => {
    render(
      <IdentityProviderCapabilities
        reads={[
          read({
            capability: "claims_provisioning",
            resource: "authorization_servers",
          }),
          read({ capability: "group_assignment", resource: "apps" }),
        ]}
      />,
    );

    // A capability with no copy falls through to its raw key and two dashes,
    // which reads as a bug rather than as information.
    expect(screen.queryByText("claims_provisioning")).toBeNull();
    expect(screen.getByText("Sign-in claims")).toBeTruthy();
    expect(screen.getByText("Manage")).toBeTruthy();
    expect(screen.getByText("Single sign-on")).toBeTruthy();

    expect(screen.queryByText("group_assignment")).toBeNull();
    expect(screen.getByText("Group assignments")).toBeTruthy();
    expect(screen.getByText("Directory sync")).toBeTruthy();
  });

  it("shows a capability it has no words for rather than hiding the read", () => {
    render(
      <IdentityProviderCapabilities
        reads={[read({ capability: "future_capability", resource: "users" })]}
      />,
    );

    // Okta grows capabilities faster than this table does; an unknown one
    // still has to say that the read happened.
    expect(screen.getByText("future_capability")).toBeTruthy();
    expect(screen.getAllByText("—")).toHaveLength(2);
  });
});
