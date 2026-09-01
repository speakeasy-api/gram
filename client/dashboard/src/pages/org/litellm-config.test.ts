import { describe, expect, it } from "vitest";
import {
  buildLiteLLMEnvironment,
  buildLiteLLMGuardrailConfig,
  liteLLMVerificationCommands,
} from "./litellm-config";

describe("LiteLLM configuration", () => {
  it("references the secret from the environment and preserves fail-closed defaults", () => {
    const config = buildLiteLLMGuardrailConfig("https://api.getgram.ai/");

    expect(config).toContain(
      "api_base: https://api.getgram.ai/rpc/litellm.ingest",
    );
    expect(config).toContain("Gram-Key: os.environ/GRAM_LITELLM_INGEST_KEY");
    expect(config).toContain(`      extra_headers:
        - x-gram-acting-principal
        - x-gram-acting-principal-contract
        - x-gram-inference-invocation-id
        - x-gram-session-id
        - x-claude-code-session-id
        - session-id
        - thread-id
        - x-session-id
        - x-gram-agent-provider
        - x-gram-agent-session-id
        - x-gram-agent-turn-id
        - x-codex-turn-metadata
        - x-opencode-session
        - x-opencode-request`);
    expect(config).toContain("streaming_end_of_stream_only: true");
    expect(config).toContain("unreachable_fallback: fail_closed");
  });

  it("always emits the canonical fail-closed posture", () => {
    const config = buildLiteLLMGuardrailConfig("https://api.getgram.ai");
    expect(config).toContain("unreachable_fallback: fail_closed");
    expect(config).not.toContain("unreachable_fallback: fail_open");
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
      'export OTEL_ENDPOINT="https://api.getgram.ai/rpc/hooks.otel"',
    );
    expect(environment).toContain(
      'export OTEL_HEADERS="Gram-Key=${GRAM_LITELLM_INGEST_KEY},Gram-Project=${GRAM_PROJECT_SLUG}"',
    );
  });

  it("rejects cleartext non-loopback endpoints", () => {
    expect(() =>
      buildLiteLLMEnvironment("http://api.example.com", "my-project"),
    ).toThrow("LiteLLM integration endpoints require HTTPS");
    expect(
      buildLiteLLMEnvironment("http://localhost:8080", "my-project"),
    ).toContain('export OTEL_ENDPOINT="http://localhost:8080/rpc/hooks.otel"');
    expect(
      buildLiteLLMEnvironment("http://127.0.0.2:8080", "my-project"),
    ).toContain('export OTEL_ENDPOINT="http://127.0.0.2:8080/rpc/hooks.otel"');
    expect(() =>
      buildLiteLLMEnvironment("ftp://localhost", "my-project"),
    ).toThrow("LiteLLM integration endpoints require HTTPS");
    expect(() =>
      buildLiteLLMEnvironment("http://127.example.com", "my-project"),
    ).toThrow("LiteLLM integration endpoints require HTTPS");
  });

  it("provides safe and synthetic-block verification commands", () => {
    expect(liteLLMVerificationCommands.safe).toContain("Reply with OK.");
    expect(liteLLMVerificationCommands.blocked).toContain("ghp_R2D2C3POL");
  });
});
