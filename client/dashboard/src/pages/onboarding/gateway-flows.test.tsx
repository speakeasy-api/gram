import type { ComponentProps, ReactNode } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import UploadOpenAPI from "./UploadOpenAPI";
import FunctionsOnboarding from "./FunctionsOnboarding";
const state = vi.hoisted(() => ({
  scopes: new Set([
    "project:write:project",
    "mcp:write:project",
    "mcp:write:gateway",
  ]),
  gatewayId: "gateway" as string | null,
  createdServerId: null as string | null,
  attachmentError: null as string | null,
  isAttaching: false,
  cancel: vi.fn(),
  complete: vi.fn(),
  retry: vi.fn().mockResolvedValue(undefined),
  deploy: vi.fn(),
}));
vi.mock("@/pages/mcp/gateway/useGatewayCreation", async (importOriginal) => ({
  ...(await importOriginal<
    typeof import("@/pages/mcp/gateway/useGatewayCreation")
  >()),
  useGatewayCreation: () => state,
}));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project" }) }));
vi.mock("@/components/page-templates", async () => {
  const { RequireScope } = await import("@/components/require-scope");
  return {
    FormPage: ({
      scope,
      scopeAll,
      resourceId,
      children,
    }: ComponentProps<
      typeof import("@/components/page-templates").FormPage
    >) =>
      scope ? (
        <RequireScope
          scope={scope}
          all={scopeAll}
          resourceId={resourceId}
          level="page"
        >
          {children}
        </RequireScope>
      ) : (
        children
      ),
  };
});
vi.mock("@/components/page-layout", () => {
  const Frame = ({ children }: { children: ReactNode }) => children;
  return {
    Page: Object.assign(Frame, {
      Header: Object.assign(Frame, { Breadcrumbs: () => null }),
      Body: Frame,
    }),
  };
});
vi.mock("@/hooks/useRBAC", () => {
  const hasScope = (scope: string, resourceId?: string) =>
    resourceId
      ? state.scopes.has(`${scope}:${resourceId}`)
      : [...state.scopes].some((grant) => grant.startsWith(`${scope}:`));
  return {
    useRBAC: () => ({
      isLoading: false,
      hasScope,
      hasAllScopes: (scopes: string[], resourceId?: string) =>
        scopes.every((scope) => hasScope(scope, resourceId)),
      hasAnyScope: (scopes: string[], resourceId?: string) =>
        scopes.some((scope) => hasScope(scope, resourceId)),
    }),
  };
});
vi.mock("@gram/client/react-query/requestAccess.js", () => ({
  useRequestAccessMutation: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("@/components/functions/GettingStartedInstructions", () => ({
  GettingStartedInstructions: () => "CLI instructions",
}));
vi.mock("@/components/upload-asset/deploy-step", () => ({
  default: (props: unknown) => {
    state.deploy(props);
    return null;
  },
}));
vi.mock("@/components/upload-asset/upload-file-step", () => ({
  default: () => null,
}));
vi.mock("@/components/upload-asset/name-deployment-step", () => ({
  default: () => null,
}));
vi.mock("@/components/upload-asset/stepper", () => {
  const Frame = ({ children }: { children: ReactNode }) => children;
  return { default: { Provider: Frame, Frame } };
});
vi.mock("@/components/upload-asset/step", () => {
  const Frame = ({ children }: { children: ReactNode }) => children;
  return {
    default: Object.assign(Frame, {
      Content: Frame,
      Header: () => null,
      Indicator: () => null,
    }),
  };
});
vi.mock("@/components/upload-asset/stepper/use-stepper", () => ({
  useStepper: () => ({ state: "completed", meta: { current: {} } }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      goTo: vi.fn(),
      add: {
        fromSource: {
          Link: ({
            queryParams,
            children,
          }: {
            queryParams: Record<string, string>;
            children: ReactNode;
          }) => (
            <a
              href={"/from-existing-source?" + new URLSearchParams(queryParams)}
            >
              {children}
            </a>
          ),
        },
      },
    },
  }),
}));
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.scopes = new Set([
    "project:write:project",
    "mcp:write:project",
    "mcp:write:gateway",
  ]);
  state.gatewayId = "gateway";
  state.createdServerId = null;
  state.attachmentError = null;
});
it("keeps function gateway context when continuing from CLI instructions to source selection", () => {
  render(<FunctionsOnboarding />);
  expect(
    screen
      .getByRole("link", { name: /select deployed function/i })
      .getAttribute("href"),
  ).toBe("/from-existing-source?attachToGateway=gateway");
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(state.cancel).toHaveBeenCalled();
});
it("does not add gateway actions to standalone function instructions", () => {
  state.gatewayId = null;
  render(<FunctionsOnboarding />);
  expect(
    screen.queryByRole("link", { name: /select deployed function/i }),
  ).toBeNull();
});
it("passes gateway completion into OpenAPI creation and prevents leaving for inventory prematurely", () => {
  render(<UploadOpenAPI />);
  expect(state.deploy).toHaveBeenCalledWith(
    expect.objectContaining({ gateway: state }),
  );
  expect(screen.queryByRole("button", { name: "Continue" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(state.cancel).toHaveBeenCalled();
});
it("replaces OpenAPI creation steps with attachment recovery after creation", () => {
  state.createdServerId = "server";
  state.attachmentError = "Attachment failed";
  render(<UploadOpenAPI />);
  expect(state.deploy).not.toHaveBeenCalled();
  expect(screen.getByText("Attachment failed")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: /retry/i }));
  expect(state.retry).toHaveBeenCalled();
});
it("keeps standalone OpenAPI continue navigation", () => {
  state.gatewayId = null;
  render(<UploadOpenAPI />);
  expect(screen.getByRole("button", { name: "Continue" })).toBeTruthy();
});

it.each([UploadOpenAPI, FunctionsOnboarding])(
  "requires project-level MCP write for gateway onboarding (%s)",
  (Component) => {
    state.scopes.delete("mcp:write:project");
    render(<Component />);
    expect(state.deploy).not.toHaveBeenCalled();
    expect(screen.queryByText("CLI instructions")).toBeNull();
    expect(
      screen.queryByRole("link", { name: /select deployed function/i }),
    ).toBeNull();
  },
);
it.each([UploadOpenAPI, FunctionsOnboarding])(
  "preserves standalone onboarding without MCP write (%s)",
  (Component) => {
    state.gatewayId = null;
    state.scopes = new Set(["project:write:project"]);
    render(<Component />);
    if (Component === UploadOpenAPI) expect(state.deploy).toHaveBeenCalled();
    else expect(screen.getByText("CLI instructions")).toBeTruthy();
  },
);
