import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ChangeClientIdDialog } from "./clientDialogs";

const preflight = vi.hoisted(() => ({
  current: {
    data: undefined as
      | { sessionCount: number; mcpServerNames: string[] }
      | undefined,
    isFetching: false,
    isError: false,
  },
}));

vi.mock(
  "@gram/client/react-query/organizationRemoteSessionClientDeletePreflight.js",
  () => ({
    useOrganizationRemoteSessionClientDeletePreflight: () => preflight.current,
  }),
);

beforeEach(() => {
  preflight.current = {
    data: { sessionCount: 2, mcpServerNames: [] },
    isFetching: false,
    isError: false,
  };
});

afterEach(cleanup);

function renderDialog() {
  render(
    <ChangeClientIdDialog
      clientId="client-row-id"
      newClientId="corrected-id"
      isPending={false}
      onConfirm={() => {}}
      onClose={() => {}}
    />,
  );
}

it("states how many sessions the change revokes", () => {
  renderDialog();

  expect(screen.getByText("2 sessions will be revoked.")).toBeTruthy();
  expect(
    screen
      .getByRole("button", { name: "Change client ID" })
      .hasAttribute("disabled"),
  ).toBe(false);
});

it("refuses to confirm when the session count can't be loaded", () => {
  preflight.current = { data: undefined, isFetching: false, isError: true };
  renderDialog();

  expect(screen.getByRole("alert").textContent).toContain(
    "Couldn't check how many sessions this would sign out.",
  );
  expect(screen.queryByText(/will be revoked/)).toBeNull();
  expect(
    screen
      .getByRole("button", { name: "Change client ID" })
      .hasAttribute("disabled"),
  ).toBe(true);
});

it("uses the singular for one session and lists the affected MCP servers", () => {
  preflight.current = {
    data: { sessionCount: 1, mcpServerNames: ["Acme Wiki", "GitHub"] },
    isFetching: false,
    isError: false,
  };
  renderDialog();

  expect(screen.getByText("1 session will be revoked.")).toBeTruthy();
  expect(screen.getByText("Acme Wiki")).toBeTruthy();
  expect(screen.getByText("GitHub")).toBeTruthy();
});

it("holds confirmation while the session count loads", () => {
  preflight.current = { data: undefined, isFetching: true, isError: false };
  renderDialog();

  expect(screen.queryByText(/will be revoked/)).toBeNull();
  expect(
    screen
      .getByRole("button", { name: "Change client ID" })
      .hasAttribute("disabled"),
  ).toBe(true);
});
