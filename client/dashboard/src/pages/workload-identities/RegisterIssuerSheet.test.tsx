import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { RegisterIssuerSheet } from "./RegisterIssuerSheet";

// Covered by its own tests; stubbed so these stay about where it appears.
vi.mock("./OrganizationTokenEndpoint", () => ({
  OrganizationTokenEndpoint: () => <div data-testid="token-endpoint" />,
}));

afterEach(cleanup);

function renderSheet(
  initial?: Parameters<typeof RegisterIssuerSheet>[0]["initial"],
) {
  render(
    <RegisterIssuerSheet
      open
      onOpenChange={vi.fn<(open: boolean) => void>()}
      onSubmit={vi.fn<() => void>()}
      isPending={false}
      initial={initial}
    />,
  );
}

it("shows the token endpoint to point the platform at when registering", () => {
  renderSheet();

  expect(screen.getByText("Point the platform at Gram")).toBeTruthy();
  expect(screen.getByTestId("token-endpoint")).toBeTruthy();
});

it("places the token endpoint after the values copied from the platform", () => {
  renderSheet();

  const jwks = screen.getByLabelText("JWKS URI");
  const endpoint = screen.getByTestId("token-endpoint");
  expect(
    jwks.compareDocumentPosition(endpoint) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
});

it("leaves the token endpoint out when editing a platform", () => {
  renderSheet({
    name: "Example CI",
    description: "",
    issuer: "https://ci-identity.example.com",
    jwksUri: "https://ci-identity.example.com/jwks",
    tags: [],
  });

  expect(screen.queryByText("Point the platform at Gram")).toBeNull();
  expect(screen.queryByTestId("token-endpoint")).toBeNull();
});
