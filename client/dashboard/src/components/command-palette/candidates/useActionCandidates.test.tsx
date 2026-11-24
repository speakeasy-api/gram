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
import { fuzzyScore } from "../ranker";
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

describe("page action keywords", () => {
  it("finds MCP Sign-in by its former name", () => {
    const actions = candidatesFor((routes, orgRoutes) => [
      routeToNavAction(routes.mcpSessions, "Pages", "nav-page-mcp-sessions"),
      routeToNavAction(
        orgRoutes.userSessionIssuers,
        "Organization",
        "nav-org-userSessionIssuers",
      ),
      routeToNavAction(routes.triggers, "Pages", "nav-page-triggers"),
    ]);
    const [project, org, other] = rendered(actions);

    expect(project!.title).toBe("MCP Sign-in");
    expect(fuzzyScore("sessions", { ...project!, keywords: [] })).toBe(0);
    expect(fuzzyScore("sessions", project!)).toBeGreaterThan(0);
    expect(fuzzyScore("mcp sessions", project!)).toBeGreaterThan(0);
    expect(fuzzyScore("authentication provider", org!)).toBeGreaterThan(0);
    expect(other!.keywords).toEqual([]);
  });
});
