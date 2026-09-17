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
  search: "",
  sources: [] as { name: string; slug: string; kind: string }[],
  isLoading: false,
  isError: false,
  provider: vi.fn(),
  sourcesContinue: vi.fn(),
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
      current: {
        existingDocument: null as { name: string; slug: string } | null,
        deployment: { id: "deployment" } as { id: string } | null,
      },
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
  return {
    default: {
      Provider: (props: {
        children: ReactNode;
        existingDocument?: unknown;
      }) => {
        state.provider(props.existingDocument);
        return <>{props.children}</>;
      },
      Frame: Pass,
    },
  };
});
vi.mock("@/components/upload-asset/stepper/use-stepper", () => ({
  useStepper: () => state.stepper,
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { goTo: state.continue, sources: { goTo: state.sourcesContinue } },
    deployments: { deployment: { goTo: state.logs } },
  }),
}));
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.search = "";
  state.sources = [];
  state.isLoading = false;
  state.isError = false;
  state.stepper.meta.current.existingDocument = null;
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

vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useSearchParams: () => [new URLSearchParams(state.search)],
}));
vi.mock("@/components/sources/source-list", () => ({
  useProjectSources: () => ({
    sources: state.sources,
    isLoading: state.isLoading,
    isError: state.isError,
  }),
}));

it("retains the existing source in gateway mode after lookup succeeds", () => {
  state.search = "slug=original";
  state.sources = [{ name: "Original API", slug: "original", kind: "openapi" }];
  render(<UploadOpenAPI />);
  expect(state.provider).toHaveBeenCalledWith({
    name: "Original API",
    slug: "original",
  });
  expect(screen.getByRole("button", { name: "Cancel" })).toBeTruthy();
});
it.each(["loading", "error"])(
  "does not mount an upload before a safe source lookup: %s",
  (status) => {
    state.search = "slug=original";
    state.isLoading = status === "loading";
    state.isError = status === "error";
    render(<UploadOpenAPI />);
    expect(state.provider).not.toHaveBeenCalled();
  },
);
it("returns standalone source updates to sources", () => {
  state.gateway.gatewayId = null;
  state.stepper.state = "completed";
  state.stepper.meta.current.existingDocument = {
    name: "Original API",
    slug: "original",
  };
  render(<UploadOpenAPI />);
  fireEvent.click(screen.getByRole("button", { name: "Continue" }));
  expect(state.sourcesContinue).toHaveBeenCalledOnce();
  expect(state.continue).not.toHaveBeenCalled();
});
