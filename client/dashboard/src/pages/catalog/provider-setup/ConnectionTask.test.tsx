import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { initialState } from "./state";
import { MockContext } from "./use-mock";
import { ConnectionTask } from "./ConnectionTask";

afterEach(cleanup);
describe("connection action errors", () => {
  it.each(["signin", "test"] as const)(
    "keeps a live %s failure in its own action row",
    (errorStep) => {
      const state = initialState("slack");
      render(
        <MockContext
          value={{
            s: {
              ...state,
              saved: true,
              authorized: errorStep === "test",
            },
            dispatch: vi.fn(),
            live: {
              pending: null,
              error: "Try this action again.",
              errorStep,
              toolCount: null,
              loading: false,
            },
          }}
        >
          <ConnectionTask />
        </MockContext>,
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
  const dispatch = vi.fn();
  const state = initialState("slack");
  render(
    <MockContext
      value={{
        s: {
          ...state,
          saved: true,
        },
        dispatch,
        live: {
          pending: null,
          error: "Sign in is required.",
          errorStep: "test",
          recovery: "signin",
          toolCount: null,
          loading: false,
        },
      }}
    >
      <ConnectionTask />
    </MockContext>,
  );
  await user.click(screen.getByRole("button", { name: "Sign in again ↗" }));
  expect(dispatch).toHaveBeenCalledWith({ type: "modal", modal: "signin" });
});

it("allows a waiting sign-in to be cancelled", async () => {
  const { default: userEvent } = await import("@testing-library/user-event");
  const user = userEvent.setup();
  const cancelSignIn = vi.fn();
  const state = initialState("slack");
  render(
    <MockContext
      value={{
        s: { ...state, saved: true },
        dispatch: vi.fn(),
        live: {
          pending: "signin",
          error: null,
          errorStep: "signin",
          toolCount: null,
          loading: false,
          cancelSignIn,
        },
      }}
    >
      <ConnectionTask />
    </MockContext>,
  );
  await user.click(screen.getByRole("button", { name: "Cancel sign-in" }));
  expect(cancelSignIn).toHaveBeenCalledTimes(1);
});
