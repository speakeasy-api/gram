import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RegressionWarning } from "./SkillInsightsSection";

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: { detail: { href: (id: string) => `/skills/${id}` } },
  }),
}));
vi.mock("react-router", () => ({
  Link: ({ to, children }: { to: string; children: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

afterEach(cleanup);

describe("RegressionWarning", () => {
  it("shows server score context and deep-links the predecessor restore action", () => {
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
    );

    expect(
      screen.getByText(/Current: 45.0% across 12 scored sessions/),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Review version to restore" })
        .getAttribute("href"),
    ).toBe("/skills/skill_a#version-version_previous");
  });
});
