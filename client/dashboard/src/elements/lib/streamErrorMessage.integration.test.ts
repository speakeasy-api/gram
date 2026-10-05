import { createOpenRouter } from "@openrouter/ai-sdk-provider";
import { createUIMessageStream, streamText } from "ai";
import { describe, expect, it } from "vitest";
import {
  sanitizeStreamErrorForTelemetry,
  toElementsUIMessageStream,
} from "./streamErrorMessage";

describe("Elements AI SDK stream error propagation", () => {
  it("renders a production-shaped AI access denial without reporting its note", async () => {
    const note =
      "AI access is paused while this workspace completes its security review.";
    const reportedErrors: unknown[] = [];
    const model = createOpenRouter({
      baseURL: "https://gram.example.test",
      apiKey: "unused",
      fetch: async () =>
        new Response(
          JSON.stringify({ name: "ai_access_denied", message: note }),
          {
            status: 403,
            headers: { "content-type": "application/json" },
          },
        ),
    }).chat("openai/gpt-4o-mini");

    const result = streamText({
      model,
      messages: [{ role: "user", content: "Hello" }],
      maxRetries: 0,
      onError: ({ error }) => {
        // ElementsProvider sends this sanitized value to both console.error and
        // telemetry. Preserve the same boundary in this integration test.
        reportedErrors.push(sanitizeStreamErrorForTelemetry(error));
      },
    });
    const uiStream = createUIMessageStream({
      execute: ({ writer }) => {
        writer.merge(toElementsUIMessageStream({ stream: result.stream }));
      },
    });

    const chunks = [];
    for await (const chunk of uiStream) chunks.push(chunk);

    expect(chunks).toContainEqual({ type: "error", errorText: note });
    expect(chunks).not.toContainEqual({
      type: "error",
      errorText: "An error occurred.",
    });
    expect(reportedErrors).toHaveLength(1);
    expect(String(reportedErrors[0])).toBe("Error: ai_access_denied");
    expect(JSON.stringify(reportedErrors)).not.toContain(note);
  });
});
