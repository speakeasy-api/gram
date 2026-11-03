import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { ViewOrgSessionsButton } from "./ViewOrgSessionsButton";

const { hasScope } = vi.hoisted(() => ({
  hasScope: vi.fn(),
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

  it("renders nothing without read access to the active project", () => {
    hasScope.mockReturnValue(false);

    render(<ViewOrgSessionsButton />, { wrapper: MemoryRouter });

    expect(link()).toBeNull();
  });
});
