import { StrictMode, type Dispatch, type PropsWithChildren } from "react";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver";
import type { Action, State } from "@/pages/catalog/provider-setup/state";
import { ProviderSetupPage } from "./ProviderSetupPage";

const mocks = vi.hoisted(() => ({
  fire: Object.assign(vi.fn<(options: Record<string, unknown>) => void>(), {
    reset: vi.fn<() => void>(),
  }),
  create: vi.fn(),
  dispatch: vi.fn<Dispatch<Action>>(),
  provider: "github",
  initiallyValidated: false,
  pending: "",
  error: "",
  loading: false,
}));
vi.mock("canvas-confetti", () => ({ default: { create: mocks.create } }));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org" }),
  useProject: () => ({ id: "project" }),
  useSession: () => ({ user: { id: "person" } }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      details: {
        overview: { href: () => "/overview" },
        settings: { href: () => "/settings" },
      },
    },
  }),
}));
vi.mock("@gram/client/react-query/getRemoteMcpServer", () => ({
  useGetRemoteMcpServer: () => ({
    data: {
      url:
        mocks.provider === "github"
          ? "https://api.githubcopilot.com/mcp"
          : "https://mcp.slack.com/mcp",
    },
  }),
}));
vi.mock("./provider-setup", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./provider-setup")>();
  const { initialState } = await import("@/pages/catalog/provider-setup/state");
  return {
    ...actual,
    loadProviderProgress: () => {
      const state = initialState(
        mocks.provider === "github" ? "github" : "slack",
      );
      return {
        ...state,
        step: "test",
        saved: true,
        authorized: true,
        tested: mocks.initiallyValidated,
        values: { ...state.values, org: "example-org" },
      };
    },
  };
});
vi.mock("./useProviderConnection", () => ({
  useProviderConnection: (_state: State, dispatch: Dispatch<Action>) => {
    mocks.dispatch.mockImplementation(dispatch);
    return {
      dispatch,
      pending: mocks.pending || null,
      error: mocks.error || null,
      errorStep: "test",
      loading: mocks.loading,
      toolCount: 2,
    };
  },
}));
vi.mock("@/components/icon-dither", () => ({
  useIconDither: () => ({
    canvasRef: { current: null },
    start: vi.fn(),
    stop: vi.fn(),
  }),
}));
vi.mock("@/components/brand-mesh", () => ({ BrandMeshLayers: () => null }));
vi.mock("@/components/setup-guide/SetupGuideLink", () => ({
  SetupGuideLink: () => null,
}));
vi.mock("@/pages/catalog/provider-setup/ProviderTasks", () => ({
  ProviderTask: () => null,
}));
vi.mock("@/components/page-layout", () => {
  const Container = ({ children }: PropsWithChildren) => <div>{children}</div>;
  return {
    Page: Object.assign(Container, { Header: Container, Body: Container }),
  };
});

