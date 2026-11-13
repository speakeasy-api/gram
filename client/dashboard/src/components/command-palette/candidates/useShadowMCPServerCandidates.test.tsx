import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  projectId: "project-1",
  navigate: vi.fn(),
  // Pages keyed by the cursor that requests them; "" is the first page.
  pages: {} as Record<
    string,
    { servers: Array<Record<string, unknown>>; nextCursor?: string } | Error
  >,
  requests: [] as Array<Record<string, unknown>>,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
  useProject: () => ({ id: mocks.projectId }),
}));
vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    shadowAI: {
      href: () => "/org/projects/p/shadow-ai",
      mcps: {
        detail: {
          href: (slug: string) => `/org/projects/p/shadow-ai/mcps/${slug}`,
        },
      },
    },
  }),
}));
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => mocks.navigate,
}));
vi.mock("@gram/client/react-query/shadowMCPInventory.core.js", () => ({
  buildShadowMCPInventoryQuery: (
    _sdk: unknown,
    request: Record<string, unknown>,
  ) => ({
    queryKey: ["unused"],
    queryFn: async () => {
      mocks.requests.push(request);
      const page = mocks.pages[(request.cursor as string | undefined) ?? ""];
      if (page instanceof Error) throw page;
      return page ?? { servers: [] };
    },
  }),
}));

import { useShadowMCPServerCandidates } from "./useShadowMCPServerCandidates";

function server(slug: string, overrides: Record<string, unknown> = {}) {
  return {
    serverSlug: slug,
    serverName: undefined,
    urlHost: `${slug}.example.com`,
    canonicalServerUrl: `https://${slug}.example.com/mcp`,
    ...overrides,
  };
}

function render(enabled: boolean) {
  const client = new QueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
  return renderHook(
    ({ open }: { open: boolean }) =>
      useShadowMCPServerCandidates({ enabled: open }),
    { wrapper, initialProps: { open: enabled } },
  );
}

function settle(): Promise<void> {
  return new Promise<void>((resolve) => {
    setTimeout(resolve, 20);
  });
}

describe("useShadowMCPServerCandidates", () => {
  beforeEach(() => {
    mocks.projectId = "project-1";
    mocks.navigate.mockReset();
    mocks.pages = {};
    mocks.requests.length = 0;
  });

  it("fetches nothing while the palette is closed", async () => {
    mocks.pages[""] = { servers: [server("notion")] };
    const { result } = render(false);

    await settle();
    expect(mocks.requests).toEqual([]);
    expect(result.current).toEqual([]);
  });

  it("indexes every page of the project's inventory and opens each server's page", async () => {
    mocks.pages[""] = {
      servers: [server("notion", { serverName: "Notion" })],
      nextCursor: "page-2",
    };
    mocks.pages["page-2"] = { servers: [server("github")] };
    const { result } = render(true);

    await waitFor(() => expect(result.current).toHaveLength(2));
    expect(mocks.requests).toEqual([
      { projectId: "project-1", limit: 200, cursor: undefined },
      { projectId: "project-1", limit: 200, cursor: "page-2" },
    ]);
    const [notion, github] = result.current;
    expect(notion).toMatchObject({
      title: "Notion",
      kind: "shadow_mcp_server",
      group: "Shadow MCP Servers",
    });
    expect(notion?.keywords).toContain("https://notion.example.com/mcp");
    // A server with no name reads as its host.
    expect(github?.title).toBe("github.example.com");

    void github?.run("open");
    expect(mocks.navigate).toHaveBeenCalledWith(
      "/org/projects/p/shadow-ai/mcps/github",
    );
  });

  it("opens the MCP servers tab for a local command, which has no page", async () => {
    mocks.pages[""] = {
      servers: [server("npx-foo", { targetKind: "stdio_command" })],
    };
    const { result } = render(true);

    await waitFor(() => expect(result.current).toHaveLength(1));
    expect(result.current[0]?.detail).toBe("Shadow MCP · local command");
    void result.current[0]?.run("open");
    expect(mocks.navigate).toHaveBeenCalledWith(
      "/org/projects/p/shadow-ai?tab=mcp-servers",
    );
  });

  it("keeps the pages it has when a later page fails, and stops there", async () => {
    mocks.pages[""] = { servers: [server("notion")], nextCursor: "page-2" };
    mocks.pages["page-2"] = new Error("boom");
    const { result } = render(true);

    await waitFor(() => expect(mocks.requests).toHaveLength(2));
    await settle();
    expect(mocks.requests).toHaveLength(2);
    expect(result.current.map((c) => c.id)).toEqual([
      "shadow_mcp_server:notion",
    ]);
  });

  it("yields nothing when the first page fails", async () => {
    mocks.pages[""] = new Error("boom");
    const { result } = render(true);

    await waitFor(() => expect(mocks.requests).toHaveLength(1));
    await settle();
    expect(mocks.requests).toHaveLength(1);
    expect(result.current).toEqual([]);
  });

  it("retries a failed page the next time the palette opens", async () => {
    mocks.pages[""] = { servers: [server("notion")], nextCursor: "page-2" };
    mocks.pages["page-2"] = new Error("boom");
    const { result, rerender } = render(true);
    await waitFor(() => expect(mocks.requests).toHaveLength(2));
    await settle();

    rerender({ open: false });
    mocks.pages["page-2"] = { servers: [server("github")] };
    rerender({ open: true });

    await waitFor(() =>
      expect(result.current.map((c) => c.id)).toEqual([
        "shadow_mcp_server:notion",
        "shadow_mcp_server:github",
      ]),
    );
  });

  it("titles a request-only local command by its command", async () => {
    mocks.pages[""] = {
      servers: [
        server("npx-foo", {
          targetKind: "stdio_command",
          urlHost: "",
          canonicalServerUrl: "npx -y foo-mcp",
        }),
      ],
    };
    const { result } = render(true);

    await waitFor(() => expect(result.current).toHaveLength(1));
    expect(result.current[0]?.title).toBe("npx -y foo-mcp");
  });
});
