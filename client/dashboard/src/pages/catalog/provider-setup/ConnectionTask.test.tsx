import { connectionFixture } from "./connection-fixture";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { initialState } from "./state";
import { SetupContext } from "./setup-context";
import { ConnectionTask } from "./ConnectionTask";

afterEach(cleanup);
describe("connection action errors", () => {
  it.each(["signin", "test"] as const)(
    "keeps a live %s failure in its own action row",
    (errorStep) => {
      const state = initialState("slack");
      const dispatch = vi.fn<() => void>();
      render(
        <SetupContext
          value={{
            s: {
              ...state,
              saved: true,
              authorized: errorStep === "test",
            },
            dispatch,
            live: connectionFixture(dispatch, {
              pending: null,
              error: "Try this action again.",
              errorStep,
              toolCount: null,
              loading: false,
            }),
          }}
        >
          <ConnectionTask />
        </SetupContext>,
      );
      const errorRow = screen.getByText("Try this action again.").closest("tr");
      expect(errorRow?.previousElementSibling?.textContent).toContain(
        errorStep === "signin" ? "Slack sign-in" : "Connection test",
      );
      expect(screen.getAllByText("Try this action again.")).toHaveLength(1);
    },
  );
});

it("offers sign-in again when the backend requires authorization", async () => {
  const { default: userEvent } = await import("@testing-library/user-event");
  const user = userEvent.setup();
  const dispatch = vi.fn<() => void>();
  const state = initialState("slack");
  const signIn = vi.fn<() => void>();
  render(
    <SetupContext
      value={{
        s: {
          ...state,
          saved: true,
        },
        dispatch,
        live: connectionFixture(dispatch, {
          pending: null,
          error: "Sign in is required.",
          signIn,
          errorStep: "test",
          recovery: "signin",
          toolCount: null,
          loading: false,
        }),
      }}
    >
      <ConnectionTask />
    </SetupContext>,
  );
  await user.click(screen.getByRole("button", { name: "Sign in again ↗" }));
  expect(signIn).toHaveBeenCalledTimes(1);
});

it("allows a waiting sign-in to be cancelled", async () => {
  const { default: userEvent } = await import("@testing-library/user-event");
  const user = userEvent.setup();
  const cancelSignIn = vi.fn<() => void>();
  const dispatch = vi.fn<() => void>();
  const state = initialState("slack");
  render(
    <SetupContext
      value={{
        s: { ...state, saved: true },
        dispatch,
        live: connectionFixture(dispatch, {
          pending: "signin",
          error: null,
          errorStep: "signin",
          toolCount: null,
          loading: false,
          cancelSignIn,
        }),
      }}
    >
      <ConnectionTask />
    </SetupContext>,
  );
  await user.click(screen.getByRole("button", { name: "Cancel sign-in" }));
  expect(cancelSignIn).toHaveBeenCalledTimes(1);
});

it.each([
  [0, "Connected · 0 tools discovered. No tools were run."],
  [1, "Connected · 1 tool discovered. No tools were run."],
  [2, "Connected · 2 tools discovered. No tools were run."],
] as const)("shows the discovery result for %s tools", (count, copy) => {
  const dispatch = vi.fn<() => void>();
  render(
    <SetupContext
      value={{
        s: {
          ...initialState("slack"),
          saved: true,
          authorized: true,
          tested: true,
        },
        dispatch,
        live: connectionFixture(dispatch, { toolCount: count }),
      }}
    >
      <ConnectionTask />
    </SetupContext>,
  );
  screen.getByText(copy);
});
