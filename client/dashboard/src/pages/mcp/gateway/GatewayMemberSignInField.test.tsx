import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { GatewayMemberSignInField } from "./GatewayMemberSignInField";

const mocks = vi.hoisted(() => ({
  admin: true,
  read: vi.fn(),
  retry: vi.fn(),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ scope, children }: { scope: string; children: unknown }) =>
    scope === "org:admin" && mocks.admin ? children : null,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { details: { settings: { href: (id: string) => `/servers/${id}` } } },
  }),
}));
vi.mock("./useGatewayMemberRows", () => ({
  useGatewayMemberRows: () => ({
    rows: [
      {
        classification: "proxied",
        server: { id: "configured", name: "Configured member" },
      },
      {
        classification: "proxied",
        server: { id: "incomplete", name: "Incomplete member" },
      },
      {
        classification: "proxied",
        server: { id: "failed", name: "Unavailable member" },
      },
      {
        classification: "hosted",
        server: { id: "hosted", name: "Hosted member" },
      },
    ],
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/mcpServerAuthentication.js", () => ({
  useMcpServerAuthentication: ({ mcpServerId }: { mcpServerId: string }) => {
    mocks.read(mcpServerId);
    return {
      data: {
        status:
          mcpServerId === "incomplete" ? "setup_incomplete" : "configured",
      },
      isError: mcpServerId === "failed",
      refetch: mocks.retry,
    };
  },
}));

function renderField() {
  render(
    <MemoryRouter>
      <GatewayMemberSignInField metaMcpServerId="gateway" />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  mocks.admin = true;
});

it("reads typed state per proxied member without offering a gateway mutation", () => {
  renderField();
  expect(mocks.read.mock.calls).toEqual([
    ["configured"],
    ["incomplete"],
    ["failed"],
  ]);
  expect(screen.getByText("Upstream credentials configured")).toBeTruthy();
  expect(screen.getByText("Upstream credential setup incomplete")).toBeTruthy();
  expect(screen.getByText("Upstream sign-in status unavailable")).toBeTruthy();
  expect(screen.queryByText("Hosted member")).toBeNull();
  expect(screen.queryByRole("button", { name: /link|unlink/i })).toBeNull();
  expect(
    screen
      .getByRole("link", { name: "Repair upstream credentials" })
      .getAttribute("href"),
  ).toBe("/servers/incomplete#upstream-credentials");
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(mocks.retry).toHaveBeenCalledOnce();
});

it("does not link to management for non-admin readers", () => {
  mocks.admin = false;
  renderField();
  expect(screen.getByText("Upstream credentials configured")).toBeTruthy();
  expect(screen.queryByRole("link")).toBeNull();
});
