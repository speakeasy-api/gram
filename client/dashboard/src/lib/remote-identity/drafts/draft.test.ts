import { describe, expect, it } from "vitest";
import { reconcileDraft, seedDraft, type DraftError } from "./draft";

type Values = { name: string };

const noBlank = (values: Values): readonly DraftError[] =>
  values.name.trim() === ""
    ? [{ path: "name", message: "Name is required." }]
    : [];

describe("seedDraft", () => {
  it("starts clean, with validity judged independently", () => {
    const clean = seedDraft({ name: "linear" }, noBlank);
    expect(clean.isDirty).toBe(false);
    expect(clean.isValid).toBe(true);

    // Valid and dirty are different questions: server state can arrive
    // already invalid without anyone having touched it.
    const invalid = seedDraft({ name: "" }, noBlank);
    expect(invalid.isDirty).toBe(false);
    expect(invalid.isValid).toBe(false);
    expect(invalid.errors).toHaveLength(1);
  });
});

describe("reconcileDraft", () => {
  it("adopts fresher server state when there is nothing to lose", () => {
    const draft = seedDraft({ name: "linear" }, noBlank);
    const next = reconcileDraft(draft, { name: "renamed" }, noBlank);

    expect(next.values).toEqual({ name: "renamed" });
    expect(next.baseline).toEqual({ name: "renamed" });
    expect(next.conflict).toBe(false);
  });

  it("keeps what the operator typed, and moves the baseline anyway", () => {
    const dirty = {
      ...seedDraft({ name: "linear" }, noBlank),
      values: { name: "mine" },
      isDirty: true,
    };
    const next = reconcileDraft(dirty, { name: "theirs" }, noBlank);

    // Keeping both the values and the old baseline is the tempting version,
    // and it quietly breaks planning: the diff would be computed against a
    // world the server no longer holds.
    expect(next.values).toEqual({ name: "mine" });
    expect(next.baseline).toEqual({ name: "theirs" });
    expect(next.conflict).toBe(true);
  });
});
