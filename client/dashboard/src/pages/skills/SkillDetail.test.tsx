import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SkillDetail from "./SkillDetail";

const testState = vi.hoisted(() => ({
  queryClient: { id: "query-client" },
  archive: { mutateAsync: vi.fn(), isPending: false },
  share: { mutateAsync: vi.fn(), isPending: false },
  unshare: { mutateAsync: vi.fn(), isPending: false },
  navigate: vi.fn(),
  invalidateSkills: vi.fn().mockResolvedValue(undefined),
  invalidateSkill: vi.fn().mockResolvedValue(undefined),
  invalidateDistributions: vi.fn().mockResolvedValue(undefined),
  invalidateVersions: vi.fn().mockResolvedValue(undefined),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  fetchNextPage: vi.fn(),
  isFetchNextPageError: false,
  versionError: null as Error | null,
  versions: [] as Array<Record<string, unknown>>,
  latestVersion: undefined as Record<string, unknown> | undefined,
  version: {
    id: "version_latest",
    skillId: "skill_a",
    content: "---\nname: example\ndescription: Example skill.\n---\n# Body",
    canonicalSha256: "1234567890abcdef",
    rawSha256: "abcdef",
    createdAt: new Date("2026-07-16T00:00:00Z"),
    createdByUserId: "user_a",
    description: "Example description",
    metadata: {},
    frontmatter: {
      name: "example",
      description: "Example skill.",
      license: "MIT",
    },
    specValid: true,
    validationErrors: [],
    seenCount: 3,
  },
}));

vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project_a" }) }));
vi.mock("react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  useLocation: () => ({ hash: "", pathname: "/skills/skill_a" }),
  useNavigate: () => testState.navigate,
  useParams: () => ({ skillId: "skill_a" }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: {
      href: () => "/skills",
      Link: ({ children }: { children: ReactNode }) => <>{children}</>,
      detail: { href: (id: string) => `/skills/${id}` },
    },
    plugins: { detail: { href: (id: string) => `/plugins/${id}` } },
  }),
}));
vi.mock("./SkillPluginBanner", () => ({
  SkillPluginBanner: () => <div>Distribution banner</div>,
}));
vi.mock("./SkillDistributionsSection", () => ({
  SkillDistributionsSection: () => <div>Distribution controls</div>,
}));
vi.mock("./SkillInsightsSection", () => ({
  SKILL_INSIGHTS_SECTION_ID: "insights",
  SkillInsightsSection: () => <div>Skill insights</div>,
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => testState.queryClient,
}));
vi.mock("@gram/client/react-query/skill.js", () => ({
  useSkill: () => ({
    isPending: false,
    error: null,
    data: {
      skill: {
        id: "skill_a",
        displayName: "Example",
        name: "example",
        summary: "Summary",
        classification: "custom",
        sourceKind: "manual",
        versionCount: 1,
        seenCount: 3,
        updatedAt: new Date("2026-07-16T00:00:00Z"),
      },
      latestVersion: testState.latestVersion,
      adoption: {
        activationsInWindow: 3,
        distinctHostnames: 2,
        windowStart: new Date("2026-06-16T00:00:00Z"),
        windowEnd: new Date("2026-07-16T00:00:00Z"),
      },
      drift: {
        activeMachines: 2,
        driftedMachines: 0,
        indeterminateMachines: 2,
        onTargetMachines: 0,
        targetState: "not_distributed",
        targetVersionIds: [],
        windowStart: new Date("2026-06-16T00:00:00Z"),
        windowEnd: new Date("2026-07-16T00:00:00Z"),
      },
      sightingTimeline: [],
    },
  }),
  invalidateAllSkill: testState.invalidateSkill,
}));
vi.mock("@gram/client/react-query/skillVersions.js", () => ({
  useSkillVersionsInfinite: () => ({
    isPending: false,
    error: testState.versionError,
    data: { pages: [{ result: { versions: testState.versions } }] },
    hasNextPage: false,
    isFetchingNextPage: false,
    isFetchNextPageError: testState.isFetchNextPageError,
    fetchNextPage: testState.fetchNextPage,
  }),
  invalidateAllSkillVersions: testState.invalidateVersions,
}));
vi.mock("@gram/client/react-query/skillDistributions.js", () => ({
  invalidateAllSkillDistributions: testState.invalidateDistributions,
}));
vi.mock("@gram/client/react-query/skills.js", () => ({
  invalidateAllSkills: testState.invalidateSkills,
}));
vi.mock("@gram/client/react-query/archiveSkill.js", () => ({
  useArchiveSkillMutation: () => testState.archive,
}));
vi.mock("@gram/client/react-query/shareSkill.js", () => ({
  useShareSkillMutation: () => testState.share,
}));
vi.mock("@gram/client/react-query/unshareSkill.js", () => ({
  useUnshareSkillMutation: () => testState.unshare,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    resourceId,
    scope,
  }: {
    children: ReactNode;
    resourceId: string;
    scope: string;
  }) => (
    <div
      data-testid="write-gate"
      data-resource-id={resourceId}
      data-scope={scope}
    >
      {children}
    </div>
  ),
}));
vi.mock("@/elements/components/Markdown", () => ({
  Markdown: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock("./SkillManifestDialog", () => ({ SkillManifestDialog: () => null }));
vi.mock("./EditSkillDetailsDialog", () => ({
  EditSkillDetailsDialog: () => null,
}));
vi.mock("@/components/page-layout", () => {
  const Wrapper = ({ children }: { children?: ReactNode }) => (
    <div>{children}</div>
  );
  return {
    Page: Object.assign(Wrapper, {
      Header: Object.assign(Wrapper, { Breadcrumbs: () => null }),
      Body: Wrapper,
    }),
  };
});
vi.mock("@speakeasy-api/moonshine", () => ({
  Badge: ({ children }: { children: ReactNode }) => <span>{children}</span>,
  Button: ({ children }: { children: ReactNode }) => (
    <button>{children}</button>
  ),
  Icon: () => <span />,
  Table: ({
    columns,
    data,
  }: {
    columns: Array<{
      key: string;
      render?: (row: Record<string, unknown>) => ReactNode;
    }>;
    data: Array<Record<string, unknown>>;
  }) => (
    <div>
      Version table
      {data.map((row) => (
        <div key={String(row.id)}>
          {columns.map((column) => (
            <div key={column.key}>{column.render?.(row)}</div>
          ))}
        </div>
      ))}
    </div>
  ),
}));
vi.mock("sonner", () => ({
  toast: { success: testState.toastSuccess, error: testState.toastError },
}));

beforeEach(() => {
  testState.archive.mutateAsync.mockReset();
  testState.navigate.mockReset();
  testState.invalidateSkills.mockClear();
  testState.invalidateSkill.mockClear();
  testState.invalidateDistributions.mockClear();
  testState.invalidateVersions.mockClear();
  testState.toastSuccess.mockReset();
  testState.toastError.mockReset();
  testState.fetchNextPage.mockReset();
  testState.isFetchNextPageError = false;
  testState.versionError = null;
  testState.versions = [testState.version];
  testState.latestVersion = testState.version;
});

afterEach(cleanup);

describe("SkillDetail", () => {
  it("project-scopes every write affordance", () => {
    render(<SkillDetail />);
    const gates = screen.getAllByTestId("write-gate");
    expect(gates.length).toBeGreaterThan(0);
    for (const gate of gates) {
      expect(gate.getAttribute("data-scope")).toBe("skill:write");
      expect(gate.getAttribute("data-resource-id")).toBe("project_a");
    }
  });

  it("lists validation errors for an invalid historical version", () => {
    testState.versions = [
      testState.version,
      {
        ...testState.version,
        id: "version_invalid",
        canonicalSha256: "invalid1234567890",
        specValid: false,
        validationErrors: [
          {
            code: "invalid_format",
            field: "name",
            message: "Name must be lowercase.",
          },
        ],
      },
    ];
    render(<SkillDetail />);
    expect(
      screen.getByText(
        (_, element) =>
          element?.tagName === "LI" &&
          element.textContent?.includes("Name must be lowercase.") === true,
      ),
    ).toBeTruthy();
  });

  it("shows observed metadata when manifest content was not captured", () => {
    testState.latestVersion = undefined;
    testState.versions = [];

    render(<SkillDetail />);

    expect(
      screen.getByText(
        "Manifest content has not been captured for this observed skill.",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Edit SKILL.md" })).toBeNull();
    expect(screen.queryByText("Version history")).toBeNull();
    // The banner stays visible so it can explain why distribution is blocked.
    expect(screen.getByText("Distribution banner")).toBeTruthy();
    expect(screen.queryByText("Distribution controls")).toBeNull();
  });

  it("keeps loaded versions visible and retries a next-page failure explicitly", () => {
    testState.isFetchNextPageError = true;
    testState.versionError = new Error("next page failed");
    render(<SkillDetail />);

    expect(screen.getAllByText("Version table").length).toBeGreaterThan(0);
    expect(screen.getByText("Unable to load more versions.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.fetchNextPage).toHaveBeenCalledOnce();
  });

  it("archives with the exact wrapper, navigates back, and invalidates all skill caches", async () => {
    testState.archive.mutateAsync.mockResolvedValue(undefined);
    render(<SkillDetail />);
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    fireEvent.click(screen.getByRole("button", { name: "Archive skill" }));

    await waitFor(() => {
      expect(testState.archive.mutateAsync).toHaveBeenCalledWith({
        request: { archiveSkillRequestBody: { id: "skill_a" } },
      });
    });
    expect(testState.navigate).toHaveBeenCalledWith("/skills");
    expect(testState.invalidateSkills).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateSkill).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateDistributions).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateVersions).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.toastSuccess).toHaveBeenCalledWith("Example archived");
  });
});
