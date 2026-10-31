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
      render(
        <SetupContext
          value={{
            s: {
              ...state,
              saved: true,
              authorized: errorStep === "test",
            },
            dispatch: vi.fn<() => void>(),
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
  render(
    <SetupContext
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
    </SetupContext>,
  );
  await user.click(screen.getByRole("button", { name: "Sign in again ↗" }));
  expect(dispatch).toHaveBeenCalledWith({ type: "modal", modal: "signin" });
});

it("allows a waiting sign-in to be cancelled", async () => {
  const { default: userEvent } = await import("@testing-library/user-event");
  const user = userEvent.setup();
  const cancelSignIn = vi.fn<() => void>();
  const state = initialState("slack");
  render(
    <SetupContext
      value={{
        s: { ...state, saved: true },
        dispatch: vi.fn<() => void>(),
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
    </SetupContext>,
  );
  await user.click(screen.getByRole("button", { name: "Cancel sign-in" }));
  expect(cancelSignIn).toHaveBeenCalledTimes(1);
});

it.each(["signin", "test"] as const)(
  "offers GitHub recovery beneath the failed %s action",
  (errorStep) => {
    const state = initialState("github");
    render(
      <SetupContext
        value={{
          s: {
            ...state,
            saved: true,
            authorized: errorStep === "test",
            values: { ...state.values, org: "example-org" },
          },
          dispatch: vi.fn<() => void>(),
          live: {
            pending: null,
            error: "Couldn’t connect GitHub.",
            errorStep,
            toolCount: null,
            loading: false,
          },
        }}
      >
        <ConnectionTask />
      </SetupContext>,
    );
    const row = screen.getByText("Couldn’t connect GitHub.").closest("tr");
    expect(row?.previousElementSibling?.textContent).toContain(
      errorStep === "signin" ? "GitHub sign-in" : "Connection test",
    );
    expect(
      screen.getByRole("button", { name: "Review credentials" }),
    ).toBeDefined();
    expect(
      screen.getByRole("button", { name: "Review app settings" }),
    ).toBeDefined();
    expect(screen.getByText("Review organization approval")).toBeDefined();
    expect(screen.getByText("Review SSO sign-in")).toBeDefined();
    expect(
      screen
        .getByRole("link", { name: "Open organization ↗" })
        .getAttribute("href"),
    ).toBe("https://github.com/example-org");
  },
);
