import type { ApproveSkillSuggestionResultOutcome } from "@gram/client/models/components/approveskillsuggestionresult.js";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SkillManifestDialog } from "./SkillManifestDialog";
import {
  MAX_SKILL_MANIFEST_BYTES,
  SKILL_MANIFEST_TEMPLATE,
} from "./skill-manifest";

const testState = vi.hoisted(() => ({
  queryClient: { id: "query-client" },
  create: {
    mutateAsync: vi.fn(),
    reset: vi.fn(),
    isPending: false,
  },
  addVersion: {
    mutateAsync: vi.fn(),
    reset: vi.fn(),
    isPending: false,
  },
  approveSuggestion: {
    mutateAsync: vi.fn(),
    reset: vi.fn(),
    isPending: false,
  },
  invalidateSkills: vi.fn().mockResolvedValue(undefined),
  invalidateSkill: vi.fn().mockResolvedValue(undefined),
  invalidateDistributions: vi.fn().mockResolvedValue(undefined),
  invalidateVersions: vi.fn().mockResolvedValue(undefined),
  invalidateSuggestions: vi.fn().mockResolvedValue(undefined),
  invalidateFeedback: vi.fn().mockResolvedValue(undefined),
  invalidateEfficacy: vi.fn().mockResolvedValue(undefined),
  setSkillParam: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => testState.queryClient,
}));
vi.mock("@gram/client/react-query/createSkill.js", () => ({
  useCreateSkillMutation: () => testState.create,
}));
vi.mock("@gram/client/react-query/addSkillVersion.js", () => ({
  useAddSkillVersionMutation: () => testState.addVersion,
}));
vi.mock("@gram/client/react-query/approveSkillSuggestion.js", () => ({
  useApproveSkillSuggestionMutation: () => testState.approveSuggestion,
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
vi.mock("@gram/client/react-query/skillSuggestions.js", () => ({
  invalidateAllSkillSuggestions: testState.invalidateSuggestions,
}));
vi.mock("@gram/client/react-query/skillFeedback.js", () => ({
  invalidateAllSkillFeedback: testState.invalidateFeedback,
}));
vi.mock("@gram/client/react-query/skillEfficacyInsights.js", () => ({
  invalidateAllSkillEfficacyInsights: testState.invalidateEfficacy,
}));
vi.mock("@gram/client/react-query/skillTags.js", () => ({
  invalidateAllSkillTags: vi.fn().mockResolvedValue(undefined),
}));
vi.mock("nuqs", () => ({
  useQueryState: () => [null, testState.setSkillParam],
}));

const VALID_MANIFEST =
  "---\nname: example\ndescription: Example skill.\n---\n# Example";
const UPDATED_MANIFEST =
  "---\nname: example\ndescription: Updated example skill.\n---\n# Updated";
const INVALID_MANIFEST =
  "---\nname: Example\ndescription: Example skill.\n---\n# Example";

const validResult = {
  createdSkill: true,
  createdVersion: true,
  skill: { id: "skill_result" },
  version: {
    id: "version_result",
    content: VALID_MANIFEST,
    description: "Example skill.",
    specValid: true,
    validationErrors: [],
  },
};

function ApprovalHarness(): JSX.Element {
  const [open, setOpen] = useState(true);
  return (
    <>
      <button onClick={() => setOpen(true)}>Reopen approval</button>
      <SkillManifestDialog
        mode="approve-suggestion"
        open={open}
        onOpenChange={setOpen}
        suggestionId="suggestion_a"
        initialContent={UPDATED_MANIFEST}
      />
    </>
  );
}

beforeEach(() => {
  testState.create.isPending = false;
  testState.addVersion.isPending = false;
  testState.create.mutateAsync.mockReset();
  testState.create.reset.mockReset();
  testState.addVersion.mutateAsync.mockReset();
  testState.addVersion.reset.mockReset();
  testState.approveSuggestion.isPending = false;
  testState.approveSuggestion.mutateAsync.mockReset();
  testState.approveSuggestion.reset.mockReset();
  testState.invalidateSkills.mockReset().mockResolvedValue(undefined);
  testState.invalidateSkill.mockReset().mockResolvedValue(undefined);
  testState.invalidateDistributions.mockReset().mockResolvedValue(undefined);
  testState.invalidateVersions.mockReset().mockResolvedValue(undefined);
  testState.invalidateSuggestions.mockReset().mockResolvedValue(undefined);
  testState.invalidateFeedback.mockReset().mockResolvedValue(undefined);
  testState.invalidateEfficacy.mockReset().mockResolvedValue(undefined);
  testState.setSkillParam.mockReset();
});

afterEach(cleanup);

describe("SkillManifestDialog", () => {
  it("uses the exact create wrapper and broadly invalidates before navigating by result skill ID", async () => {
    testState.create.mutateAsync.mockResolvedValue(validResult);
    render(<SkillManifestDialog mode="create" open onOpenChange={() => {}} />);
    fireEvent.change(screen.getByLabelText("SKILL.md content"), {
      target: { value: VALID_MANIFEST },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add skill" }));

    await waitFor(() => {
      expect(testState.create.mutateAsync).toHaveBeenCalledWith({
        request: {
          createSkillRequestBody: {
            content: VALID_MANIFEST,
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
    expect(testState.invalidateSuggestions).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateFeedback).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(testState.invalidateEfficacy).toHaveBeenCalledWith(
      testState.queryClient,
    );
    await waitFor(() => {
      expect(testState.setSkillParam).toHaveBeenCalledWith("skill_result");
    });
  });

  it("uses the exact add-version wrapper", async () => {
    testState.addVersion.mutateAsync.mockResolvedValue(validResult);
    render(
      <SkillManifestDialog
        mode="edit"
        open
        onOpenChange={() => {}}
        skillId="skill_a"
        derivedFromVersionId="version_source"
        initialContent={UPDATED_MANIFEST}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Save new version" }));

    await waitFor(() => {
      expect(testState.addVersion.mutateAsync).toHaveBeenCalledWith({
        request: {
          addSkillVersionRequestBody: {
            id: "skill_a",
            content: UPDATED_MANIFEST,
            derivedFromVersionId: "version_source",
          },
        },
      });
    });
  });

  it("submits edited suggestion content through approve instead of add version", async () => {
    const onSuggestionApproved =
      vi.fn<(outcome: ApproveSkillSuggestionResultOutcome) => void>();
    testState.approveSuggestion.mutateAsync.mockResolvedValue({
      outcome: "applied",
    });
    render(
      <SkillManifestDialog
        mode="approve-suggestion"
        open
        onOpenChange={() => {}}
        suggestionId="suggestion_a"
        initialContent={UPDATED_MANIFEST}
        onSuggestionApproved={onSuggestionApproved}
      />,
    );
    expect(
      screen.getByText(/unless the skill changed, in which case/),
    ).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Approve edited suggestion" }),
    );

    await waitFor(() => {
      expect(testState.approveSuggestion.mutateAsync).toHaveBeenCalledWith({
        request: {
          approveSkillSuggestionRequestBody: {
            id: "suggestion_a",
            content: UPDATED_MANIFEST,
          },
        },
      });
    });
    expect(testState.addVersion.mutateAsync).not.toHaveBeenCalled();
    expect(onSuggestionApproved).toHaveBeenCalledWith("applied");
    expect(testState.invalidateSuggestions).toHaveBeenCalledWith(
      testState.queryClient,
    );
  });

  it("invalidates and warns when edited approval transport fails", async () => {
    testState.approveSuggestion.mutateAsync.mockRejectedValue(
      new Error("connection lost"),
    );
    render(<ApprovalHarness />);
    fireEvent.click(
      screen.getByRole("button", { name: "Approve edited suggestion" }),
    );

    expect(
      await screen.findByText(
        /Approval status may be unknown. Review the refreshed state before retrying/,
      ),
    ).toBeTruthy();
    expect(testState.invalidateSuggestions).toHaveBeenCalledWith(
      testState.queryClient,
    );
    expect(
      screen
        .getByRole("button", { name: "Approve edited suggestion" })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.click(
      screen.getByRole("button", { name: "Approve edited suggestion" }),
    );
    expect(testState.approveSuggestion.mutateAsync).toHaveBeenCalledOnce();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(screen.getByRole("button", { name: "Reopen approval" }));
    expect(
      screen
        .getByRole("button", { name: "Approve edited suggestion" })
        .hasAttribute("disabled"),
    ).toBe(false);
    expect(screen.queryByText(/Approval status may be unknown/)).toBeNull();
  });

  it("disables edited approval controls until cache reconciliation finishes", async () => {
    const onOpenChange = vi.fn<(open: boolean) => void>();
    testState.approveSuggestion.mutateAsync.mockResolvedValue({
      outcome: "applied",
    });
    let finishInvalidation!: () => void;
    testState.invalidateSuggestions.mockReturnValueOnce(
      new Promise<void>((resolve) => {
        finishInvalidation = resolve;
      }),
    );
    render(
      <SkillManifestDialog
        mode="approve-suggestion"
        open
        onOpenChange={onOpenChange}
        suggestionId="suggestion_a"
        initialContent={UPDATED_MANIFEST}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Approve edited suggestion" }),
    );
    await waitFor(() =>
      expect(testState.invalidateSuggestions).toHaveBeenCalled(),
    );
    expect(
      screen
        .getByRole("button", { name: "Saving..." })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(
      screen.getByRole("button", { name: "Cancel" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(onOpenChange).not.toHaveBeenCalled();

    await act(async () => finishInvalidation());
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it("keeps a persisted invalid version in the dialog and disables repeat submission", async () => {
    testState.addVersion.mutateAsync.mockResolvedValue(validResult);
    testState.create.mutateAsync.mockResolvedValue({
      ...validResult,
      version: {
        id: "version_result",
        content: INVALID_MANIFEST,
        description: "Example skill.",
        specValid: false,
        validationErrors: [
          { code: "invalid-name", field: "name", message: "Use lowercase." },
        ],
      },
    });
    render(<SkillManifestDialog mode="create" open onOpenChange={() => {}} />);
    fireEvent.change(screen.getByLabelText("SKILL.md content"), {
      target: { value: INVALID_MANIFEST },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add skill" }));

    expect(
      await screen.findByText("Saved with validation issues."),
    ).toBeTruthy();
    expect(screen.getByText(/Use lowercase/)).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "Add version" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(screen.getByRole("button", { name: "View skill" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Continue editing" }));
    expect(
      screen.queryByText(/saving again will create another version/i),
    ).toBeNull();
    expect(screen.getByText(/Future saves add versions/i)).toBeTruthy();
    fireEvent.change(screen.getByLabelText("SKILL.md content"), {
      target: { value: UPDATED_MANIFEST },
    });
    expect(
      screen
        .getByRole("button", { name: "Add version" })
        .hasAttribute("disabled"),
    ).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Add version" }));
    await waitFor(() => {
      expect(testState.addVersion.mutateAsync).toHaveBeenCalledWith({
        request: {
          addSkillVersionRequestBody: {
            id: "skill_result",
            content: UPDATED_MANIFEST,
            derivedFromVersionId: "version_result",
          },
        },
      });
    });
    expect(testState.create.mutateAsync).toHaveBeenCalledTimes(1);
  });

  it("skips invalidation for a no-op and requires an edit before adding a version", async () => {
    testState.addVersion.mutateAsync.mockResolvedValue(validResult);
    testState.create.mutateAsync.mockResolvedValue({
      ...validResult,
      createdSkill: false,
      createdVersion: false,
    });
    render(<SkillManifestDialog mode="create" open onOpenChange={() => {}} />);
    fireEvent.change(screen.getByLabelText("SKILL.md content"), {
      target: { value: VALID_MANIFEST },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add skill" }));

    expect(await screen.findByText("No changes detected.")).toBeTruthy();
    expect(testState.setSkillParam).not.toHaveBeenCalled();
    expect(testState.invalidateSkills).not.toHaveBeenCalled();
    expect(testState.invalidateSkill).not.toHaveBeenCalled();
    expect(testState.invalidateVersions).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Continue editing" }));
    expect(
      screen
        .getByRole("button", { name: "Add version" })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.change(screen.getByLabelText("SKILL.md content"), {
      target: { value: UPDATED_MANIFEST },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add version" }));
    await waitFor(() => {
      expect(testState.addVersion.mutateAsync).toHaveBeenCalledWith({
        request: {
          addSkillVersionRequestBody: {
            id: "skill_result",
            content: UPDATED_MANIFEST,
            derivedFromVersionId: "version_result",
          },
        },
      });
    });
    expect(testState.create.mutateAsync).toHaveBeenCalledTimes(1);
  });

  it("prefills the create dialog with a SKILL.md skeleton", () => {
    render(<SkillManifestDialog mode="create" open onOpenChange={() => {}} />);

    const textarea = screen.getByLabelText("SKILL.md content");
    expect((textarea as HTMLTextAreaElement).value).toBe("");

    fireEvent.click(screen.getByRole("button", { name: "Insert template" }));

    expect((textarea as HTMLTextAreaElement).value).toBe(SKILL_MANIFEST_TEMPLATE);
  });

  it("hides the template button outside create mode", () => {
    render(
      <SkillManifestDialog
        mode="edit"
        open
        onOpenChange={() => {}}
        skillId="skill_a"
        derivedFromVersionId="version_source"
        initialContent={UPDATED_MANIFEST}
      />,
    );

    expect(
      screen.queryByRole("button", { name: "Insert template" }),
    ).toBeNull();
  });

  it("rejects an oversized upload before reading it", async () => {
    const file = new File(
      [new Uint8Array(MAX_SKILL_MANIFEST_BYTES + 1)],
      "too-large.md",
      { type: "text/markdown" },
    );
    const read = vi.spyOn(file, "arrayBuffer");
    render(<SkillManifestDialog mode="create" open onOpenChange={() => {}} />);

    fireEvent.change(screen.getByLabelText("Upload .md file"), {
      target: { files: [file] },
    });

    expect(await screen.findByText(/65,536 bytes or fewer/)).toBeTruthy();
    expect(read).not.toHaveBeenCalled();
  });

  it("does not close while a save is in flight", () => {
    const onOpenChange = vi.fn((_open: boolean): void => {});
    testState.create.isPending = true;
    render(
      <SkillManifestDialog mode="create" open onOpenChange={onOpenChange} />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onOpenChange).not.toHaveBeenCalled();
    expect(
      screen
        .getByRole("button", { name: "Insert template" })
        .hasAttribute("disabled"),
    ).toBe(true);
  });
});
