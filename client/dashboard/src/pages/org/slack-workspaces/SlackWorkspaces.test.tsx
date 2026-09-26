import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, useLocation } from "react-router";
import type { ReactNode } from "react";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { SlackWorkspaces } from "./SlackWorkspaces";

const mocks = vi.hoisted(() => ({
  admin: true,
  organizationSlug: "example",
  configured: true,
  generation: "generation-one",
  mutate: vi.fn(),
  list: vi.fn(),
  invalidate: vi.fn(),
  begin: vi.fn(),
}));
vi.mock("nuqs", async (original) => {
  const actual = await original<typeof import("nuqs")>();
  const { useSearchParams } = await import("react-router");
  const { useCallback } = await import("react");
  return {
    ...actual,
    useQueryState: (key: string) => {
      const [params, setParams] = useSearchParams();
      const set = useCallback(
        (value: string | null) => {
          setParams(
            (previous) => {
              const next = new URLSearchParams(previous);
              if (value === null) next.delete(key);
              else next.set(key, value);
              return next;
            },
            { replace: true },
          );
        },
        [key, setParams],
      );
      return [params.get(key), set];
    },
  };
});
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ slug: mocks.organizationSlug }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    scope,
    children,
  }: {
    scope: string;
    children: ReactNode;
  }) =>
    mocks.admin && scope === "org:admin" ? children : <div>Unauthorized</div>,
}));
vi.mock("@gram/client/react-query/slackDirectoryConnections.js", () => ({
  invalidateAllSlackDirectoryConnections: (...args: unknown[]) =>
    mocks.invalidate(...args),
  useSlackDirectoryConnections: () => {
    mocks.list();
    return {
      data: {
        authorizationConfigured: mocks.configured,
        connections: [
          {
            id: "connection-one",
            workspaceId: "TEXAMPLE01",
            workspaceName: "Example workspace",
            status: "connected",
            generation: mocks.generation,
            grantedScopes: [],
            updatedAt: "2026-01-01T00:00:00Z",
          },
        ],
      },
      isPending: false,
      isError: false,
      error: null,
    };
  },
}));
vi.mock("@gram/client/react-query/beginSlackDirectoryConnection.js", () => ({
  useBeginSlackDirectoryConnectionMutation: () => ({
    mutate: mocks.begin,
    isPending: false,
    error: null,
  }),
}));
vi.mock(
  "@gram/client/react-query/disconnectSlackDirectoryConnection.js",
  () => ({
    useDisconnectSlackDirectoryConnectionMutation: () => ({
      mutate: mocks.mutate,
      reset: vi.fn(),
      isPending: false,
      error: null,
    }),
  }),
);

function Location() {
  return <output data-testid="location">{useLocation().search}</output>;
}
function show(search = "") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const tree = () => (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/example/identity${search}`]}>
        <TooltipProvider>
          <SlackWorkspaces />
          <Location />
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>
  );
  const view = render(tree());
  return { ...view, refresh: () => view.rerender(tree()) };
}

beforeEach(() => {
  mocks.admin = true;
  mocks.organizationSlug = "example";
  mocks.configured = true;
  mocks.generation = "generation-one";
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

it("does not fetch workspace data for a non-admin", () => {
  mocks.admin = false;
  show();
  expect(mocks.list).not.toHaveBeenCalled();
});
it("disables connect when the deployment is unconfigured", () => {
  mocks.configured = false;
  show();
  expect(
    screen
      .getByRole("button", { name: "Connect Slack" })
      .hasAttribute("disabled"),
  ).toBe(true);
  expect(screen.getByText(/Ask your deployment administrator/)).toBeTruthy();
});
it.each([
  ["connected", "Slack workspace connected."],
  [
    "cancelled",
    "Slack authorization was cancelled. Your connections have not changed.",
  ],
  [
    "invalid_state",
    "This authorization link expired or belongs to another session. Connect Slack again.",
  ],
  [
    "wrong_workspace",
    "That is a different Slack workspace. Connect Slack again and choose the workspace shown here.",
  ],
  [
    "connection_changed",
    "The workspace state changed during authorization. Try connecting again.",
  ],
  [
    "authorization_failed",
    "Slack authorization failed. Try connecting again, or contact your administrator.",
  ],
  [
    "unavailable",
    "Slack connections are unavailable or your access changed. Return to Identity and try again.",
  ],
])("renders and clears the %s outcome", async (outcome, message) => {
  show(`?tab=slack-workspaces&slack_result=${outcome}`);
  await waitFor(() =>
    expect(screen.getByTestId("location").textContent).not.toContain(
      "slack_result",
    ),
  );
  expect(mocks.invalidate).toHaveBeenCalled();
  expect(screen.getByText(message)).toBeTruthy();
  expect(screen.getByRole("button", { name: /dismiss|close/i })).toBeTruthy();
});

it("disconnects the generation the administrator confirmed", () => {
  show();
  fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
  fireEvent.click(screen.getByRole("button", { name: "Disconnect workspace" }));
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      request: {
        disconnectSlackDirectoryConnectionRequestBody: {
          id: "connection-one",
          generation: "generation-one",
        },
      },
    }),
  );
});

it.each(["__proto__", "constructor", "toString", "unexpected"])(
  "ignores an unknown %s callback outcome",
  async (outcome) => {
    show(`?tab=slack-workspaces&slack_result=${outcome}`);
    await waitFor(() =>
      expect(screen.getByTestId("location").textContent).not.toContain(
        "slack_result",
      ),
    );
    expect(screen.getByRole("button", { name: "Connect Slack" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: /dismiss|close/i })).toBeNull();
  },
);

it.each([
  "http://slack.com/oauth/v2/authorize",
  "https://example.com/oauth/v2/authorize",
  "https://slack.com/other",
])("rejects an unexpected authorization destination %s", (authorizationUrl) => {
  mocks.begin.mockImplementationOnce((_request, options) =>
    options.onSuccess({ authorizationUrl }),
  );
  show();
  fireEvent.click(screen.getByRole("button", { name: "Connect Slack" }));
  expect(screen.getByText(/invalid authorization link/)).toBeTruthy();
});

it("requires confirmation again when the workspace changed", () => {
  const view = show();
  fireEvent.click(screen.getByRole("button", { name: "Disconnect" }));
  mocks.generation = "generation-two";
  view.refresh();
  fireEvent.click(screen.getByRole("button", { name: "Disconnect workspace" }));
  expect(mocks.mutate).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Disconnect workspace" }));
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      request: {
        disconnectSlackDirectoryConnectionRequestBody: {
          id: "connection-one",
          generation: "generation-two",
        },
      },
    }),
  );
});

it.each([true, false])(
  "keeps shared-demo controls read-only when configured=%s",
  (configured) => {
    mocks.organizationSlug = DEMO_ORG_SLUG;
    mocks.configured = configured;
    show();
    expect(screen.getByText("Example workspace")).toBeTruthy();
    expect(
      screen.getByText(
        /Slack workspaces are read-only in the demo organization/,
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/Ask your deployment administrator/)).toBeNull();
    for (const name of ["Connect Slack", "Disconnect"]) {
      const button = screen.getByRole("button", { name });
      expect(button.hasAttribute("disabled")).toBe(true);
      fireEvent.click(button);
    }
    expect(mocks.begin).not.toHaveBeenCalled();
    expect(mocks.mutate).not.toHaveBeenCalled();
  },
);

it("has no per-workspace Reconnect button", () => {
  show();
  expect(screen.getByText("Example workspace")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Reconnect" })).toBeNull();
});
