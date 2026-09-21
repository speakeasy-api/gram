import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  grants: [] as Array<{
    scope: string;
    effect?: string;
    selectors: Array<Record<string, string>>;
  }>,
  mutateAsync: vi.fn(),
  complete: vi.fn(),
}));
const grant = (scope: string, resourceId: string, effect = "allow") => ({
  scope,
  effect,
  selectors: [
    {
      resourceKind: scope.startsWith("plugin:") ? "project" : "skill",
      resourceId,
    },
  ],
});
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-a" }),
  useOrganization: () => ({ id: "org-a" }),
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => ({ grants: state.grants }),
}));
vi.mock("@/routes", () => ({ useRoutes: () => ({}) }));
vi.mock("@/hooks/useDrainInfiniteQuery", () => ({
  useDrainInfiniteQuery: () => undefined,
}));
vi.mock("@gram/client/react-query/skills.js", () => ({
  useSkillsInfinite: () => ({
    isPending: false,
    hasNextPage: false,
    data: {
      pages: [
        {
          result: {
            skills: [
              {
                id: "skill-a",
                name: "first",
                displayName: "First skill",
                hasValidVersion: true,
              },
              {
                id: "skill-b",
                name: "second",
                displayName: "Second skill",
                hasValidVersion: true,
              },
            ],
          },
        },
      ],
    },
  }),
}));
vi.mock("@gram/client/react-query/distributeSkill.js", () => ({
  useDistributeSkillMutation: () => ({ mutateAsync: state.mutateAsync }),
}));
import { SkillPickerDialog } from "./SkillPickerDialog";

const picker = () => (
  <MemoryRouter>
    <SkillPickerDialog
      open
      onOpenChange={() => undefined}
      excludedSkillIds={[]}
      target={{ pluginId: "plugin-a" }}
      title="Pick skills"
      description="Select skills to distribute"
      actionLabel="Distribute"
      emptyMessage="No writable skills"
      onBatchComplete={state.complete}
    />
  </MemoryRouter>
);
afterEach(cleanup);
beforeEach(() => {
  state.grants = [grant("plugin:write", "project-a")];
  state.mutateAsync.mockReset().mockResolvedValue({});
  state.complete.mockReset();
});
describe("plugin skill picker authorization", () => {
  it("distributes readable skills with plugin write and no skill write", async () => {
    render(picker());
    expect(screen.getByText("First skill")).toBeTruthy();
    expect(screen.getByText("Second skill")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("checkbox")[0]!);
    fireEvent.click(screen.getByRole("button", { name: "Distribute" }));
    await waitFor(() =>
      expect(state.mutateAsync).toHaveBeenCalledWith({
        request: {
          distributeSkillRequestBody: { id: "skill-a", pluginId: "plugin-a" },
        },
      }),
    );
  });
  it.each(["skill:write", "skill:read"])(
    "does not grant distribution from %s",
    (scope) => {
      state.grants = [grant(scope, "project-a")];
      render(picker());
      expect(screen.queryByRole("checkbox")).toBeNull();
    },
  );
  it("rejects plugin write in another project", () => {
    state.grants = [grant("plugin:write", "project-b")];
    render(picker());
    expect(screen.queryByRole("checkbox")).toBeNull();
  });
  it("retains distribution when underlying skill edits are blocked", () => {
    state.grants.push(grant("skill:blocked_write", "skill-a"));
    render(picker());
    expect(screen.getByText("First skill")).toBeTruthy();
  });
  it("rechecks permission after selection", () => {
    const view = render(picker());
    fireEvent.click(screen.getAllByRole("checkbox")[0]!);
    state.grants.push(grant("plugin:blocked_write", "project-a"));
    view.rerender(picker());
    fireEvent.click(screen.getByRole("button", { name: "Distribute" }));
    expect(state.mutateAsync).not.toHaveBeenCalled();
  });
});
