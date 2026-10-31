import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ServerInfoPopover } from "./ServerInfoPopover";
import { StepServers } from "./StepServers";
import type { InventoryServer } from "./useServerInventory";
const auth = vi.hoisted(() => ({
  // Still asking, unless a test answers: the rows that do not test sign-in
  // stay free of a second "Unavailable".
  result: {
    isLoading: true,
    isError: false,
    data: undefined as unknown,
  },
  requests: [] as { mcpServerId: string }[],
}));
vi.mock("@gram/client/react-query/mcpServerAuthentication.js", () => ({
  useMcpServerAuthentication: (request: { mcpServerId: string }) => {
    auth.requests.push(request);
    return auth.result;
  },
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    toolset: { href: (slug: string) => `/toolsets/${slug}` },
    mcp: { details: { overview: { href: (id: string) => `/mcp/${id}` } } },
  }),
}));
afterEach(() => {
  cleanup();
  auth.result = { isLoading: true, isError: false, data: undefined };
  auth.requests = [];
});
const client = (id: string, clientType: string, name: string) => ({
  id,
  clientType,
  name,
  enabled: true,
  personal: clientType === "remote_session",
});
const server: InventoryServer = {
  id: "server",
  resourceId: "server",
  name: "Example",
  slug: "hidden-slug",
  projectId: "project",
  projectName: "Project",
  projectSlug: "project",
  kind: "Remote",
  upstreamUrl: "https://example.test/mcp",
};
it("opens metadata without selecting the server, and keeps its slug out of the row", () => {
  const onChange = vi.fn<() => void>();
  render(
    <StepServers
      servers={[server]}
      isLoading={false}
      isError={false}
      onRetry={vi.fn<() => void>()}
      allServers={false}
      onAllServersChange={vi.fn<() => void>()}
      scopeLabel="project"
      selected={[]}
      onChange={onChange}
    />,
  );
  expect(screen.queryByText("hidden-slug")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "About Example" }));
  expect(onChange).not.toHaveBeenCalled();
  expect(screen.getByText("hidden-slug")).toBeTruthy();
  expect(screen.getByText("No description available.")).toBeTruthy();
  expect(screen.getByText("Unavailable")).toBeTruthy();
  const link = screen.getByRole("link", { name: "Open MCP server" });
  expect(link.getAttribute("target")).toBe("_blank");
  expect(link.getAttribute("rel")).toBe("noopener noreferrer");
});
it("distinguishes zero calls from missing telemetry", () => {
  render(
    <ServerInfoPopover
      server={{ ...server, description: "A useful server", recentToolCalls: 0 }}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "About Example" }));
  expect(screen.getByText("0")).toBeTruthy();
  expect(screen.getByText("A useful server")).toBeTruthy();
});

it("links legacy toolsets by their own slug rather than endpoint suffix or MCP id", () => {
  render(
    <ServerInfoPopover
      server={{ ...server, kind: "Toolset", toolsetSlug: "actual-toolset" }}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "About Example" }));
  expect(
    screen.getByRole("link", { name: "Open MCP server" }).getAttribute("href"),
  ).toBe("/toolsets/actual-toolset");
});

it("names each way a server signs in when it offers several", () => {
  auth.result = {
    isLoading: false,
    isError: false,
    data: {
      status: "configured",
      clients: [
        client("a", "static_credential", "Automation token"),
        client("b", "remote_session", "Linear"),
        { ...client("c", "pass_through", "Old header"), enabled: false },
      ],
    },
  };
  render(<ServerInfoPopover server={server} />);
  fireEvent.click(screen.getByRole("button", { name: "About Example" }));
  expect(auth.requests[0]).toEqual({ mcpServerId: "server" });
  expect(screen.getByText("Sign-in · 2 options")).toBeTruthy();
  expect(screen.getByText("Shared credential")).toBeTruthy();
  expect(screen.getByText("Each person's own sign-in")).toBeTruthy();
  expect(screen.queryByText("Caller's own credential")).toBeNull();
});

it("says when a server needs no sign-in, and when it has none set up", () => {
  auth.result = {
    isLoading: false,
    isError: false,
    data: { status: "no_auth_required", clients: [] },
  };
  render(<ServerInfoPopover server={server} />);
  fireEvent.click(screen.getByRole("button", { name: "About Example" }));
  expect(screen.getByText("No sign-in needed")).toBeTruthy();
  cleanup();

  auth.result = {
    isLoading: false,
    isError: false,
    data: { status: "undecided", clients: [] },
  };
  render(<ServerInfoPopover server={server} />);
  fireEvent.click(screen.getByRole("button", { name: "About Example" }));
  expect(screen.getByText("Not set up")).toBeTruthy();
});

it("calls a toolset hosted by Speakeasy", () => {
  render(
    <ServerInfoPopover
      server={{ ...server, kind: "Toolset", toolsetSlug: "actual-toolset" }}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "About Example" }));
  expect(screen.getByText("Hosted by Speakeasy")).toBeTruthy();
});