const server: McpServer = {
  id: "server",
  projectId: "project",
  remoteMcpServerId: "remote",
  name: "Provider",
  slug: "provider",
  visibility: "private",
  networkAccessMode: "public_only",
  createdAt: new Date(),
  updatedAt: new Date(),
};
function Location() {
  return (
    <output aria-label="Current location">{useLocation().pathname}</output>
  );
}
function Setup() {
  return (
    <StrictMode>
      <MemoryRouter initialEntries={["/setup"]}>
        <ProviderSetupPage mcpServer={server} />
        <Location />
      </MemoryRouter>
    </StrictMode>
  );
}
function completeButton() {
  return screen.getByRole<HTMLButtonElement>("button", {
    name: "Complete setup",
  });
}
function validate() {
  act(() => mocks.dispatch({ type: "connection", tested: true }));
}
beforeEach(() => {
  vi.resetAllMocks();
  mocks.create.mockReturnValue(mocks.fire);
  mocks.provider = "github";
  mocks.initiallyValidated = false;
  mocks.pending = "";
  mocks.error = "";
  mocks.loading = false;
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("provider setup completion celebration", () => {
  it.each(["github", "slack"])(
    "celebrates %s after success renders, without delaying completion",
    async (provider) => {
      mocks.provider = provider;
      const user = userEvent.setup();
      const view = render(<Setup />);
      expect(completeButton().disabled).toBe(true);
      expect(mocks.fire).not.toHaveBeenCalled();
      mocks.fire.mockImplementation(() => {
        expect(screen.getByText("Passed").className).toContain(
          "text-default-success",
        );
        expect(completeButton().disabled).toBe(false);
      });
      validate();
      expect(mocks.fire).toHaveBeenCalledTimes(1);
      expect(mocks.fire).toHaveBeenCalledWith(
        expect.objectContaining({ disableForReducedMotion: true }),
      );
      const canvas = mocks.create.mock.calls[0]?.[0];
      expect(canvas).toBeInstanceOf(HTMLCanvasElement);
      expect(canvas.className).toContain("pointer-events-none");
      expect(canvas.getAttribute("aria-hidden")).toBe("true");
      expect(canvas.closest("section")?.getAttribute("aria-label")).toBe(
        `Current ${provider === "github" ? "GitHub" : "Slack"} setup task`,
      );
      view.rerender(<Setup />);
      expect(mocks.fire).toHaveBeenCalledTimes(1);
      await user.click(screen.getByRole("button", { name: /Setup plan/ }));
      expect(canvas.hidden).toBe(true);
      await user.click(
        screen.getByRole("button", { name: /Connect and test/ }),
      );
      expect(canvas.hidden).toBe(false);
      expect(mocks.fire).toHaveBeenCalledTimes(1);
      await user.click(completeButton());
      expect(screen.getByLabelText("Current location").textContent).toBe(
        "/overview",
      );
      expect(mocks.fire.reset).toHaveBeenCalledTimes(1);
    },
  );

  it.each(["saved", "authorized"] as const)(
    "does not celebrate without %s validation",
    (missing) => {
      render(<Setup />);
      act(() =>
        mocks.dispatch({ type: "connection", [missing]: false, tested: true }),
      );
      expect(completeButton().disabled).toBe(true);
      expect(mocks.fire).not.toHaveBeenCalled();
    },
  );

  it("celebrates a successful retry only after validation was invalidated", () => {
    render(<Setup />);
    validate();
    expect(mocks.fire).toHaveBeenCalledTimes(1);
    act(() => mocks.dispatch({ type: "connection", tested: false }));
    expect(completeButton().disabled).toBe(true);
    validate();
    expect(completeButton().disabled).toBe(false);
    expect(mocks.fire).toHaveBeenCalledTimes(2);
    expect(mocks.create).toHaveBeenCalledTimes(1);
  });

  it("honors reduced motion without drawing or delaying the final button", async () => {
    const { default: confetti } = await vi.importActual<{
      default: typeof import("canvas-confetti");
    }>("canvas-confetti");
    vi.stubGlobal(
      "matchMedia",
      vi.fn(() => ({ matches: true })),
    );
    mocks.create.mockImplementation(confetti.create);
    const draw = vi.spyOn(HTMLCanvasElement.prototype, "getContext");
    const user = userEvent.setup();
    render(<Setup />);
    validate();
    expect(completeButton().disabled).toBe(false);
    expect(draw).not.toHaveBeenCalled();
    await user.click(completeButton());
    expect(screen.getByLabelText("Current location").textContent).toBe(
      "/overview",
    );
  });

  it("does not celebrate when opening an already validated setup", () => {
    mocks.initiallyValidated = true;
    const view = render(<Setup />);
    expect(completeButton().disabled).toBe(false);
    view.rerender(<Setup />);
    expect(mocks.fire).not.toHaveBeenCalled();
  });

  it.each(["pending", "error", "loading"])(
    "waits for the existing %s guard before celebrating and enabling completion",
    (guard) => {
      if (guard === "pending") mocks.pending = "test";
      if (guard === "error") mocks.error = "Try again.";
      if (guard === "loading") mocks.loading = true;
      const view = render(<Setup />);
      validate();
      expect(completeButton().disabled).toBe(true);
      expect(mocks.fire).not.toHaveBeenCalled();
      mocks.pending = "";
      mocks.error = "";
      mocks.loading = false;
      view.rerender(<Setup />);
      expect(completeButton().disabled).toBe(false);
      expect(mocks.fire).toHaveBeenCalledTimes(1);
    },
  );

  it("does not replay when an already validated setup finishes loading", () => {
    mocks.initiallyValidated = true;
    mocks.loading = true;
    const view = render(<Setup />);
    mocks.loading = false;
    view.rerender(<Setup />);
    expect(completeButton().disabled).toBe(false);
    expect(mocks.fire).not.toHaveBeenCalled();
  });
});
