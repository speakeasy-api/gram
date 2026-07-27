import type { SkillEditSuggestion } from "@gram/client/models/components/skilleditsuggestion.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SkillSuggestionComment } from "./SkillSuggestionComment";

const testState = vi.hoisted(() => ({
  feedback: [] as Array<{
    id: string;
    outcome: string;
    note?: string;
    createdAt: Date;
  }>,
  enabled: false as boolean,
}));

vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project_a" }) }));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@gram/client/react-query/skillSuggestionFeedback.js", () => ({
  useSkillSuggestionFeedback: (
    _request: unknown,
    _options: unknown,
    config: { enabled: boolean },
  ) => {
    testState.enabled = config.enabled;
    return {
      isPending: false,
      error: null,
      data: config.enabled ? { feedback: testState.feedback } : undefined,
    };
  },
}));

function suggestion(
  overrides: Partial<SkillEditSuggestion> = {},
): SkillEditSuggestion {
  return {
    id: "suggestion_a",
    skillId: "skill_a",
    skillName: "incident-review",
    skillDisplayName: "Incident review",
    baseVersionID: "version_a",
    proposedDiff: "",
    proposedContent: "",
    appliesCleanly: true,
    rationale: "Leadership reviews stall without a number here.",
    status: "open",
    feedbackCount: 11,
    feedbackSessionCount: 11,
    scoredSessionCount: 4,
    createdAt: new Date("2026-07-01T00:00:00Z"),
    updatedAt: new Date("2026-07-01T00:00:00Z"),
    ...overrides,
  } as SkillEditSuggestion;
}

const actions = {
  disabled: false,
  approving: false,
  onApply: vi.fn(),
  onApplyAll: vi.fn(),
};

describe("SkillSuggestionComment", () => {
  afterEach(cleanup);
  beforeEach(() => {
    testState.feedback = [];
    testState.enabled = false;
  });

  it("leads with the session count the suggestion was built from", () => {
    render(
      <SkillSuggestionComment
        suggestion={suggestion()}
        changeCount={1}
        actions={actions}
      />,
    );

    expect(screen.getByText(/Requested in 11 sessions\./)).toBeTruthy();
    expect(
      screen.getByText(/Leadership reviews stall without a number here\./),
    ).toBeTruthy();
  });

  it("omits the session count when no session reported the feedback", () => {
    render(
      <SkillSuggestionComment
        suggestion={suggestion({ feedbackSessionCount: 0 })}
        changeCount={1}
        actions={actions}
      />,
    );

    expect(screen.queryByText(/Requested in/)).toBeNull();
  });

  it("loads the source reports only once the reviewer expands them", () => {
    testState.feedback = [
      {
        id: "feedback_a",
        outcome: "did_not_help",
        note: "No impact numbers to cite.",
        createdAt: new Date("2026-07-01T00:00:00Z"),
      },
    ];
    render(
      <SkillSuggestionComment
        suggestion={suggestion()}
        changeCount={1}
        actions={actions}
      />,
    );

    expect(testState.enabled).toBe(false);
    fireEvent.click(
      screen.getByRole("button", { name: /Built from 11 agent reports/ }),
    );
    expect(testState.enabled).toBe(true);
    expect(screen.getByText("No impact numbers to cite.")).toBeTruthy();
  });

  it("hides the source expander when nothing is linked", () => {
    render(
      <SkillSuggestionComment
        suggestion={suggestion({ feedbackCount: 0 })}
        changeCount={1}
        actions={actions}
      />,
    );

    expect(screen.queryByText(/agent report/)).toBeNull();
  });
});

describe("SkillSuggestionComment actions", () => {
  afterEach(cleanup);

  it("offers applying everything only when more than one change is proposed", () => {
    const { rerender } = render(
      <SkillSuggestionComment
        suggestion={suggestion()}
        changeCount={1}
        actions={actions}
      />,
    );
    expect(screen.queryByRole("button", { name: "Apply all" })).toBeNull();

    rerender(
      <SkillSuggestionComment
        suggestion={suggestion()}
        changeCount={3}
        actions={actions}
      />,
    );
    expect(screen.getByRole("button", { name: "Apply all" })).toBeTruthy();
  });

  it("keeps applying one change separate from applying them all", () => {
    const onApply = vi.fn();
    const onApplyAll = vi.fn();
    render(
      <SkillSuggestionComment
        suggestion={suggestion()}
        changeCount={2}
        actions={{ ...actions, onApply, onApplyAll }}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(onApply).toHaveBeenCalledTimes(1);
    expect(onApplyAll).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Apply all" }));
    expect(onApplyAll).toHaveBeenCalledTimes(1);
    expect(onApply).toHaveBeenCalledTimes(1);
  });
});
