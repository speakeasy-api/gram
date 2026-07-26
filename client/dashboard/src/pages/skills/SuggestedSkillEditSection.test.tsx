import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SuggestedSkillEditSection } from "./SuggestedSkillEditSection";

const testState = vi.hoisted(() => ({
  queryClient: { id: "query-client" },
  suggestion: null as Record<string, unknown> | null,
  queryError: null as Error | null,
  cachedOnError: false,
  canWrite: true,
  approve: { mutateAsync: vi.fn(), isPending: false },
  dismiss: { mutateAsync: vi.fn(), isPending: false },
  refetch: vi.fn(),
  invalidate: vi.fn().mockResolvedValue(undefined),
  toastSuccess: vi.fn(),
  toastInfo: vi.fn(),
}));

vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project_a" }) }));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => testState.queryClient,
}));
vi.mock("@gram/client/react-query/skillSuggestions.js", () => ({
  useSkillSuggestions: () => ({
    data:
      testState.queryError && !testState.cachedOnError
        ? undefined
        : {
            result: {
              suggestions: testState.suggestion ? [testState.suggestion] : [],
            },
          },
    error: testState.queryError,
    isPending: false,
    refetch: testState.refetch,
  }),
}));
vi.mock("@gram/client/react-query/approveSkillSuggestion.js", () => ({
  useApproveSkillSuggestionMutation: () => testState.approve,
}));
vi.mock("@gram/client/react-query/dismissSkillSuggestion.js", () => ({
  useDismissSkillSuggestionMutation: () => testState.dismiss,
}));
vi.mock("./invalidate-skill-queries", () => ({
  invalidateSkillQueries: testState.invalidate,
}));
vi.mock("./SkillTextDiff", () => ({
  default: ({
    oldContent,
    newContent,
  }: {
    oldContent: string;
    newContent: string;
  }) => (
    <div>
      Diff: {oldContent} to {newContent}
    </div>
  ),
}));
vi.mock("./SkillManifestDialog", () => ({
  SkillManifestDialog: ({
    open,
    initialContent,
    onSuggestionApproved,
  }: {
    open: boolean;
    initialContent: string;
    onSuggestionApproved: (outcome: "applied") => void;
  }) =>
    open ? (
      <div>
        Edit dialog: {initialContent}
        <button onClick={() => onSuggestionApproved("applied")}>
          Complete edited approval
        </button>
      </div>
    ) : null,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    scope,
    resourceId,
  }: {
    children: ReactNode;
    scope: string;
    resourceId: string;
  }) => (
    <fieldset
      disabled={!testState.canWrite}
      data-testid="write-gate"
      data-scope={scope}
      data-resource-id={resourceId}
    >
      {children}
    </fieldset>
  ),
}));
vi.mock("sonner", () => ({
  toast: { success: testState.toastSuccess, info: testState.toastInfo },
}));

const latestVersion = {
  id: "version_current",
  content: "current content",
  canonicalSha256: "1234567890abcdef",
  specValid: true,
  validationErrors: [],
} as never;

beforeEach(() => {
  testState.suggestion = {
    id: "suggestion_a",
    skillId: "skill_a",
    proposedContent: "proposed content",
    rationale: "Agents repeatedly missed an important step.",
    feedbackCount: 3,
    scoredSessionCount: 5,
    status: "open",
  };
  testState.queryError = null;
  testState.cachedOnError = false;
  testState.canWrite = true;
  testState.approve.isPending = false;
  testState.dismiss.isPending = false;
  testState.approve.mutateAsync.mockReset();
  testState.dismiss.mutateAsync.mockReset();
  testState.invalidate.mockReset().mockResolvedValue(undefined);
  testState.refetch.mockReset().mockResolvedValue({
    isSuccess: true,
    data: { result: { suggestions: [testState.suggestion] } },
  });
  testState.toastSuccess.mockReset();
  testState.toastInfo.mockReset();
});

afterEach(cleanup);

