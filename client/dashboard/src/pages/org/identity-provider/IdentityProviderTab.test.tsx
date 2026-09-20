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
    },
  );
  it.each([undefined, "revoked"] as const)(
    "delegates to Okta setup before connecting (%s)",
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
  });
  it("keeps an existing connection manageable when rollout is disabled", () => {
    render(
      <IdentityProviderTab
        connection={{ status: "verified" } as OktaIdentityProviderConnection}
        rolloutEnabled={false}
      />,
    );
    expect(screen.getByText("Okta setup: verified")).toBeTruthy();
  });
});
