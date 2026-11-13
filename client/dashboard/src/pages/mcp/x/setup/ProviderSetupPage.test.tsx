import { accountSetupUrls } from "@/pages/catalog/account-provider";
import { StrictMode, type Dispatch, type PropsWithChildren } from "react";
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader";
import type { McpServer } from "@gram/client/models/components/mcpserver";
import {
  names,
  type Action,
  type State,
} from "@/pages/catalog/provider-setup/state";
import { ProviderSetupPage } from "./ProviderSetupPage";
import { providerSourceFingerprint } from "./provider-setup";

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    metaMcp: {
      listMembers: mocks.members,
      addMember: mocks.addMember,
    },
  }),
}));
vi.mock("@gram/client/react-query/metaMcpMembers", () => ({
  invalidateAllMetaMcpMembers: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
const mocks = vi.hoisted(() => ({
  fire: Object.assign(vi.fn<(options: Record<string, unknown>) => void>(), {
    reset: vi.fn<() => void>(),
  }),
  create: vi.fn(),
  dispatch: vi.fn<Dispatch<Action>>(),
  provider: "github",
  oktaUrl: "https://example.okta.com/mcp",
  snowflakeUrl:
    "https://example.snowflakecomputing.com/api/v2/databases/APP/schemas/PUBLIC/mcp-servers/MCP",
  initiallyValidated: false,
  initialStep: "test",
  salesforcePath: "own",
  pending: "",
  error: "",
  loading: false,
  loadProgress: vi.fn(),
  entry: "/setup",
  members: vi.fn(),
  addMember: vi.fn(),
  configured: undefined as (() => Promise<void>) | undefined,
  headers: [] as RemoteMcpServerHeader[],
}));
vi.mock("canvas-confetti", () => ({ default: { create: mocks.create } }));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org" }),
  useProject: () => ({ id: "project" }),
  useSession: () => ({ user: { id: "person" } }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    policyCenter: { new: { href: () => "/policy/new" } },
    mcp: {
      gateway: { overview: { href: (id: string) => `/gateway/${id}` } },
      details: {
        overview: { href: () => "/overview" },
        settings: { href: () => "/settings" },
      },
    },
  }),
}));
vi.mock("@gram/client/react-query/getRemoteMcpServer", () => ({
  useGetRemoteMcpServer: () => {
    const urls: Record<string, string> = {
      okta: mocks.oktaUrl,
      github: "https://api.githubcopilot.com/mcp",
      slack: "https://mcp.slack.com/mcp",
      salesforce:
        "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
      snowflake: mocks.snowflakeUrl,
    };
    return { data: { url: urls[mocks.provider] } };
  },
}));
vi.mock("@gram/client/react-query/remoteMcpServerHeaders", () => ({
  useRemoteMcpServerHeaders: () => ({
    data: { headers: mocks.headers },
    isPending: false,
    isError: false,
  }),
}));
vi.mock("./provider-setup", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./provider-setup")>();
  const { initialState } = await import("@/pages/catalog/provider-setup/state");
  return {
    ...actual,
    loadProviderProgress: (
      provider: State["provider"],
      key: string,
      defaults?: Partial<State["values"]>,
      sourceFingerprint?: string,
    ) => {
      mocks.loadProgress(provider, key, defaults, sourceFingerprint);
      const state = initialState(provider);
      return {
        ...state,
        step: mocks.initialStep,
        saved: true,
        authorized: true,
        tested: mocks.initiallyValidated,
        values: {
          ...state.values,
          ...defaults,
          org: "example-org",
          path: mocks.salesforcePath,
        },
        sourceFingerprint,
      };
    },
  };
});
vi.mock("./useProviderConnection", () => ({
  useProviderConnection: (
    _state: State,
    dispatch: Dispatch<Action>,
    _server: McpServer,
    _source: unknown,
    configured?: () => Promise<void>,
  ) => {
    mocks.configured = configured;
    mocks.dispatch.mockImplementation(dispatch);
    return {
      dispatch,
      next: () => dispatch({ type: "next" }),
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
      <MemoryRouter initialEntries={[mocks.entry]}>
        <Routes>
          <Route
            path="/setup"
            element={<ProviderSetupPage mcpServer={server} />}
          />
          <Route path="*" element={null} />
        </Routes>
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

it.each(["slack", "github", "salesforce"])(
  "keeps the %s credentials step marker in sync with saved configuration",
  (provider) => {
    mocks.provider = provider;
    render(<Setup />);
    const step = screen.getByRole("button", { name: /Enter app credentials$/ });
    // Hydrated credentials are complete even without a recorded step visit.
    expect(step.textContent).toBe("Enter app credentials");
    act(() =>
      mocks.dispatch({
        type: "field",
        field: "clientId",
        value: "replacement",
      }),
    );
    expect(step.textContent).toMatch(/^\d+Enter app credentials$/);
    act(() => mocks.dispatch({ type: "connection", saved: true }));
    expect(step.textContent).toBe("Enter app credentials");
  },
);
function validate() {
  act(() => mocks.dispatch({ type: "connection", tested: true }));
}
it.each(["slack", "github", "salesforce"])(
  "keeps the %s final step marker in sync with connection testing",
  (provider) => {
    mocks.provider = provider;
    render(<Setup />);
    const step = screen.getByRole("button", { name: /Connect and test$/ });
    expect(step.textContent).toMatch(/^\d+Connect and test$/);
    validate();
    expect(step.textContent).toBe("Connect and test");
    act(() =>
      mocks.dispatch({
        type: "field",
        field: "clientId",
        value: "replacement",
      }),
    );
    expect(step.textContent).toMatch(/^\d+Connect and test$/);
  },
);
beforeEach(() => {
  mocks.headers = [];
  mocks.entry = "/setup";
  mocks.configured = undefined;
  vi.resetAllMocks();
  mocks.create.mockReturnValue(mocks.fire);
  mocks.members.mockResolvedValue({ members: [] });
  mocks.addMember.mockResolvedValue({});
  mocks.provider = "github";
  mocks.oktaUrl = "https://example.okta.com/mcp";
  mocks.snowflakeUrl =
    "https://example.snowflakecomputing.com/api/v2/databases/APP/schemas/PUBLIC/mcp-servers/MCP";
  mocks.initiallyValidated = false;
  mocks.initialStep = "test";
  mocks.salesforcePath = "own";
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
  it.each(["github", "slack", "salesforce", "snowflake", "okta"] as const)(
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
        `Current ${names[provider]} setup task`,
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

it("hands the native Snowflake source values and fingerprint into retained progress", () => {
  mocks.provider = "snowflake";
  render(<Setup />);
  expect(mocks.loadProgress).toHaveBeenCalledWith(
    "snowflake",
    expect.any(String),
    {
      host: "https://example.snowflakecomputing.com",
      database: "APP",
      schema: "PUBLIC",
      serverName: "MCP",
    },
    providerSourceFingerprint(server.remoteMcpServerId!, {
      url: "https://example.snowflakecomputing.com/api/v2/databases/APP/schemas/PUBLIC/mcp-servers/MCP",
    }),
  );
});

describe("Salesforce instance header entry", () => {
  it.each(["duplicate", "secret", "request"])(
    "shows actionable %s header errors before entering setup",
    (kind) => {
      mocks.provider = "salesforce";
      const header: RemoteMcpServerHeader = {
        id: "instance",
        name: "MCP-Instance-URL",
        value: "https://example.my.salesforce.com",
        isSecret: kind === "secret",
        isRequired: true,
        createdAt: new Date(),
        updatedAt: new Date(),
        valueFromRequestHeader: kind === "request" ? "X-Instance" : undefined,
      };
      mocks.headers =
        kind === "duplicate" ? [header, { ...header, id: "other" }] : [header];
      render(<Setup />);
      expect(screen.getByText(/keep one plain MCP-Instance-URL/)).toBeTruthy();
      expect(
        screen
          .getByRole("link", { name: "Open identity settings" })
          .getAttribute("href"),
      ).toBe("/settings#authentication");
      expect(
        screen.queryByRole("button", { name: "Complete setup" }),
      ).toBeNull();
    },
  );
});

it("allows Salesforce installation instructions but disables Continue at the support handoff", async () => {
  mocks.provider = "salesforce";
  mocks.initialStep = "install";
  mocks.salesforcePath = "support";
  const user = userEvent.setup();
  render(<Setup />);
  const next = () =>
    screen.getByRole<HTMLButtonElement>("button", { name: "Continue" });
  expect(next().disabled).toBe(false);
  await user.click(next());
  expect(
    screen
      .getByRole("button", { name: /Finish with support/ })
      .getAttribute("aria-current"),
  ).toBe("step");
  expect(next().disabled).toBe(true);
  expect(screen.queryByRole("button", { name: "Complete setup" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Back" }));
  expect(next().disabled).toBe(false);
});

it.each(["snowflake", "okta"] as const)(
  "opens the installed %s Setup page before its account details exist",
  (provider) => {
    mocks.provider = provider;
    mocks.snowflakeUrl = accountSetupUrls.snowflake;
    mocks.oktaUrl = accountSetupUrls.okta;
    mocks.initialStep = "begin";
    render(<Setup />);
    expect(screen.getByRole("heading", { name: "Setup plan" })).toBeDefined();
    expect(
      screen.getByRole("link", { name: "Read the guide" }).getAttribute("href"),
    ).toBe(
      provider === "okta"
        ? "https://help.okta.com/mcp/en-us/content/topics/mcpserver/oidc-pkce-browser-based.htm"
        : "https://www.speakeasy.com/docs/ai-control-plane/guides/snowflake",
    );
    expect(mocks.loadProgress).toHaveBeenCalledWith(
      provider,
      expect.any(String),
      undefined,
      providerSourceFingerprint(server.remoteMcpServerId!, {
        url: accountSetupUrls[provider],
      }),
    );
  },
);

it.each([
  ["snowflake", false],
  ["snowflake", true],
  ["okta", false],
  ["okta", true],
] as const)(
  "keeps the %s server setup destination (customize=%s)",
  async (provider, customize) => {
    mocks.provider = provider;
    mocks.entry =
      "/setup?attachToGateway=gateway-id" +
      (customize
        ? "&customizePolicy=true&categories=pii&action=block&score=3"
        : "");
    render(<Setup />);
    expect(mocks.addMember).not.toHaveBeenCalled();
    if (!mocks.configured)
      throw new Error("Expected post-configuration handoff");
    await act(async () => {
      await mocks.configured!();
    });
    expect(mocks.addMember).toHaveBeenCalledWith({
      addMetaMcpMemberForm: {
        metaMcpServerId: "gateway-id",
        mcpServerId: server.id,
      },
    });
    validate();
    await userEvent.click(completeButton());
    expect(screen.getByLabelText("Current location").textContent).toBe(
      customize ? "/policy/new" : "/gateway/gateway-id",
    );
  },
);
