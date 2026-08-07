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
  invalidateSuggestions: vi.fn().mockResolvedValue(undefined),
  invalidateFeedback: vi.fn().mockResolvedValue(undefined),
  invalidateEfficacy: vi.fn().mockResolvedValue(undefined),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  fetchNextPage: vi.fn(),
  isFetchNextPageError: false,
  hasNextPage: false,
  versionError: null as Error | null,
  versions: [] as Array<Record<string, unknown>>,
  sightingTimeline: [] as Array<{
    bucketStart: Date;
    skillVersionId?: string;
    activationCount: number;
  }>,
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
vi.mock("./SuggestedSkillEditSection", () => ({
  SuggestedSkillEditSection: () => <div>Suggested edit review</div>,
}));
vi.mock("./SkillFeedbackSection", () => ({
  SkillFeedbackSection: () => <div>All agent reviews</div>,
}));
vi.mock("./RestoreSkillVersionDialog", () => ({
  RestoreSkillVersionDialog: () => null,
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
        tags: [],
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
      sightingTimeline: testState.sightingTimeline,
    },
  }),
  invalidateAllSkill: testState.invalidateSkill,
}));
vi.mock("@gram/client/react-query/skillVersions.js", () => ({
  useSkillVersionsInfinite: () => ({
    isPending: false,
    isError: testState.versionError !== null,
    error: testState.versionError,
    data: { pages: [{ result: { versions: testState.versions } }] },
    hasNextPage: testState.hasNextPage,
    isFetchingNextPage: false,
    isFetchNextPageError: testState.isFetchNextPageError,
    fetchNextPage: testState.fetchNextPage,
  }),
  invalidateAllSkillVersions: testState.invalidateVersions,
}));
vi.mock("@gram/client/react-query/skillSuggestions.js", () => ({
  invalidateAllSkillSuggestions: testState.invalidateSuggestions,
}));
vi.mock("@gram/client/react-query/skillFeedback.js", () => ({
  invalidateAllSkillFeedback: testState.invalidateFeedback,
}));
vi.mock("@gram/client/react-query/skillEfficacyInsights.js", () => ({
  invalidateAllSkillEfficacyInsights: testState.invalidateEfficacy,
}));
vi.mock("@gram/client/react-query/skillDistributions.js", () => ({
  invalidateAllSkillDistributions: testState.invalidateDistributions,
}));
vi.mock("@gram/client/react-query/skills.js", () => ({
  invalidateAllSkills: testState.invalidateSkills,
}));
vi.mock("@gram/client/react-query/skillTags.js", () => ({
  invalidateAllSkillTags: vi.fn().mockResolvedValue(undefined),
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
vi.mock("react-chartjs-2", () => ({
  Line: ({
    data,
  }: {
    data: { datasets: Array<{ label: string; data: number[] }> };
  }) => (
    <div data-testid="activation-chart">
      {data.datasets.map((dataset) => (
        <span key={dataset.label}>
          {dataset.label}:{dataset.data.reduce((sum, value) => sum + value, 0)}
        </span>
      ))}
    </div>
  ),
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
vi.mock("@/components/ui/Badge", () => ({
  Badge: ({ children }: { children: ReactNode }) => <span>{children}</span>,
}));

vi.mock("@/components/ui/Button", () => {
  const Button = ({
    children,
    onClick,
    disabled,
    variant,
  }: {
    children: ReactNode;
    onClick?: () => void;
    disabled?: boolean;
    variant?: string;
  }) => (
    <button onClick={onClick} disabled={disabled} data-variant={variant}>
      {children}
    </button>
  );
  Button.Text = ({ children }: { children: ReactNode }) => <>{children}</>;
  Button.LeftIcon = ({ children }: { children: ReactNode }) => <>{children}</>;
  Button.RightIcon = ({ children }: { children: ReactNode }) => <>{children}</>;
  return { Button };
});

vi.mock("@/components/ui/Icon", () => ({
  Icon: () => <span />,
}));

vi.mock("@/components/ui/Table", () => ({
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

vi.mock("@/lib/utils", () => ({
  cn: (...classes: Array<string | false | null | undefined>) =>
    classes.filter(Boolean).join(" "),
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
  testState.invalidateSuggestions.mockClear();
  testState.invalidateFeedback.mockClear();
  testState.invalidateEfficacy.mockClear();
  testState.toastSuccess.mockReset();
  testState.toastError.mockReset();
  testState.fetchNextPage.mockReset();
  testState.isFetchNextPageError = false;
  testState.hasNextPage = false;
  testState.versionError = null;
  testState.versions = [testState.version];
  testState.sightingTimeline = [];
  testState.latestVersion = testState.version;
});

afterEach(cleanup);

describe("SkillDetail", () => {
  it("charts activation counts by known and unknown version", () => {
    testState.sightingTimeline = [
      {
        bucketStart: new Date("2026-07-15T00:00:00Z"),
        skillVersionId: "version_latest",
        activationCount: 3,
      },
      {
        bucketStart: new Date("2026-07-15T00:00:00Z"),
        activationCount: 2,
      },
    ];

    render(<SkillDetail />);

    const chart = screen.getByTestId("activation-chart");
    expect(chart.textContent).toContain("v1 (12345678):3");
    expect(chart.textContent).toContain("Unknown version:2");
  });

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
    testState.hasNextPage = true;
    testState.versionError = new Error("next page failed");
    testState.sightingTimeline = [
      {
        bucketStart: new Date("2026-07-15T00:00:00Z"),
        skillVersionId: "version_latest",
        activationCount: 3,
      },
    ];
    render(<SkillDetail />);

    expect(screen.getAllByText("Version table").length).toBeGreaterThan(0);
    expect(screen.getByTestId("activation-chart")).toBeTruthy();
    expect(screen.getByText("Unable to load more versions.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.fetchNextPage).toHaveBeenCalledOnce();
  });

  it("labels valid non-current version actions by direction", () => {
    testState.versions = [
      {
        ...testState.version,
        id: "version_new",
        canonicalSha256: "newvalid12345678",
        createdAt: new Date("2026-07-17T00:00:00Z"),
      },
      testState.version,
      {
        ...testState.version,
        id: "version_old",
        canonicalSha256: "oldvalid12345678",
        createdAt: new Date("2026-07-15T00:00:00Z"),
      },
      {
        ...testState.version,
        id: "version_invalid",
        canonicalSha256: "invalid123456789",
        specValid: false,
      },
    ];
    render(<SkillDetail />);

    expect(screen.getByRole("button", { name: "Promote" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Roll back" })).toBeTruthy();
    expect(screen.getByText("Current")).toBeTruthy();
    expect(
      screen.getByRole("group", {
        name: "Version 12345678, current version",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("group", {
        name: "Version newvalid, promotion target",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("group", {
        name: "Version oldvalid, roll back target",
      }),
    ).toBeTruthy();
    expect(
      screen.getByRole("group", { name: "Version invalid1, invalid version" }),
    ).toBeTruthy();
  });

  it("uses the destructive primary archive action and archives the skill", async () => {
    testState.archive.mutateAsync.mockResolvedValue(undefined);
    render(<SkillDetail />);
    const archiveButton = screen.getByRole("button", { name: "Archive" });
    expect(archiveButton.getAttribute("data-variant")).toBe(
      "destructive-primary",
    );

    fireEvent.click(archiveButton);
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
