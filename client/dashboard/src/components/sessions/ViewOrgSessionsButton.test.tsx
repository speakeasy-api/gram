import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { ViewOrgSessionsButton } from "./ViewOrgSessionsButton";

const { hasScope, health } = vi.hoisted(() => ({
  hasScope: vi.fn(),
  health: vi.fn(),
}));

vi.mock("@/pages/mcp-sessions/connection-health/flag", async (original) => ({
  ...(await original<
    typeof import("@/pages/mcp-sessions/connection-health/flag")
  >()),
  useConnectionHealthFlag: () => health(),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_example" }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: Scope, resourceId: string) => hasScope(scope, resourceId),
  }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org-slug", projectSlug: "project-slug" }),
}));

function link() {
  return screen.queryByRole("link", {
    name: /view all project sessions/i,
  });
}

describe("ViewOrgSessionsButton", () => {
  beforeEach(() => {
    hasScope.mockReturnValue(true);
    health.mockReturnValue("off");
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("links to the project MCP Sessions page with read access", () => {
    render(<ViewOrgSessionsButton />, { wrapper: MemoryRouter });

    expect(link()?.getAttribute("href")).toBe(
      "/org-slug/projects/project-slug/mcp-sessions",
    );
    expect(hasScope).toHaveBeenCalledWith("project:read", "project_example");
  });

  it("opens the sessions list instead of Overview with connection health on", () => {
    health.mockReturnValue("on");

    render(<ViewOrgSessionsButton />, { wrapper: MemoryRouter });

    expect(link()?.getAttribute("href")).toBe(
      "/org-slug/projects/project-slug/mcp-sessions?view=sessions",
    );
  });

  it("renders nothing without read access to the active project", () => {
    hasScope.mockReturnValue(false);

    render(<ViewOrgSessionsButton />, { wrapper: MemoryRouter });

    expect(link()).toBeNull();
  });
});
