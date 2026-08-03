import { describe, expect, it } from "vitest";
import {
  buildLiteLLMEnvironment,
  buildLiteLLMGuardrailConfig,
  liteLLMVerificationCommands,
} from "./litellm-config";

describe("LiteLLM configuration", () => {
  it("references the secret from the environment and preserves fail-closed defaults", () => {
    const config = buildLiteLLMGuardrailConfig(
      "https://api.getgram.ai/",
      "fail_closed",
    );

    expect(config).toContain(
      "api_base: https://api.getgram.ai/rpc/litellm.ingest",
    );
    expect(config).toContain("Gram-Key: os.environ/GRAM_LITELLM_INGEST_KEY");
    expect(config).toContain("streaming_end_of_stream_only: true");
    expect(config).toContain("unreachable_fallback: fail_closed");
  });

  it("renders the explicit fail-open posture", () => {
    expect(
      buildLiteLLMGuardrailConfig("https://api.getgram.ai", "fail_open"),
    ).toContain("unreachable_fallback: fail_open");
  });

  it("builds project-bound OTel environment variables", () => {
    const environment = buildLiteLLMEnvironment(
      "https://api.getgram.ai/",
      "my-project",
    );

    expect(environment).toContain(
      'export GRAM_LITELLM_INGEST_KEY="<PASTE_KEY_SHOWN_ABOVE>"',
    );
    expect(environment).toContain('export GRAM_PROJECT_SLUG="my-project"');
    expect(environment).toContain(
      'export OTEL_ENDPOINT="https://api.getgram.ai/rpc/litellm.otel"',
    );
    expect(environment).toContain(
      'export OTEL_HEADERS="Gram-Key=${GRAM_LITELLM_INGEST_KEY},Gram-Project=${GRAM_PROJECT_SLUG}"',
    );
  });

  it("provides safe and synthetic-block verification commands", () => {
    expect(liteLLMVerificationCommands.safe).toContain("Reply with OK.");
    expect(liteLLMVerificationCommands.blocked).toContain("ghp_R2D2C3POL");
  });
});
