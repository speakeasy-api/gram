import type { ReactElement } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  render as renderComponent,
  screen,
} from "@testing-library/react";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { IdentityProviderTab } from "./IdentityProviderTab";

vi.mock("./OktaConnectionTab", () => ({
  OktaConnectionTab: ({
    connection,
  }: {
    connection?: OktaIdentityProviderConnection;
  }) => <div>Okta setup: {connection?.status ?? "new connection"}</div>,
}));
afterEach(cleanup);

const render = (ui: ReactElement) =>
  renderComponent(ui, { wrapper: MemoryRouter });

describe("Okta setup", () => {
  it.each(["pending", "verified", "degraded"] as const)(
    "delegates a %s connection to Okta setup",
    (status) => {
      render(
        <IdentityProviderTab
          connection={
            status ? ({ status } as OktaIdentityProviderConnection) : undefined
          }
          rolloutEnabled
        />,
      );
      expect(
        screen.getByText(`Okta setup: ${status ?? "new connection"}`),
      ).toBeTruthy();
      expect(
        screen.getByText(
          /Cross App Access is required for Enterprise Managed Auth/,
        ),
      ).toBeTruthy();
      expect(screen.getByText(/does not create or verify/)).toBeTruthy();
      expect(
        screen
          .getByRole("link", { name: "Configure Cross App Access" })
          .getAttribute("href"),
      ).toBe(
        "/?tab=enterprise-managed-auth&provider=okta&view=cross-app-access",
      );
    },
  );
  it.each([undefined, "revoked"] as const)(
    "hides access instructions before connecting (%s)",
    (status) => {
      render(
        <IdentityProviderTab
          connection={
            status ? ({ status } as OktaIdentityProviderConnection) : undefined
          }
          rolloutEnabled
        />,
      );
      expect(
        screen.getByText(`Okta setup: ${status ?? "new connection"}`),
      ).toBeTruthy();
      expect(screen.queryByText(/Cross App Access is required/)).toBeNull();
      expect(
        screen.queryByRole("link", { name: "Configure Cross App Access" }),
      ).toBeNull();
    },
  );

  it("gates new connections when rollout is disabled", () => {
    render(
      <IdentityProviderTab connection={undefined} rolloutEnabled={false} />,
    );
    expect(
      screen.getByText("Okta setup is not enabled for this organization"),
    ).toBeTruthy();
    expect(screen.queryByText(/^Okta setup:/)).toBeNull();
    expect(
      screen.queryByRole("link", { name: "Configure Cross App Access" }),
    ).toBeNull();
  });
  it("keeps an existing connection manageable when rollout is disabled", () => {
    render(
      <IdentityProviderTab
        connection={{ status: "verified" } as OktaIdentityProviderConnection}
        rolloutEnabled={false}
      />,
    );
    expect(screen.getByText("Okta setup: verified")).toBeTruthy();
    expect(
      screen.getByText(
        /Cross App Access is required for Enterprise Managed Auth/,
      ),
    ).toBeTruthy();
    expect(screen.getByText(/does not create or verify/)).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Configure Cross App Access" })
        .getAttribute("href"),
    ).toBe("/?tab=enterprise-managed-auth&provider=okta&view=cross-app-access");
  });
});
