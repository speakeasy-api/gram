import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SkillDetailRoot from "./SkillDetailRoot";

const testState = vi.hoisted(() => ({
  pathname: "/skills/skill_a",
}));

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
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    skills: {
      Link: ({ children }: { children: ReactNode }) => <>{children}</>,
      detail: {
        href: (id: string) => `/skills/${id}`,
      },
    },
  }),
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
    useLocation: () => testState,
    useParams: () => ({ skillId: "skill_a" }),
  };
});

beforeEach(() => {
  testState.pathname = "/skills/skill_a";
});

afterEach(cleanup);

describe("SkillDetailRoot", () => {
  it("redirects the base route to overview", () => {
    render(<SkillDetailRoot />);
    expect(screen.getByTestId("navigate").textContent).toBe(
      "/skills/skill_a/overview",
    );
  });
});
