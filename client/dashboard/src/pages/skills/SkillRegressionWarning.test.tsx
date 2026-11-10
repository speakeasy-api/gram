import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RegressionWarning } from "./SkillInsightsSection";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

afterEach(cleanup);

describe("RegressionWarning", () => {
  it("shows server score context and links to version history", () => {
    render(
      <RegressionWarning
        skillId="skill_a"
        signal={{
          comparable: true,
          regression: true,
          currentAverageScore: 0.45,
          currentScoredSessions: 12,
          currentVersionId: "version_current",
          predecessorAverageScore: 0.8,
          predecessorScoredSessions: 20,
          predecessorVersionId: "version_previous",
          windowStart: new Date("2026-07-01T00:00:00Z"),
          windowEnd: new Date("2026-07-20T00:00:00Z"),
        }}
      />,
      { wrapper: MemoryRouter },
    );

    expect(
      screen.getByText(/Current: 45.0% across 12 scored sessions/),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Review version to restore" })
        .getAttribute("href"),
    ).toBe("/org/projects/project/skills/skill_a/versions/version_previous");
  });
});
