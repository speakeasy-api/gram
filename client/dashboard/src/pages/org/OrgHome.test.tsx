import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/components/page-layout", () => {
  function Page({ children }: { children: ReactNode }) {
    return <>{children}</>;
  }
  function Header({ children }: { children?: ReactNode }) {
    return <>{children}</>;
  }
  Header.Breadcrumbs = () => null;
  Page.Header = Header;
  Page.Body = ({ children }: { children: ReactNode }) => <>{children}</>;

  return { Page };
});

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@/components/project-menu", () => ({
  ProjectAvatar: () => <span />,
}));
vi.mock("@/components/member-facepile", () => ({
  MemberFacepile: () => <span />,
}));
vi.mock("@/components/ui/context-menu", () => ({
  ContextMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  ContextMenuTrigger: ({ children }: { children: ReactNode }) => (
    <>{children}</>
  ),
  ContextMenuContent: () => null,
  ContextMenuItem: () => null,
  ContextMenuSeparator: () => null,
}));
vi.mock("@/components/auditlogs/feed", () => ({
  ActionBadge: () => null,
  ActionDot: () => null,
}));
vi.mock("@/pages/access/ChallengesTab", () => ({
  ChallengesEmptyState: () => null,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "org-1",
    name: "Acme",
    slug: "acme",
    projects: [{ id: "project-1", name: "Project One", slug: "project-one" }],
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ projects: { create: vi.fn() } }),
  useSlugs: () => ({ orgSlug: "acme" }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ isFeatureEnabled: () => false }),
}));
vi.mock("@/hooks/useLocalStorageState", () => ({
  useLocalStorageState: () => ["list", vi.fn()],
}));
vi.mock("@/hooks/useProjectFavorites", () => ({
  useProjectFavorites: () => ({
    favoriteSet: new Set<string>(),
    isFavorite: () => false,
    toggleFavorite: vi.fn(),
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    access: {
      challenges: {
        Link: ({ children }: { children: ReactNode }) => <>{children}</>,
      },
      roles: { goTo: vi.fn() },
    },
    auditLogs: {
      Link: ({ children }: { children: ReactNode }) => <>{children}</>,
    },
    team: { goTo: vi.fn() },
  }),
}));

vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/auditLogs.js", () => ({
  useAuditLogs: () => ({ data: { result: { logs: [] } } }),
}));
vi.mock("@gram/client/react-query/challengeBuckets.js", () => ({
  useChallengeBuckets: () => ({ data: { buckets: [] }, isLoading: false }),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({ data: { logsEnabled: false } }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({ prefetchQuery: vi.fn() }),
}));
vi.mock("react-router", () => ({
  Link: ({ children, ...props }: React.ComponentProps<"a">) => (
    <a {...props}>{children}</a>
  ),
  useNavigate: () => vi.fn(),
}));
vi.mock("@speakeasy-api/moonshine", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@speakeasy-api/moonshine")>()),
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => (
    <>{children}</>
  ),
  DropdownMenuItem: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ children }: { children: ReactNode }) => (
    <>{children}</>
  ),
  Icon: () => null,
}));

import OrgHome from "./OrgHome";

afterEach(cleanup);

describe("OrgHome", () => {
  it("does not wrap project list rows in a full-height element", () => {
    render(<OrgHome />);

    const projectName = screen.getByText("Project One");
    expect(projectName.closest(".h-full")).toBeNull();
  });
});
