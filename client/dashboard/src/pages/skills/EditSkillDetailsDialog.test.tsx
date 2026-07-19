import type { Skill } from "@gram/client/models/components/skill.js";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EditSkillDetailsDialog } from "./EditSkillDetailsDialog";

const testState = vi.hoisted(() => ({
  queryClient: { id: "query-client" },
  update: { mutateAsync: vi.fn(), isPending: false },
  invalidateSkills: vi.fn().mockResolvedValue(undefined),
  invalidateSkill: vi.fn().mockResolvedValue(undefined),
  invalidateDistributions: vi.fn().mockResolvedValue(undefined),
  invalidateVersions: vi.fn().mockResolvedValue(undefined),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => testState.queryClient,
}));
vi.mock("@gram/client/react-query/updateSkill.js", () => ({
  useUpdateSkillMutation: () => testState.update,
}));
vi.mock("@gram/client/react-query/skills.js", () => ({
  invalidateAllSkills: testState.invalidateSkills,
}));
vi.mock("@gram/client/react-query/skill.js", () => ({
  invalidateAllSkill: testState.invalidateSkill,
}));
vi.mock("@gram/client/react-query/skillDistributions.js", () => ({
  invalidateAllSkillDistributions: testState.invalidateDistributions,
}));
vi.mock("@gram/client/react-query/skillVersions.js", () => ({
  invalidateAllSkillVersions: testState.invalidateVersions,
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const skill = {
  id: "skill_id",
  projectId: "project_id",
  name: "captured-name",
  displayName: "Captured name",
  summary: "Captured summary",
  sourceKind: "captured",
  classification: "custom",
  versionCount: 1,
  seenCount: 2,
  createdAt: new Date("2026-01-01T00:00:00Z"),
  updatedAt: new Date("2026-01-01T00:00:00Z"),
} satisfies Skill;

beforeEach(() => {
  testState.update.isPending = false;
  testState.update.mutateAsync.mockReset().mockResolvedValue(skill);
  testState.invalidateSkills.mockClear();
  testState.invalidateSkill.mockClear();
  testState.invalidateDistributions.mockClear();
  testState.invalidateVersions.mockClear();
});

afterEach(cleanup);

describe("EditSkillDetailsDialog", () => {
  it("updates canonical and presentation metadata then invalidates skill queries", async () => {
    const onOpenChange = vi.fn();
    render(
      <EditSkillDetailsDialog
        skill={skill}
        open
        onOpenChange={(nextOpen) => {
          onOpenChange(nextOpen);
        }}
      />,
    );

    fireEvent.change(screen.getByLabelText("Canonical name"), {
      target: { value: "curated-name" },
    });
    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "Curated name" },
    });
    fireEvent.change(screen.getByLabelText("Summary"), {
      target: { value: "Curated summary" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(testState.update.mutateAsync).toHaveBeenCalledWith({
        request: {
          updateSkillRequestBody: {
            id: "skill_id",
            name: "curated-name",
            displayName: "Curated name",
            summary: "Curated summary",
          },
        },
      });
    });
    expect(testState.invalidateSkills).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateSkill).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateDistributions).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateVersions).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("counts astral characters by Unicode code point", () => {
    render(
      <EditSkillDetailsDialog skill={skill} open onOpenChange={() => {}} />,
    );

    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "😀".repeat(256) },
    });
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);

    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "😀".repeat(257) },
    });
    expect(
      screen.getByText("Display name must be 256 characters or fewer."),
    ).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("rejects canonical names over 64 characters", () => {
    render(
      <EditSkillDetailsDialog skill={skill} open onOpenChange={() => {}} />,
    );

    fireEvent.change(screen.getByLabelText("Canonical name"), {
      target: { value: "a".repeat(65) },
    });

    expect(
      screen.getByText("Canonical name must be 64 characters or fewer."),
    ).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });
});
