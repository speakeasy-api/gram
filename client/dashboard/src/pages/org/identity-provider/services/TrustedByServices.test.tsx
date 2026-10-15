import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { TrustedByServices } from "./TrustedByServices";
import type { ServiceRow } from "./useEmaServices";

const mocks = vi.hoisted(() => ({
  rows: [] as unknown[],
  isPending: false,
  isError: false,
}));
vi.mock("./useEmaServices", () => ({
  useEmaServices: () => mocks,
}));

const service = (
  name: string,
  lastAcceptedAt: Date | undefined,
): ServiceRow => ({
  key: `https://${name}.example.com`,
  vendorIssuer: `https://${name}.example.com`,
  name,
  isDefaultOrder: true,
  providers: [
    {
      key: "speakeasy",
      kind: "speakeasy",
      name: "Speakeasy",
      usable: true,
      unavailable: "",
      lastAcceptedAt,
      unusedBySignIn: false,
    },
  ],
  serverCount: 1,
  readyServerCount: 1,
});

function show() {
  render(
    <MemoryRouter>
      <TrustedByServices providerKey="speakeasy" />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  mocks.rows = [];
  mocks.isPending = false;
  mocks.isError = false;
});
afterEach(cleanup);

describe("Trusted by", () => {
  it("lists each service with whether it has accepted the provider", () => {
    mocks.rows = [
      service("Linear", new Date(Date.now() - 2 * 60 * 60 * 1000)),
      service("Notion", undefined),
    ];
    show();
    const items = within(
      screen.getByRole("list", { name: "Trusted by" }),
    ).getAllByRole("listitem");
    expect(items.map((item) => item.textContent)).toEqual([
      "LinearLast worked 2h ago",
      "NotionNot accepted yet",
    ]);
    expect(
      screen.getByRole("link", { name: "Linear" }).getAttribute("href"),
    ).toBe("/?tab=identity-providers&service=https%3A%2F%2FLinear.example.com");
  });

  it("names the first step when no service reaches it yet", () => {
    show();
    expect(
      screen.getByText(
        /Turn on Enterprise Managed Authorization on an MCP server/,
      ),
    ).toBeTruthy();
  });

  it("stays out of the way while loading", () => {
    mocks.isPending = true;
    show();
    expect(screen.queryByText("Trusted by")).toBeNull();
  });
});
