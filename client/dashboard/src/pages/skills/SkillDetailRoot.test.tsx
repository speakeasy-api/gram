import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import SkillDetailRoot from "./SkillDetailRoot";

vi.mock("@/components/page-layout", () => ({
  Page: () => null,
}));
vi.mock("@gram/client/react-query/skill.js", () => ({
  useSkill: () => ({
    data: {
      skill: { id: "skill_a", displayName: "Example", versionCount: 0 },
    },
    error: null,
    isPending: false,
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));
vi.mock("react-router", async () => {
  const actual =
    await vi.importActual<typeof import("react-router")>("react-router");
  return {
    ...actual,
    Navigate: ({ to }: { to: string }) => (
      <div data-testid="navigate">{to}</div>
    ),
    Outlet: () => null,
  };
});

afterEach(cleanup);

describe("SkillDetailRoot", () => {
  it("redirects the base route to overview", () => {
    render(
      <MemoryRouter initialEntries={["/org/projects/project/skills/skill_a"]}>
        <Routes>
          <Route
            path="/org/projects/project/skills/:skillId"
            element={<SkillDetailRoot />}
          />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByTestId("navigate").textContent).toBe(
      "/org/projects/project/skills/skill_a/overview",
    );
  });
});
