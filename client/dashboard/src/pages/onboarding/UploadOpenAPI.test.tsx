import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import UploadOpenAPI from "./UploadOpenAPI";

const state = vi.hoisted(() => ({
  pending: undefined as ((pending: boolean) => void) | undefined,
  gateway: {
    gatewayId: "gateway" as string | null,
    createdServerId: null as string | null,
    isAttaching: false,
    cancel: vi.fn(),
  },
  stepper: {
    state: "error",
    meta: {
      current: { deployment: { id: "deployment" } as { id: string } | null },
    },
    reset: vi.fn(),
  },
  logs: vi.fn(),
  continue: vi.fn(),
}));
vi.mock("@/pages/mcp/gateway/useGatewayCreation", () => ({
  useGatewayCreation: () => state.gateway,
}));
vi.mock("@/pages/mcp/gateway/GatewayAttachmentStatus", () => ({
  GatewayAttachmentStatus: () => null,
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project" }) }));
vi.mock("@/components/page-templates", () => ({
  FormPage: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/upload-asset/deploy-step", () => ({
  default: ({
    onPendingChange,
  }: {
    onPendingChange?: (pending: boolean) => void;
  }) => {
    state.pending = onPendingChange;
    return null;
  },
}));
vi.mock("@/components/upload-asset/name-deployment-step", () => ({
  default: () => null,
}));
vi.mock("@/components/upload-asset/upload-file-step", () => ({
  default: () => null,
}));
vi.mock("@/components/upload-asset/step", () => {
  const Pass = ({ children }: { children: ReactNode }) => <>{children}</>;
  return {
    default: Object.assign(Pass, {
      Indicator: () => null,
      Header: () => null,
      Content: Pass,
    }),
  };
});
vi.mock("@/components/upload-asset/stepper", () => {
  const Pass = ({ children }: { children: ReactNode }) => <>{children}</>;
  return { default: { Provider: Pass, Frame: Pass } };
});
vi.mock("@/components/upload-asset/stepper/use-stepper", () => ({
  useStepper: () => state.stepper,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { goTo: state.continue },
    deployments: { deployment: { goTo: state.logs } },
  }),
}));
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.gateway.gatewayId = "gateway";
  state.gateway.createdServerId = null;
  state.gateway.isAttaching = false;
  state.stepper.state = "error";
  state.stepper.meta.current.deployment = { id: "deployment" };
});
it("disables Cancel throughout creation and allows it only once writes settle", () => {
  render(<UploadOpenAPI />);
  act(() => state.pending?.(true));
  const cancel = screen.getByRole("button", {
    name: "Cancel",
  }) as HTMLButtonElement;
  expect(cancel.disabled).toBe(true);
  fireEvent.click(cancel);
  expect(state.gateway.cancel).not.toHaveBeenCalled();
  act(() => state.pending?.(false));
  expect(cancel.disabled).toBe(false);
  fireEvent.click(cancel);
  expect(state.gateway.cancel).toHaveBeenCalledTimes(1);
});
it("exposes retained deployment logs in gateway mode without a destructive reset", () => {
  render(<UploadOpenAPI />);
  fireEvent.click(screen.getByRole("button", { name: "View Logs" }));
  expect(state.logs).toHaveBeenCalledWith("deployment");
  expect(screen.queryByRole("button", { name: "Try Again" })).toBeNull();
  expect(state.stepper.reset).not.toHaveBeenCalled();
});
it("does not offer a reset or standalone Continue for gateway creation", () => {
  state.stepper.meta.current.deployment = null;
  const view = render(<UploadOpenAPI />);
  expect(screen.queryByRole("button", { name: "Try Again" })).toBeNull();
  state.stepper.state = "completed";
  view.rerender(<UploadOpenAPI />);
  expect(screen.queryByRole("button", { name: "Continue" })).toBeNull();
});
it("keeps attachment cancellation disabled but releases stale creation state after attachment settles", () => {
  const view = render(<UploadOpenAPI />);
  act(() => state.pending?.(true));
  state.gateway.createdServerId = "server";
  state.gateway.isAttaching = true;
  view.rerender(<UploadOpenAPI />);
  expect(
    (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  state.gateway.isAttaching = false;
  view.rerender(<UploadOpenAPI />);
  expect(
    (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
      .disabled,
  ).toBe(false);
});
it("preserves standalone Continue", () => {
  state.gateway.gatewayId = null;
  state.stepper.state = "completed";
  render(<UploadOpenAPI />);
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  expect(state.continue).toHaveBeenCalledTimes(1);
});
