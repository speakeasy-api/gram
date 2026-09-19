import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { OktaIdentityProviderConnection } from "@gram/client/models/components/oktaidentityproviderconnection.js";
import { OktaConnectionTab } from "./OktaConnectionTab";

const mocks = vi.hoisted(() => ({ create: vi.fn(), pending: false }));
vi.mock("@gram/client/react-query/createIdentityProviderConnection.js", () => ({
  useCreateIdentityProviderConnectionMutation: () => ({
    mutate: mocks.create,
    isPending: mocks.pending,
    error: null,
  }),
}));

afterEach(cleanup);
beforeEach(() => {
  mocks.create.mockReset();
  mocks.pending = false;
});

function show(connection?: OktaIdentityProviderConnection) {
  return render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <OktaConnectionTab connection={connection} />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

describe("initial Okta connection", () => {
  it.each([undefined, "revoked"] as const)(
    "shows only the organization form for %s",
    (status) => {
      show(status ? ({ status } as OktaIdentityProviderConnection) : undefined);
      expect(screen.getByLabelText("Okta organization URL")).toBeTruthy();
      expect(screen.getAllByRole("textbox")).toHaveLength(1);
      expect(screen.queryByText("How you add the app")).toBeNull();
      expect(screen.queryByText("Okta catalog (OIN)")).toBeNull();
      expect(screen.queryByText("Add your Okta organization")).toBeNull();
      expect(screen.queryByText("Set up the Okta app")).toBeNull();
      expect(screen.queryByText("Verify connection")).toBeNull();
    },
  );

  it("creates a custom-app connection with a normalized organization URL", () => {
    show();
    fireEvent.change(screen.getByLabelText("Okta organization URL"), {
      target: { value: "https://example.okta.com/" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create connection" }));
    expect(mocks.create).toHaveBeenCalledExactlyOnceWith({
      security: expect.anything(),
      request: {
        createIdentityProviderConnectionRequestBody: {
          orgUrl: "https://example.okta.com",
          listingMode: "custom_app",
        },
      },
    });
  });

  it("does not submit an invalid organization URL", () => {
    show();
    fireEvent.change(screen.getByLabelText("Okta organization URL"), {
      target: { value: "https://unrelated.example.com" },
    });
    const create = screen.getByRole("button", { name: "Create connection" });
    expect(create.hasAttribute("disabled")).toBe(true);
    fireEvent.click(create);
    expect(mocks.create).not.toHaveBeenCalled();
  });

  it("disables the form while creation is pending", () => {
    mocks.pending = true;
    show();
    expect(
      screen.getByLabelText("Okta organization URL").hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen
        .getByRole("button", { name: "Creating..." })
        .hasAttribute("disabled"),
    ).toBe(true);
  });
});
