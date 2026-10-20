import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

vi.mock("../access/MCPAssignments", () => ({
  MCPAssignments: ({ onReady }: { onReady: (ready: boolean) => void }) => (
    <>
      <button onClick={() => onReady(true)}>Assignments loaded</button>
      <button onClick={() => onReady(false)}>Assignments failed</button>
    </>
  ),
}));
import { GatewayAccessReview } from "./GatewayAccessReview";

afterEach(cleanup);

it("requires complete assignments and explicit sign-off before enabling", () => {
  const onEnable = vi.fn();
  render(
    <GatewayAccessReview
      isPending={false}
      onEnable={onEnable}
      onCancel={vi.fn()}
    />,
  );
  const enable = screen.getByRole("button", {
    name: "Enable Organization Gateway",
  }) as HTMLButtonElement;
  expect(enable.disabled).toBe(true);
  fireEvent.click(enable);
  expect(onEnable).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Assignments loaded" }));
  expect(enable.disabled).toBe(true);
  fireEvent.click(screen.getByRole("checkbox"));
  expect(enable.disabled).toBe(false);
  fireEvent.click(enable);
  expect(onEnable).toHaveBeenCalledOnce();
});

it("clears sign-off if the complete review becomes unavailable", () => {
  render(
    <GatewayAccessReview
      isPending={false}
      onEnable={vi.fn()}
      onCancel={vi.fn()}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Assignments loaded" }));
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(screen.getByRole("button", { name: "Assignments failed" }));
  fireEvent.click(screen.getByRole("button", { name: "Assignments loaded" }));
  expect(
    (
      screen.getByRole("button", {
        name: "Enable Organization Gateway",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  expect(screen.getByRole("checkbox").getAttribute("aria-checked")).toBe(
    "false",
  );
});
