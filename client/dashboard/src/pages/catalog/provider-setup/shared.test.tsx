import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useReducer } from "react";
import { SetupContext } from "./setup-context";
import { initialState, reduce } from "./state";
import { CopyValue } from "./shared";
function Subject() {
  const [s, dispatch] = useReducer(reduce, initialState("slack"));
  return (
    <SetupContext value={{ s, dispatch }}>
      <CopyValue
        label="Redirect URL"
        value="https://callback.example/callback"
      />
    </SetupContext>
  );
}
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
describe("setup clipboard", () => {
  it("shows success only after the clipboard write succeeds", async () => {
    const user = userEvent.setup();
    let finish!: () => void;
    vi.spyOn(navigator.clipboard, "writeText").mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    render(<Subject />);
    await user.click(screen.getByRole("button", { name: "Copy" }));
    expect(screen.queryByText("Copied")).toBeNull();
    finish();
    await screen.findByRole("button", { name: "Copied" });
  });
  it("recovers inline from a failed clipboard write", async () => {
    const user = userEvent.setup();
    const write = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockRejectedValueOnce(new Error("Denied"));
    render(<Subject />);
    await user.click(screen.getByRole("button", { name: "Copy" }));
    await screen.findByText(
      "Couldn’t copy. Select the value and copy it manually.",
    );
    expect(screen.queryByText("Copied")).toBeNull();
    write.mockResolvedValue();
    await user.click(screen.getByRole("button", { name: "Copy" }));
    await waitFor(() =>
      expect(
        screen.queryByText(
          "Couldn’t copy. Select the value and copy it manually.",
        ),
      ).toBeNull(),
    );
    expect(screen.getByRole("button", { name: "Copied" })).toBeDefined();
  });
});
