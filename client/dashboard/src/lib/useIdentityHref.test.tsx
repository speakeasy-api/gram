import { renderHook } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";

import { useIdentityHrefBuilder } from "./useIdentityHref";

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "default",
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: () => true,
    hasAnyScope: () => true,
    hasAllScopes: () => true,
    isLoading: false,
    grants: [],
    error: null,
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: (overrides?: { projectSlug?: string }) => {
    const section = (name: string) => ({
      href: (urn: string) =>
        `/acme/projects/${overrides?.projectSlug}/identities/${urn}/${name}`,
    });
    return {
      identities: {
        detail: { overview: section("overview"), access: section("access") },
      },
    };
  },
}));

function buildAt(
  at: string,
  ...args: Parameters<typeof useIdentityHrefBuilder>
) {
  return renderHook(() => useIdentityHrefBuilder(...args), {
    wrapper: ({ children }) => (
      <MemoryRouter initialEntries={[at]}>{children}</MemoryRouter>
    ),
  }).result.current;
}

describe("useIdentityHrefBuilder", () => {
  it("carries the reader's window onto the person's page", () => {
    const href = buildAt("/acme/mcp-sessions?range=custom&from=a&to=b&other=x");
    expect(href({ userId: "user-1" })).toBe(
      "/acme/projects/default/identities/user%3Auser-1/overview?range=custom&from=a&to=b",
    );
  });

  it("opens the project the caller names rather than the request fallback", () => {
    const href = buildAt("/acme/mcp-sessions", "access", "billing");
    expect(href({ userId: "user-1" })).toBe(
      "/acme/projects/billing/identities/user%3Auser-1/access",
    );
  });
});
