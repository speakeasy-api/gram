import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { GatewayAttachmentStatus } from "./GatewayAttachmentStatus";
import type { GatewayCreationFlow } from "./useGatewayCreation";
afterEach(cleanup);
function flow(
  overrides: Partial<GatewayCreationFlow> = {},
): GatewayCreationFlow {
  return {
    gatewayId: "gateway",
    createdServerId: "server",
    attachmentError: null,
    isAttaching: false,
    complete: vi.fn(),
    retry: vi.fn().mockResolvedValue(undefined),
    cancel: vi.fn(),
    ...overrides,
  };
}
it("announces attachment retry progress after the previous error is cleared", () => {
  const state = flow({ attachmentError: "Could not attach" });
  const { rerender } = render(<GatewayAttachmentStatus flow={state} />);
  expect(screen.getByRole("alert").textContent).toContain("Could not attach");
  expect(screen.queryByRole("status")).toBeNull();
  fireEvent.click(
    screen.getByRole("button", { name: "Retry adding to gateway" }),
  );
  expect(state.retry).toHaveBeenCalledOnce();
  rerender(
    <GatewayAttachmentStatus
      flow={{ ...state, attachmentError: null, isAttaching: true }}
    />,
  );
  expect(screen.getByRole("alert").textContent).toContain("Adding to gateway…");
  expect(screen.queryByRole("status")).toBeNull();
  expect(screen.getAllByText("Adding to gateway…")).toHaveLength(1);
  expect(screen.queryByText("Could not attach")).toBeNull();
  expect(
    (
      screen.getByRole("button", {
        name: "Adding to gateway…",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});
it("hides status when neither attaching nor failed", () => {
  const { container } = render(<GatewayAttachmentStatus flow={flow()} />);
  expect(container.textContent).toBe("");
});
