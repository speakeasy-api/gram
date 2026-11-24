// @vitest-environment node
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

const OPUS_LATEST = "~anthropic/claude-opus-latest";

describe("model defaults", () => {
  it("supports Claude Opus Latest across dashboard and Elements", () => {
    expect(AVAILABLE_MODELS).toContainEqual({
      value: OPUS_LATEST,
      label: "Claude Opus Latest",
      expensive: true,
    });
    expect(MODELS).toContain(OPUS_LATEST);
  });

  it("defaults in-app chat and new assistants to Claude Opus Latest", () => {
    expect(DEFAULT_MODEL).toBe(OPUS_LATEST);
    expect(ELEMENTS_DEFAULT_MODEL).toBe(OPUS_LATEST);
    expect(DEFAULT_ASSISTANT_MODEL).toBe(OPUS_LATEST);
  });
});
