import { describe, expect, it } from "vitest";
import { parsePrompt } from "./toolBuilderState";

describe("parsePrompt", () => {
  it("normalizes legacy step IDs without adding runtime callbacks", () => {
    const parsed = parsePrompt(
      JSON.stringify({
        toolName: "weather_summary",
        purpose: "Summarize weather",
        inputs: [{ name: "city" }],
        steps: [
          {
            tool: "weather_lookup",
            instructions: "Look up {{city}}",
          },
        ],
      }),
    );

    expect(parsed.purpose).toBe("Summarize weather");
    expect(parsed.inputs).toEqual([{ name: "city" }]);
    expect(parsed.steps).toHaveLength(1);
    expect(parsed.steps[0]?.id).toEqual(expect.any(String));
    expect(parsed.steps[0]).not.toHaveProperty("update");
  });
});
