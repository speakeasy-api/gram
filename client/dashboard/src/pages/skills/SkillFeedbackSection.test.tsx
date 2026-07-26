import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SkillFeedbackSection } from "./SkillFeedbackSection";

const testState = vi.hoisted(() => ({
  error: null as Error | null,
  enabled: true,
  refetch: vi.fn(),
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

vi.mock("@gram/client/react-query/skillFeedback.js", () => ({
  useSkillFeedback: (
    _request: unknown,
    _security: unknown,
    options: { enabled: boolean },
  ) => {
    testState.enabled = options.enabled;
    return {
      isPending: false,
      error: testState.error,
      refetch: testState.refetch,
      data: testState.error
        ? undefined
        : {
            result: {
              counts: {
                total: 15,
                helped: 5,
                partiallyHelped: 4,
                didNotHelp: 3,
                misleading: 2,
                harmful: 1,
              },
              feedback: testState.feedback,
            },
          },
    };
  },
}));

beforeEach(() => {
  testState.error = null;
  testState.enabled = true;
  testState.refetch.mockReset();
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
  it("starts collapsed, explains raw signals, then shows counts and notes", () => {
    render(<SkillFeedbackSection skillId="skill_a" />);
    expect(testState.enabled).toBe(false);
    expect(
      screen.getByText(/Raw agent-reported signals used as analysis input/),
    ).toBeTruthy();
    expect(screen.queryByText("Helped: 5")).toBeNull();

    const trigger = screen.getByRole("button", { name: /Agent feedback log/ });
    expect(trigger.querySelector("p")).toBeNull();
    fireEvent.click(trigger);
    expect(testState.enabled).toBe(true);
    expect(
      screen.getByText((_, element) => element?.textContent === "Helped: 5"),
    ).toBeTruthy();
    expect(
      screen.getByText(
        (_, element) => element?.textContent === "Partially helped: 4",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("Clarify the final verification step."),
    ).toBeTruthy();
  });

  it("shows an error and retries", () => {
    testState.error = new Error("feedback unavailable");
    render(<SkillFeedbackSection skillId="skill_a" />);
    fireEvent.click(screen.getByRole("button", { name: /Agent feedback log/ }));
    expect(screen.getByText("feedback unavailable")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(testState.refetch).toHaveBeenCalledOnce();
  });

  it("describes an empty recent page without implying all feedback was searched", () => {
    testState.feedback = [];
    render(<SkillFeedbackSection skillId="skill_a" />);
    fireEvent.click(screen.getByRole("button", { name: /Agent feedback log/ }));

    expect(screen.getByText("No notes among recent feedback.")).toBeTruthy();
  });
});
