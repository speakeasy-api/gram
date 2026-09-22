import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";

import { OktaConnectionTab } from "./OktaConnectionTab";

vi.mock("@gram/client/react-query/createIdentityProviderConnection.js", () => ({
  useCreateIdentityProviderConnectionMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
    error: null,
  }),
}));

afterEach(cleanup);

describe("OktaConnectionTab", () => {
  it.each([undefined, "revoked"] as const)(
    "shows only the organization form for %s",
    (status) => {
      render(
        <MemoryRouter>
          <QueryClientProvider client={new QueryClient()}>
            <OktaConnectionTab
              connection={
                status
                  ? ({ status } as OktaIdentityProviderConnection)
                  : undefined
              }
            />
          </QueryClientProvider>
        </MemoryRouter>,
      );
      expect(screen.getByLabelText("Okta organization URL")).toBeTruthy();
      expect(screen.getAllByRole("textbox")).toHaveLength(1);
      expect(screen.queryByText("Connection")).toBeNull();
      expect(screen.queryByText("Okta setup checklist")).toBeNull();
      expect(screen.queryByRole("navigation")).toBeNull();
    },
  );
});
