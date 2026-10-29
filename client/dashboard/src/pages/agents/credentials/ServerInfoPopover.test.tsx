import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ServerInfoPopover } from "./ServerInfoPopover";
import { StepServers } from "./StepServers";
import type { InventoryServer } from "./useServerInventory";
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    toolset: { href: (slug: string) => `/toolsets/${slug}` },
    mcp: { details: { overview: { href: (id: string) => `/mcp/${id}` } } },
  }),
}));
afterEach(cleanup);
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
