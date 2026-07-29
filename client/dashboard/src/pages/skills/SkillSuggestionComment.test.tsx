import type { SkillEditSuggestionChange } from "@gram/client/models/components/skilleditsuggestionchange.js";
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

function change(
  overrides: Partial<SkillEditSuggestionChange> = {},
): SkillEditSuggestionChange {
  return {
    id: "change_a",
    suggestionId: "suggestion_a",
    proposedDiff: "",
    rationale: "Leadership reviews stall without a number here.",
    appliesCleanly: true,
    feedbackCount: 11,
    feedbackSessionCount: 11,
    createdAt: new Date("2026-07-01T00:00:00Z"),
    ...overrides,
  } as SkillEditSuggestionChange;
}

const actions = {
  disabled: false,
  approving: false,
  onApply: vi.fn(),
};

describe("SkillSuggestionComment", () => {
  afterEach(cleanup);
  beforeEach(() => {
    testState.feedback = [];
    testState.enabled = false;
  });

  it("leads with the session count the suggestion was built from", () => {
    render(<SkillSuggestionComment change={change()} actions={actions} />);

    expect(screen.getByText(/Requested in 11 sessions\./)).toBeTruthy();
    expect(
      screen.getByText(/Leadership reviews stall without a number here\./),
    ).toBeTruthy();
  });

  it("omits the session count when no session reported the feedback", () => {
    render(
      <SkillSuggestionComment
        change={change({ feedbackSessionCount: 0 })}
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
    render(<SkillSuggestionComment change={change()} actions={actions} />);

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
        change={change({ feedbackCount: 0 })}
        actions={actions}
      />,
    );

    expect(screen.queryByText(/agent report/)).toBeNull();
  });
});

describe("SkillSuggestionComment actions", () => {
  afterEach(cleanup);

  it("applies only the change the comment is attached to", () => {
    const onApply = vi.fn<() => void>();
    render(
      <SkillSuggestionComment
        change={change()}
        actions={{ ...actions, onApply }}
      />,
    );

    expect(screen.queryByRole("button", { name: "Apply all" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(onApply).toHaveBeenCalledTimes(1);
  });
});
