import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { ViewOrgSessionsButton } from "./ViewOrgSessionsButton";

const { flagResult, hasAnyScope } = vi.hoisted(() => ({
  flagResult: vi.fn(),
  hasAnyScope: vi.fn(),
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => flagResult() as FeatureFlagResult,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: (scopes: Scope[]) => hasAnyScope(scopes) }),
}));

// The route helper resolves :orgSlug from the URL and renders a react-router
// Link; stubbing it keeps this test off the router.
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    userSessions: {
      Link: ({ children }: { children: React.ReactNode }) => (
        <a href="/org-slug/user-sessions">{children}</a>
      ),
    },
  }),
}));

function link() {
  return screen.queryByRole("link", {
    name: /view all organization sessions/i,
  });
}

describe("ViewOrgSessionsButton", () => {
  beforeEach(() => {
    flagResult.mockReturnValue({ status: "enabled" });
    hasAnyScope.mockReturnValue(true);
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("links to the org MCP Connections page when the flag and scopes allow it", () => {
    render(<ViewOrgSessionsButton />);

    expect(link()?.getAttribute("href")).toBe("/org-slug/user-sessions");
    expect(hasAnyScope).toHaveBeenCalledWith(["org:read", "org:admin"]);
  });

  // The destination redirects to org home when the flag is off, so the link
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

  it("renders nothing without an org-level scope", () => {
    hasAnyScope.mockReturnValue(false);

    render(<ViewOrgSessionsButton />);

    expect(link()).toBeNull();
  });
});
