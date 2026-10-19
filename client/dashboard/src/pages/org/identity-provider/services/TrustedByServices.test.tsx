import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { TrustedByServices } from "./TrustedByServices";
import type { ServiceRow } from "./useEmaServices";

const mocks = vi.hoisted(() => ({
  rows: [] as unknown[],
  isPending: false,
  isError: false,
  isFetching: false,
  refetch: vi.fn(),
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
  isDefault: true,
  providers: [
    {
      key: "speakeasy",
      kind: "speakeasy",
      name: "Speakeasy",
      placeholder: false,
      usable: true,
      unavailable: "",
      lastAcceptedAt,
      unusedBySignIn: false,
    },
  ],
  serverCount: 1,
  readyServerCount: 1,
  servers: [],
  hiddenServerCount: 0,
});

function show() {
  render(
    <MemoryRouter>
      <TrustedByServices />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  mocks.rows = [];
  mocks.isPending = false;
  mocks.isError = false;
});
afterEach(cleanup);

describe("Services that trust Speakeasy", () => {
  it("splits services that accepted Speakeasy from those that haven't yet", () => {
    mocks.rows = [
      service("Linear", new Date(Date.now() - 2 * 60 * 60 * 1000)),
      service("Notion", undefined),
    ];
    show();
    expect(
      screen.getByRole("heading", {
        name: "Services that trust Speakeasy",
      }),
    ).toBeTruthy();
    const accepted = within(
      screen.getByRole("list", { name: "Working" }),
    ).getAllByRole("listitem");
    expect(accepted.map((item) => item.textContent)).toEqual([
      "LinearLast worked 2h ago",
    ]);
    const waiting = within(
      screen.getByRole("list", { name: "Hasn’t worked yet" }),
    ).getAllByRole("listitem");
    expect(waiting.map((item) => item.textContent)).toEqual(["Notion"]);
    expect(
      screen.getByRole("link", { name: "Linear" }).getAttribute("href"),
    ).toBe("/?tab=identity-providers&service=https%3A%2F%2FLinear.example.com");
  });

  it("lists a service under Not working when Speakeasy can't sign, even if it worked before", () => {
    const row = service("Linear", new Date(Date.now() - 2 * 60 * 60 * 1000));
    mocks.rows = [
      {
        ...row,
        providers: row.providers.map((provider) => ({
          ...provider,
          usable: false,
          unavailable: "Not working right now",
        })),
      },
    ];
    show();
    expect(screen.queryByRole("list", { name: "Working" })).toBeNull();
    const [item] = within(
      screen.getByRole("list", { name: "Not working" }),
    ).getAllByRole("listitem");
    expect(item?.textContent).toBe("LinearNot working right now");
    expect(item?.querySelector("[data-tone]")?.getAttribute("data-tone")).toBe(
      "warning",
    );
  });

  it("leaves out services that can't use Speakeasy", () => {
    mocks.rows = [{ ...service("Linear", undefined), providers: [] }];
    show();
    expect(screen.queryByRole("link", { name: "Linear" })).toBeNull();
    expect(screen.getByText(/None yet/)).toBeTruthy();
  });

  it("says when a service shows up here", () => {
    show();
    expect(
      screen.getByText(
        /A service shows up here once an MCP server with Speakeasy sign-in turns on Enterprise Managed Authorization/,
      ),
    ).toBeTruthy();
  });

  it("offers a retry when services fail to load", () => {
    mocks.isError = true;
    show();
    expect(screen.getByText("Couldn’t load services. Try again.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.refetch).toHaveBeenCalled();
  });

  it("holds its place while loading", () => {
    mocks.isPending = true;
    show();
    expect(
      screen.getByRole("heading", {
        name: "Services that trust Speakeasy",
      }),
    ).toBeTruthy();
    expect(screen.getByRole("status").textContent).toBe("Loading services…");
  });
});
