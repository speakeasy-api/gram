import { describe, expect, it } from "vitest";
import {
  DEFAULT_MODEL as ELEMENTS_DEFAULT_MODEL,
  MODELS,
} from "@/elements/lib/models";
import {
  AVAILABLE_MODELS,
  DEFAULT_ASSISTANT_MODEL,
  DEFAULT_MODEL,
} from "./models";

const OPUS_5 = "anthropic/claude-opus-5";

describe("model defaults", () => {
  it("supports Claude Opus 5 across dashboard and Elements", () => {
    expect(AVAILABLE_MODELS).toContainEqual({
      value: OPUS_5,
      label: "Claude Opus 5",
      expensive: true,
    });
    expect(MODELS).toContain(OPUS_5);
  });

  it("defaults in-app chat and new assistants to Claude Opus 5", () => {
    expect(DEFAULT_MODEL).toBe(OPUS_5);
    expect(ELEMENTS_DEFAULT_MODEL).toBe(OPUS_5);
    expect(DEFAULT_ASSISTANT_MODEL).toBe(OPUS_5);
  });
});
