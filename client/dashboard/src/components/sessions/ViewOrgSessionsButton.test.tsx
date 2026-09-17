import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { ViewOrgSessionsButton } from "./ViewOrgSessionsButton";

const { flagResult, hasScope } = vi.hoisted(() => ({
  flagResult: vi.fn(),
  hasScope: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_example" }),
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => flagResult() as FeatureFlagResult,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: Scope, resourceId: string) => hasScope(scope, resourceId),
  }),
}));

// The route helper resolves :orgSlug from the URL and renders a react-router
// Link; stubbing it keeps this test off the router.
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcpSessions: {
      Link: ({ children }: { children: React.ReactNode }) => (
        <a href="/org-slug/projects/project-slug/mcp-sessions">{children}</a>
      ),
    },
  }),
}));

function link() {
  return screen.queryByRole("link", {
    name: /view all project sessions/i,
  });
}

describe("ViewOrgSessionsButton", () => {
  beforeEach(() => {
    flagResult.mockReturnValue({ status: "enabled" });
    hasScope.mockReturnValue(true);
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("links to the project MCP Sessions page when the flag and scopes allow it", () => {
    render(<ViewOrgSessionsButton />);

    expect(link()?.getAttribute("href")).toBe(
      "/org-slug/projects/project-slug/mcp-sessions",
    );
    expect(hasScope).toHaveBeenCalledWith("project:read", "project_example");
  });

  // The destination redirects to project home when the flag is off, so the link
  // has to disappear rather than dead-end.
  it.each([
    ["disabled", { status: "disabled" }],
    ["loading", { status: "loading" }],
    ["missing", { status: "missing" }],
    ["error", { status: "error" }],
  ])("renders nothing when the flag is %s", (_name, result) => {
    flagResult.mockReturnValue(result);

    render(<ViewOrgSessionsButton />);

    expect(link()).toBeNull();
  });

  it("renders nothing without read access to the active project", () => {
    hasScope.mockReturnValue(false);

    render(<ViewOrgSessionsButton />);

    expect(link()).toBeNull();
  });
});
