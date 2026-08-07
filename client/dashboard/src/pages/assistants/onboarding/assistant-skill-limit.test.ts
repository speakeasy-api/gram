import { describe, expect, it } from "vitest";
import { shouldWarnAboutSkillIndex } from "./assistant-skill-limit";

describe("assistant skill soft cap", () => {
  it("warns only above 20 attached skills", () => {
    expect(shouldWarnAboutSkillIndex(20)).toBe(false);
    expect(shouldWarnAboutSkillIndex(21)).toBe(true);
  });
});
