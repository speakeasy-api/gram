import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { EmaServices } from "./EmaServices";
import type { ServiceRow } from "./useEmaServices";

const mocks = vi.hoisted(() => ({
  rows: [] as unknown[],
  isPending: false,
  isError: false,
  isFetching: false,
  refetch: vi.fn(),
}));
vi.mock("./useEmaServices", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./useEmaServices")>()),
  useEmaServices: () => mocks,
}));

const linear: ServiceRow = {
  key: "https://linear.example.com",
  vendorIssuer: "https://linear.example.com",
  name: "Linear",
  remoteSessionIssuerId: "vendor",
  isDefault: true,
  providers: [
    {
      key: "external-default",
      kind: "external",
      name: "Your identity providers",
      placeholder: true,
      usable: true,
      unavailable: "",
      unusedBySignIn: false,
    },
    {
      key: "speakeasy",
      kind: "speakeasy",
      name: "Speakeasy",
      issuerUrl: "https://speakeasy.example.com/issuer",
      placeholder: false,
      usable: true,
      unavailable: "",
      lastAcceptedAt: new Date(Date.now() - 3 * 60 * 60 * 1000),
      unusedBySignIn: false,
    },
  ],
  serverCount: 3,
  readyServerCount: 2,
  servers: [],
  hiddenServerCount: 0,
};

function show() {
  render(
    <MemoryRouter>
      <EmaServices />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  mocks.rows = [];
  mocks.isPending = false;
  mocks.isError = false;
});
afterEach(cleanup);

describe("Services", () => {
  it("explains the once-per-service step and leads with the first action when empty", () => {
    show();
    expect(
      screen.getByRole("heading", { name: "Connected services" }),
    ).toBeTruthy();
    expect(screen.getByText(/once per service/)).toBeTruthy();
    expect(
      screen.getByText(
        /By default, all identity providers are enabled for every service\. Select Manage to change that\./,
      ),
    ).toBeTruthy();
    expect(screen.getByText("No services yet")).toBeTruthy();
    expect(
      screen.getByText(
        /Turn on Enterprise Managed Authorization for an MCP server/,
      ),
    ).toBeTruthy();
  });

  it("shows each service with who it trusts and how many servers reach it", () => {
    mocks.rows = [linear];
    show();
    expect(
      screen.getByRole("link", { name: "Linear" }).getAttribute("href"),
    ).toBe("/?tab=identity-providers&service=https%3A%2F%2Flinear.example.com");
    expect(screen.getByText("https://linear.example.com")).toBeTruthy();
    const providers = within(
      screen.getByRole("list", { name: "Linear identity providers" }),
    ).getAllByRole("listitem");
    expect(providers.map((item) => item.textContent)).toEqual([
      "Your identity providersHasn’t worked yet · for servers with identity provider sign-in",
      "SpeakeasyLast worked 3h ago · for servers with Speakeasy sign-in",
    ]);
    expect(screen.getByText("3 MCP servers · 2 ready")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Manage" }).getAttribute("href"),
    ).toBe("/?tab=identity-providers&service=https%3A%2F%2Flinear.example.com");
  });

  it("says why an identity provider can't vouch, in the row", () => {
    mocks.rows = [
      {
        ...linear,
        providers: [
          {
            key: "speakeasy",
            kind: "speakeasy",
            name: "Speakeasy",
            placeholder: false,
            usable: false,
            unavailable: "Not working right now",
            fix: "speakeasy",
            unusedBySignIn: false,
          },
        ],
        serverCount: 0,
        readyServerCount: 0,
      },
    ];
    show();
    expect(
      screen.getByText(
        "Not working right now · for servers with Speakeasy sign-in",
      ),
    ).toBeTruthy();
    expect(screen.getByText("No MCP servers yet")).toBeTruthy();
  });

  it("offers a retry when the list fails to load", () => {
    mocks.isError = true;
    show();
    screen.getByRole("button", { name: "Retry" }).click();
    expect(mocks.refetch).toHaveBeenCalled();
  });
});
