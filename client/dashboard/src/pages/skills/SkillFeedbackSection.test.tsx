import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SkillFeedbackSection } from "./SkillFeedbackSection";

const testState = vi.hoisted(() => ({
  error: null as Error | null,
  enabled: true,
  refetch: vi.fn(),
  fetchNextPage: vi.fn(),
  hasNextPage: false,
  trigger: vi.fn(),
  triggerError: null as Error | null,
  feedback: [
    {
      id: "feedback_a",
      outcome: "partially_helped",
      note: "Clarify the final verification step.",
      createdAt: new Date("2026-07-20T00:00:00Z"),
      source: "agent",
    },
  ] as Array<Record<string, unknown>>,
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@gram/client/react-query/skillFeedback.js", () => ({
  useSkillFeedbackInfinite: (
    _request: unknown,
    _security: unknown,
    options: { enabled: boolean },
  ) => {
    testState.enabled = options.enabled;
    return {
      isPending: false,
      error: testState.error,
      refetch: testState.refetch,
      hasNextPage: testState.hasNextPage,
      isFetchingNextPage: false,
      fetchNextPage: testState.fetchNextPage,
      data: testState.error
        ? undefined
        : {
            pages: [
              {
                result: {
                  counts: {
                    total: 15,
                    helped: 5,
                    partiallyHelped: 4,
                    didNotHelp: 3,
                    misleading: 2,
                    harmful: 1,
                  },
                  metrics: {
                    feedbackInWindow: 6,
                    activationsInWindow: 24,
                    feedbackActivationsInWindow: 6,
                    unreviewed: 2,
                    converted: 3,
                    windowStart: new Date("2026-06-20T00:00:00Z"),
                    windowEnd: new Date("2026-07-20T00:00:00Z"),
                  },
                  timeline: Array.from({ length: 30 }, (_, index) => ({
                    bucketStart: new Date(
                      Date.UTC(2026, 5, 21 + index, 0, 0, 0),
                    ),
                    feedbackCount: index === 29 ? 6 : 0,
                  })),
                  feedback: testState.feedback,
                },
              },
            ],
          },
    };
  },
}));

vi.mock("@gram/client/react-query/triggerSkillSuggestion.js", () => ({
  useTriggerSkillSuggestionMutation: () => ({
    mutateAsync: testState.trigger,
    isPending: false,
    error: testState.triggerError,
  }),
}));

beforeEach(() => {
  testState.error = null;
  testState.enabled = true;
  testState.refetch.mockReset();
  testState.fetchNextPage.mockReset();
  testState.hasNextPage = false;
  testState.trigger.mockReset();
  testState.trigger.mockResolvedValue(undefined);
  testState.triggerError = null;
  testState.feedback = [
    {
      id: "feedback_a",
      outcome: "partially_helped",
      note: "Clarify the final verification step.",
      createdAt: new Date("2026-07-20T00:00:00Z"),
      source: "agent",
    },
  ];
});

afterEach(cleanup);

describe("SkillFeedbackSection", () => {
  it("starts collapsed, explains the pool, then shows counts and notes", () => {
    render(<SkillFeedbackSection skillId="skill_a" projectId="project_a" />);
    expect(testState.enabled).toBe(false);
    expect(screen.getByText(/See collection health/)).toBeTruthy();
    expect(screen.queryByText("Outcome distribution")).toBeNull();

    const trigger = screen.getByRole("button", { name: /All agent reviews/ });
    expect(trigger.querySelector("p")).toBeNull();
    fireEvent.click(trigger);
    expect(testState.enabled).toBe(true);
    expect(screen.getByText("Outcome distribution")).toBeTruthy();
    expect(screen.getByText("Partially helped")).toBeTruthy();
    expect(screen.getByText("25.0%")).toBeTruthy();
    expect(screen.getByText("20.0%")).toBeTruthy();
    expect(
      screen.getAllByText("Clarify the final verification step."),
    ).toHaveLength(2);
  });

  it("shows an error and retries", () => {
    testState.error = new Error("feedback unavailable");
    render(<SkillFeedbackSection skillId="skill_a" projectId="project_a" />);
    fireEvent.click(screen.getByRole("button", { name: /All agent reviews/ }));
    expect(screen.getByText("feedback unavailable")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.refetch).toHaveBeenCalledOnce();
  });

  it("describes an empty recent page without implying all feedback was searched", () => {
    testState.feedback = [];
    render(<SkillFeedbackSection skillId="skill_a" projectId="project_a" />);
    fireEvent.click(screen.getByRole("button", { name: /All agent reviews/ }));

    expect(screen.getByText("No notes among recent feedback.")).toBeTruthy();
  });

  it("groups wording-level note variants", () => {
    testState.feedback = [
      ...testState.feedback,
      {
        id: "feedback_b",
        outcome: "did_not_help",
        note: "clarify the final verification step!",
        createdAt: new Date("2026-07-19T00:00:00Z"),
        source: "dev",
      },
      {
        id: "feedback_c",
        outcome: "helped",
        note: "The final verification step needs clarification.",
        createdAt: new Date("2026-07-18T00:00:00Z"),
        source: "agent",
      },
      {
        id: "feedback_d",
        outcome: "helped",
        note: "Final verification step needs clarification details.",
        createdAt: new Date("2026-07-17T00:00:00Z"),
        source: "dev",
      },
    ];
    const { container } = render(
      <SkillFeedbackSection skillId="skill_a" projectId="project_a" />,
    );
    fireEvent.click(screen.getByRole("button", { name: /All agent reviews/ }));

    expect(container.querySelectorAll("details")).toHaveLength(1);
  });

  it("can load older reviews when the current page has no notes", () => {
    testState.feedback = [
      {
        id: "feedback_without_note",
        outcome: "helped",
        createdAt: new Date("2026-07-20T00:00:00Z"),
        source: "agent",
      },
    ];
    testState.hasNextPage = true;
    render(<SkillFeedbackSection skillId="skill_a" projectId="project_a" />);
    fireEvent.click(screen.getByRole("button", { name: /All agent reviews/ }));

    fireEvent.click(screen.getByRole("button", { name: "Load more reviews" }));
    expect(testState.fetchNextPage).toHaveBeenCalledOnce();
  });

  it("queues a manual suggestion run", async () => {
    render(<SkillFeedbackSection skillId="skill_a" projectId="project_a" />);
    fireEvent.click(screen.getByRole("button", { name: /All agent reviews/ }));
    fireEvent.click(
      screen.getByRole("button", { name: "Generate suggestion" }),
    );

    await vi.waitFor(() => {
      expect(testState.trigger).toHaveBeenCalledWith({
        request: {
          triggerSkillSuggestionRequestBody: { id: "skill_a" },
        },
      });
    });
  });
});
