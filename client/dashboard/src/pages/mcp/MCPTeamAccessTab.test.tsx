import type { ReactNode } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

vi.mock("@/components/page-layout", () => {
  const Section = ({ children }: { children: ReactNode }) => (
    <section>{children}</section>
  );
  return {
    Page: {
      Section: Object.assign(Section, {
        Title: Section,
        Description: Section,
        Body: Section,
      }),
    },
  };
});
vi.mock("./access/CheckAccess", () => ({ CheckAccess: () => null }));

const state = vi.hoisted(() => ({
  admin: true,
  failed: false,
  query: vi.fn(),
  scopes: vi.fn(),
}));
vi.mock("@gram/client/react-query/getRemoteMcpServerScopes.js", () => ({
  useGetRemoteMcpServerScopes: (...args: unknown[]) => state.scopes(...args),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => state.admin }),
}));
vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  useResourceAudience: (...args: unknown[]) => {
    state.query(...args);
    return {
      data: {
        entries: [
          {
            principalUrn: "role:global:1",
            displayName: "Engineering",
            kind: "role",
            level: "use",
            appliesTo: "resource",
          },
        ],
        version: "v1",
        rolePlugins: [
          {
            principalUrn: "role:global:1",
            pluginId: "plugin-1",
            name: "Tools",
            slug: "tools",
          },
        ],
      },
      isLoading: false,
      isError: state.failed,
    };
  },
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));
vi.mock("./access/ManageAccess", () => ({
  ManageAccess: ({
    entries,
    rolePlugins,
    version,
  }: {
    entries: { displayName: string }[];
    rolePlugins?: { name: string }[];
    version: string;
  }) => (
    <div>
      <span>{version}</span>
      {entries.map((entry) => (
        <span key={entry.displayName}>{entry.displayName}</span>
      ))}
      {rolePlugins?.map((plugin) => (
        <span key={plugin.name}>{plugin.name}</span>
      ))}
    </div>
  ),
}));

import { MCPTeamAccessTab } from "./MCPTeamAccessTab";

afterEach(cleanup);
beforeEach(() => {
  state.admin = true;
  state.failed = false;
  vi.clearAllMocks();
});

it("keeps existing audience rows after permission downgrade but hides cached plugin associations", () => {
  const { rerender } = render(
    <MCPTeamAccessTab resourceId="server-1" checkAccess={false} />,
  );
  expect(screen.getByText("Tools")).toBeDefined();
  state.admin = false;
  rerender(<MCPTeamAccessTab resourceId="server-1" checkAccess={false} />);
  expect(screen.queryByText("Tools")).toBeNull();
  expect(screen.getByText("Engineering")).toBeDefined();
  expect(screen.getByText("v1")).toBeDefined();
  expect(state.query).toHaveBeenLastCalledWith(
    { resourceKind: "mcp", resourceId: "server-1" },
    undefined,
    { throwOnError: false },
  );
});

it("does not present failed audience reads as an empty distribution", () => {
  state.failed = true;
  render(<MCPTeamAccessTab resourceId="server-1" checkAccess={false} />);
  expect(screen.getByText(/Access rules could not be loaded/)).toBeDefined();
  expect(screen.queryByText("Tools")).toBeNull();
});

it("does not read the server's requested scopes", () => {
  render(<MCPTeamAccessTab resourceId="server-1" checkAccess={false} />);
  expect(state.scopes).not.toHaveBeenCalled();
});