describe("SuggestedSkillEditSection", () => {
  it("shows rationale, evidence, diff, and project-scoped actions", async () => {
    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={latestVersion}
      />,
    );

    expect(
      screen.getByText("Agents repeatedly missed an important step."),
    ).toBeTruthy();
    expect(
      screen.getByText(
        (_, element) => element?.textContent === "3 feedback signals",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        (_, element) => element?.textContent === "5 scored sessions",
      ),
    ).toBeTruthy();
    expect(
      await screen.findByText("Diff: current content to proposed content"),
    ).toBeTruthy();
    expect(screen.getByTestId("write-gate").getAttribute("data-scope")).toBe(
      "skill:write",
    );
    expect(
      screen.getByTestId("write-gate").getAttribute("data-resource-id"),
    ).toBe("project_a");
  });

  it("hides an applied suggestion before a failed refresh can leave it actionable", async () => {
    testState.approve.mutateAsync.mockResolvedValue({ outcome: "applied" });
    testState.invalidate.mockRejectedValue(new Error("refresh failed"));
    const { rerender } = render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={latestVersion}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() =>
      expect(testState.approve.mutateAsync).toHaveBeenCalledWith({
        request: { approveSkillSuggestionRequestBody: { id: "suggestion_a" } },
      }),
    );
    await waitFor(() =>
      expect(screen.queryByText("Suggested edit")).toBeNull(),
    );
    expect(testState.invalidate).toHaveBeenCalledWith(testState.queryClient);
    testState.queryError = new Error("refresh failed");
    testState.cachedOnError = true;
    rerender(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={latestVersion}
      />,
    );
    expect(screen.queryByText("Suggested edit")).toBeNull();
  });

  it("hides a dismissed suggestion immediately", async () => {
    testState.dismiss.mutateAsync.mockResolvedValue({});
    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={latestVersion}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    await waitFor(() =>
      expect(testState.dismiss.mutateAsync).toHaveBeenCalledWith({
        request: { dismissSkillSuggestionRequestBody: { id: "suggestion_a" } },
      }),
    );
    await waitFor(() =>
      expect(screen.queryByText("Suggested edit")).toBeNull(),
    );
  });

  it("hides the suggestion when edited approval completes", () => {
    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={latestVersion}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Edit & approve" }));
    expect(screen.getByText("Edit dialog: proposed content")).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Complete edited approval" }),
    );
    expect(screen.queryByText("Suggested edit")).toBeNull();
  });

  it("reports and hides a superseded approval", async () => {
    testState.approve.mutateAsync.mockResolvedValue({ outcome: "superseded" });
    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={latestVersion}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    await waitFor(() =>
      expect(testState.toastInfo).toHaveBeenCalledWith(
        "Suggestion was superseded because the skill changed",
      ),
    );
    expect(screen.queryByText("Suggested edit")).toBeNull();
  });

  it("keeps approval disabled through reconciliation and until a successful refresh confirms it remains open", async () => {
    let finishInvalidation!: () => void;
    testState.invalidate.mockReturnValueOnce(
      new Promise<void>((resolve) => {
        finishInvalidation = resolve;
      }),
    );
    testState.approve.mutateAsync.mockRejectedValue(
      new Error("approval failed"),
    );
    testState.refetch
      .mockReset()
      .mockResolvedValueOnce({ isSuccess: false })
      .mockResolvedValueOnce({
        isSuccess: true,
        data: { result: { suggestions: [testState.suggestion] } },
      });
    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={latestVersion}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    expect(
      await screen.findByText(
        /Approval status may be unknown. Review the refreshed state before retrying/,
      ),
    ).toBeTruthy();
    expect(testState.invalidate).toHaveBeenCalledWith(testState.queryClient);
    expect(
      screen.getByRole("button", { name: "Approve" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen
        .getByRole("button", { name: "Refresh suggestion" })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    expect(testState.approve.mutateAsync).toHaveBeenCalledOnce();

    await act(async () => finishInvalidation());
    const refresh = screen.getByRole("button", { name: "Refresh suggestion" });
    expect(refresh.hasAttribute("disabled")).toBe(false);
    fireEvent.click(refresh);
    await waitFor(() => expect(testState.refetch).toHaveBeenCalledOnce());
    expect(
      screen.getByRole("button", { name: "Approve" }).hasAttribute("disabled"),
    ).toBe(true);

    await waitFor(() => expect(refresh.hasAttribute("disabled")).toBe(false));
    fireEvent.click(refresh);
    await waitFor(() => {
      expect(
        screen
          .getByRole("button", { name: "Approve" })
          .hasAttribute("disabled"),
      ).toBe(false);
    });
    expect(screen.queryByText(/Approval status may be unknown/)).toBeNull();
  });

  it("blocks dismissal retry and hides stale content when refresh confirms it is no longer open", async () => {
    testState.dismiss.mutateAsync.mockRejectedValue(
      new Error("dismiss failed"),
    );
    testState.refetch.mockResolvedValue({
      isSuccess: true,
      data: { result: { suggestions: [] } },
    });
    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={latestVersion}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));

    expect(
      await screen.findByText(
        /Dismissal status may be unknown. Review the refreshed state before retrying/,
      ),
    ).toBeTruthy();
    expect(testState.invalidate).toHaveBeenCalledWith(testState.queryClient);
    expect(
      screen.getByRole("button", { name: "Dismiss" }).hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(testState.dismiss.mutateAsync).toHaveBeenCalledOnce();
    const refresh = screen.getByRole("button", { name: "Refresh suggestion" });
    await waitFor(() => expect(refresh.hasAttribute("disabled")).toBe(false));
    fireEvent.click(refresh);
    await waitFor(() =>
      expect(screen.queryByText("Suggested edit")).toBeNull(),
    );
  });

  it("keeps suggestions visible but disables writes for read-only users", () => {
    testState.canWrite = false;
    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={latestVersion}
      />,
    );
    expect(screen.getByText("Suggested edit")).toBeTruthy();
    expect(
      (screen.getByTestId("write-gate") as HTMLFieldSetElement).disabled,
    ).toBe(true);
    expect(screen.getByRole("button", { name: "Approve" })).toBeTruthy();
  });

  it("shows and retries query errors", () => {
    testState.queryError = new Error("suggestions unavailable");
    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={latestVersion}
      />,
    );
    expect(screen.getByText("suggestions unavailable")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.refetch).toHaveBeenCalledOnce();
  });

  it("shows cached query errors and disables stale suggestion actions", () => {
    testState.queryError = new Error("refresh unavailable");
    testState.cachedOnError = true;
    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={latestVersion}
      />,
    );

    expect(screen.getByText("Suggested edit may be stale")).toBeTruthy();
    expect(screen.getByText(/Refresh before reviewing or acting/)).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "Approve" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      (
        screen.getByRole("button", {
          name: "Edit & approve",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.refetch).toHaveBeenCalledOnce();
  });
});
