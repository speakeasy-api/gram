import type { SkillEditSuggestion } from "@gram/client/models/components/skilleditsuggestion.js";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { SkillSuggestionReview } from "./use-skill-suggestion";
import { SuggestedSkillEditSection } from "./SuggestedSkillEditSection";

const testState = vi.hoisted(() => ({
  review: null as SkillSuggestionReview | null,
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project_a" }),
}));

vi.mock("./use-skill-suggestion", () => ({
  useSkillSuggestionReview: () => testState.review,
}));

vi.mock("./SkillManifestDialog", () => ({
  SkillManifestDialog: () => null,
}));

vi.mock("./SkillTextDiff", () => ({
  default: ({
    oldContent,
    newContent,
    oldLabel,
  }: {
    oldContent: string;
    newContent: string;
    oldLabel: string;
  }) => (
    <div data-testid="diff">
      <span data-testid="diff-old-label">{oldLabel}</span>
      <pre data-testid="diff-old">{oldContent}</pre>
      <pre data-testid="diff-new">{newContent}</pre>
    </div>
  ),
}));

const baseContent = "---\nname: shepherd\n---\n\nKeep CI green.\n";
const proposedContent = "---\nname: shepherd\n---\n\nKeep CI green, always.\n";

function version(id: string, content: string): SkillVersion {
  return {
    id,
    skillId: "skill_a",
    content,
    canonicalSha256: id.padEnd(64, "0"),
    rawSha256: id.padEnd(64, "1"),
    createdAt: new Date("2026-10-04T00:00:00.000Z"),
    createdByUserId: "user_a",
    metadata: {},
    frontmatter: {},
    specValid: true,
    validationErrors: [],
    seenCount: 0,
    captured: false,
  };
}

function suggestion(baseVersionId: string): SkillEditSuggestion {
  return {
    id: "suggestion_a",
    skillId: "skill_a",
    skillName: "shepherd",
    skillDisplayName: "Shepherd",
    baseVersionId,
    baseContent,
    changes: [
      {
        id: "change_a",
        suggestionId: "suggestion_a",
        proposedDiff:
          "--- a/SKILL.md\n+++ b/SKILL.md\n@@ -5 +5 @@\n-Keep CI green.\n+Keep CI green, always.\n",
        rationale: "Agents stopped early.",
        appliesCleanly: true,
        feedbackCount: 1,
        feedbackSessionCount: 1,
        createdAt: new Date("2026-10-05T00:00:00.000Z"),
      },
    ],
    proposedContent,
    appliesCleanly: true,
    rationale: "Agents stopped early.",
    status: "open",
    feedbackCount: 1,
    feedbackSessionCount: 1,
    scoredSessionCount: 1,
    createdAt: new Date("2026-10-05T00:00:00.000Z"),
    updatedAt: new Date("2026-10-05T00:00:00.000Z"),
  };
}

function review(
  loaded: SkillEditSuggestion,
  dismiss = vi.fn<() => Promise<void>>(),
): SkillSuggestionReview {
  return {
    suggestion: loaded,
    isPending: false,
    loadError: null,
    actionError: null,
    uncertain: false,
    actionsDisabled: false,
    approving: false,
    dismissing: false,
    approve: vi.fn<(changeIds?: string[]) => Promise<void>>(),
    dismiss,
    refresh: vi.fn<() => Promise<void>>(),
    retry: vi.fn<() => void>(),
    hide: vi.fn<() => void>(),
  };
}

afterEach(() => {
  cleanup();
  testState.review = null;
});

describe("SuggestedSkillEditSection", () => {
  it("diffs the proposal against its base version rather than the latest version", async () => {
    testState.review = review(suggestion("version_base"));

    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={version("version_other", "a full rewrite")}
      />,
    );

    expect((await screen.findByTestId("diff-old")).textContent).toBe(
      baseContent,
    );
    expect(screen.getByTestId("diff-new").textContent).toBe(proposedContent);
    expect(screen.getByTestId("diff-old-label").textContent).toBe(
      "Shipped SKILL.md",
    );
  });

  it("labels the base as current when it is the current version", async () => {
    testState.review = review(suggestion("version_base"));

    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={version("version_base", baseContent)}
      />,
    );

    expect((await screen.findByTestId("diff-old-label")).textContent).toBe(
      "Current SKILL.md",
    );
  });

  it("offers dismiss for a single-change suggestion", () => {
    const dismiss = vi.fn<() => Promise<void>>();
    testState.review = review(suggestion("version_base"), dismiss);

    render(
      <SuggestedSkillEditSection
        skillId="skill_a"
        latestVersion={version("version_base", baseContent)}
      />,
    );

    expect(screen.getByText("1 change proposed")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Apply all" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(dismiss).toHaveBeenCalledOnce();
  });
});
