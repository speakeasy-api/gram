import { describe, expect, it } from "vitest";
import {
  DEFAULT_MODEL as ELEMENTS_DEFAULT_MODEL,
  MODELS,
} from "@/elements/lib/models";
import {
  AVAILABLE_MODELS,
  DEFAULT_ASSISTANT_MODEL,
  DEFAULT_MODEL,
  PLAYGROUND_MODEL,
} from "./models";

const OPUS_5 = "anthropic/claude-opus-5";
const GEMINI_FLASH = "google/gemini-3.5-flash";

describe("model defaults", () => {
  it("supports Claude Opus 5 across dashboard and Elements", () => {
    expect(AVAILABLE_MODELS).toContainEqual({
      value: OPUS_5,
      label: "Claude Opus 5",
      expensive: true,
    });
    expect(MODELS).toContain(OPUS_5);
  });

  it("defaults in-app chat to Claude Opus 5", () => {
    expect(DEFAULT_MODEL).toBe(OPUS_5);
    expect(ELEMENTS_DEFAULT_MODEL).toBe(OPUS_5);
  });

  it("pins the playground and assistants to Gemini 3.5 Flash", () => {
    expect(PLAYGROUND_MODEL).toBe(GEMINI_FLASH);
    expect(DEFAULT_ASSISTANT_MODEL).toBe(GEMINI_FLASH);
    expect(MODELS).toContain(GEMINI_FLASH);
  });
});
