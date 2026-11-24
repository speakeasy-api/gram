import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

import {
  type CommandAction,
  CommandPaletteContext,
} from "@/contexts/CommandPalette";
import { useOrgRoutes, useRoutes } from "@/routes";
import { routeToNavAction } from "../navigation-actions";
import { fuzzyScore, prefilter, rank } from "../ranker";
import type { LauncherCandidate } from "./types";
import { useActionCandidates } from "./useActionCandidates";

function candidatesFor(
  pick: (
    routes: ReturnType<typeof useRoutes>,
    orgRoutes: ReturnType<typeof useOrgRoutes>,
  ) => CommandAction[],
) {
  return renderHook(
    () => {
      const actions = pick(useRoutes(), useOrgRoutes());
      return { actions };
    },
    { wrapper: Router },
  ).result.current.actions;
}

function Router({ children }: { children: ReactNode }) {
  return <MemoryRouter initialEntries={["/org"]}>{children}</MemoryRouter>;
}

function rendered(actions: CommandAction[]) {
  const context = {
    isOpen: true,
    open: () => {},
    close: () => {},
    toggle: () => {},
    actions,
    setActions: () => {},
    addActions: () => {},
    removeActions: () => {},
    contextBadge: null,
    setContextBadge: () => {},
  };
  return renderHook(() => useActionCandidates(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <CommandPaletteContext.Provider value={context}>
        {children}
      </CommandPaletteContext.Provider>
    ),
  }).result.current;
}

// Shaped like the project gateway's Connections candidate.
const gatewayConnections: LauncherCandidate = {
  id: "gateway-connections:project:project-1",
  kind: "page",
  title: "Project Gateway Connections",
  detail: "Sessions through this project's gateway",
  keywords: ["gateway", "sessions", "connections", "project"],
  verbs: ["open"],
  group: "Pages",
  run: () => {},
};

function firstFor(query: string, candidates: LauncherCandidate[]): string {
  return rank(prefilter(query, candidates))[0]!.candidate.id;
}

describe("page action keywords", () => {
  const actions = () =>
    candidatesFor((routes, orgRoutes) => [
      routeToNavAction(routes.mcpSessions, "Pages", "nav-page-mcp-sessions"),
      routeToNavAction(
        orgRoutes.userSessionIssuers,
        "Organization",
        "nav-org-userSessionIssuers",
      ),
      routeToNavAction(routes.triggers, "Pages", "nav-page-triggers"),
    ]);

  it("finds MCP Sign-in by its former name", () => {
    const [project, org, other] = rendered(actions());

    expect(project!.title).toBe("MCP Sign-in");
    expect(fuzzyScore("sessions", { ...project!, keywords: [] })).toBe(0);
    expect(fuzzyScore("sessions", project!)).toBeGreaterThan(0);
    expect(fuzzyScore("authentication provider", org!)).toBeGreaterThan(0);
    expect(other!.keywords).toEqual([]);
  });

  it("sends each former name to the page it named", () => {
    const candidates = [...rendered(actions()), gatewayConnections];

    expect(firstFor("MCP Authentication", candidates)).toBe(
      "action:nav-org-userSessionIssuers",
    );
    expect(firstFor("MCP Sessions", candidates)).toBe(
      "action:nav-page-mcp-sessions",
    );
  });

  it("leaves connections to the gateway's pages", () => {
    const candidates = [...rendered(actions()), gatewayConnections];
    const ids = rank(prefilter("connections", candidates)).map(
      (row) => row.candidate.id,
    );

    expect(ids[0]).toBe(gatewayConnections.id);
    expect(ids).not.toContain("action:nav-page-mcp-sessions");
  });
});
