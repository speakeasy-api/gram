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
  selectors: [{ resourceKind: "skill", resourceId }],
});
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ id: "project-a" }) }));
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
  state.grants = [grant("skill:write", "skill-a")];
  state.mutateAsync.mockReset().mockResolvedValue({});
  state.complete.mockReset();
});
describe("skill picker resource authorization", () => {
  it("lists and distributes the concrete writable skill, not another skill", async () => {
    render(picker());
    expect(screen.getByText("First skill")).toBeTruthy();
    expect(screen.queryByText("Second skill")).toBeNull();
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "Distribute" }));
    await waitFor(() =>
      expect(state.mutateAsync).toHaveBeenCalledWith({
        request: {
          distributeSkillRequestBody: { id: "skill-a", pluginId: "plugin-a" },
        },
      }),
    );
    await waitFor(() =>
      expect(state.complete).toHaveBeenCalledWith({
        addedCount: 1,
        failedCount: 0,
      }),
    );
  });
  it("accepts a project resource-ID grant for listing and submission", async () => {
    state.grants = [grant("skill:write", "project-a")];
    render(picker());
    expect(screen.getByText("First skill")).toBeTruthy();
    expect(screen.getByText("Second skill")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("checkbox")[0]!);
    fireEvent.click(screen.getByRole("button", { name: "Distribute" }));
    await waitFor(() => expect(state.mutateAsync).toHaveBeenCalled());
  });
  it.each(["skill:blocked_read", "skill:blocked_write", "legacy deny"])(
    "does not bypass %s on a skill with a project allow",
    (scope) => {
      state.grants = [
        grant("skill:write", "project-a"),
        grant(
          scope === "legacy deny" ? "skill:write" : scope,
          "skill-a",
          scope === "legacy deny" ? "deny" : "allow",
        ),
      ];
      render(picker());
      expect(screen.queryByText("First skill")).toBeNull();
      expect(screen.getByText("Second skill")).toBeTruthy();
    },
  );
  it("does not bypass a project exclusion with a concrete skill allow", () => {
    state.grants = [
      grant("skill:write", "skill-a"),
      grant("skill:blocked_write", "project-a"),
    ];
    render(picker());
    expect(screen.queryByRole("checkbox")).toBeNull();
  });
  it("rejects grants for another project", () => {
    state.grants = [grant("skill:write", "project-b")];
    render(picker());
    expect(screen.queryByRole("checkbox")).toBeNull();
  });
  it("rechecks exclusions added after selection despite a project allow", () => {
    state.grants = [grant("skill:write", "project-a")];
    const view = render(picker());
    fireEvent.click(screen.getAllByRole("checkbox")[0]!);
    state.grants = [...state.grants, grant("skill:blocked_read", "skill-a")];
    view.rerender(picker());
    fireEvent.click(screen.getByRole("button", { name: "Distribute" }));
    expect(state.mutateAsync).not.toHaveBeenCalled();
  });
  it("rechecks the selected skill before submitting after its write grant changes", () => {
    const view = render(picker());
    fireEvent.click(screen.getByRole("checkbox"));
    state.grants = [grant("skill:write", "skill-b")];
    view.rerender(picker());
    fireEvent.click(screen.getByRole("button", { name: "Distribute" }));
    expect(state.mutateAsync).not.toHaveBeenCalled();
    expect(state.complete).not.toHaveBeenCalled();
  });
});
