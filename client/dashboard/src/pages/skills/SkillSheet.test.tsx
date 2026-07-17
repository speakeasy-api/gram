import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SkillSheet } from "./SkillSheet";

const testState = vi.hoisted(() => ({
  queryClient: { id: "query-client" },
  archive: { mutateAsync: vi.fn(), isPending: false },
  onOpenChange: vi.fn(),
  invalidateSkills: vi.fn().mockResolvedValue(undefined),
  invalidateSkill: vi.fn().mockResolvedValue(undefined),
  invalidateVersions: vi.fn().mockResolvedValue(undefined),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  fetchNextPage: vi.fn(),
  isFetchNextPageError: false,
  versionError: null as Error | null,
  versions: [] as Array<Record<string, unknown>>,
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
  },
}));

vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project_a" }) }));
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
        updatedAt: new Date("2026-07-16T00:00:00Z"),
      },
      latestVersion: testState.version,
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
vi.mock("@gram/client/react-query/skills.js", () => ({
  invalidateAllSkills: testState.invalidateSkills,
}));
vi.mock("@gram/client/react-query/archiveSkill.js", () => ({
  useArchiveSkillMutation: () => testState.archive,
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
vi.mock("@speakeasy-api/moonshine", () => ({
  Badge: ({ children }: { children: ReactNode }) => <span>{children}</span>,
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

function renderSheet(): void {
  render(
    <SkillSheet
      skillId="skill_a"
      onOpenChange={(open) => {
        testState.onOpenChange(open);
      }}
    />,
  );
}

beforeEach(() => {
  testState.archive.mutateAsync.mockReset();
  testState.onOpenChange.mockReset();
  testState.invalidateSkills.mockClear();
  testState.invalidateSkill.mockClear();
  testState.invalidateVersions.mockClear();
  testState.toastSuccess.mockReset();
  testState.toastError.mockReset();
  testState.fetchNextPage.mockReset();
  testState.isFetchNextPageError = false;
  testState.versionError = null;
  testState.versions = [testState.version];
});

afterEach(cleanup);

describe("SkillSheet", () => {
  it("project-scopes every write affordance", () => {
    renderSheet();
    const gates = screen.getAllByTestId("write-gate");
    expect(gates).toHaveLength(1);
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
    renderSheet();
    expect(
      screen.getByText(
        (_, element) =>
          element?.tagName === "LI" &&
          element.textContent?.includes("Name must be lowercase.") === true,
      ),
    ).toBeTruthy();
  });

  it("keeps loaded versions visible and retries a next-page failure explicitly", () => {
    testState.isFetchNextPageError = true;
    testState.versionError = new Error("next page failed");
    renderSheet();

    expect(screen.getByText("Version table")).toBeTruthy();
    expect(screen.getByText("Unable to load more versions.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.fetchNextPage).toHaveBeenCalledOnce();
  });

  it("archives with the exact wrapper, closes the sheet, and invalidates all skill caches", async () => {
    testState.archive.mutateAsync.mockResolvedValue(undefined);
    renderSheet();
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    fireEvent.click(screen.getByRole("button", { name: "Archive skill" }));

    await waitFor(() => {
      expect(testState.archive.mutateAsync).toHaveBeenCalledWith({
        request: { archiveSkillRequestBody: { id: "skill_a" } },
      });
    });
    expect(testState.onOpenChange).toHaveBeenCalledWith(false);
    expect(testState.invalidateSkills).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateSkill).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateVersions).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.toastSuccess).toHaveBeenCalledWith("Example archived");
  });
});
