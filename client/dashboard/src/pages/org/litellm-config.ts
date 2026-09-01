function apiBase(serverURL: string): string {
  const url = new URL(serverURL);
  const ipv4Parts = url.hostname.split(".");
  const ipv4Loopback =
    ipv4Parts.length === 4 &&
    ipv4Parts[0] === "127" &&
    ipv4Parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255);
  const loopback =
    url.hostname === "localhost" || url.hostname === "[::1]" || ipv4Loopback;
  if (url.protocol !== "https:" && !(url.protocol === "http:" && loopback)) {
    throw new Error("LiteLLM integration endpoints require HTTPS");
  }
  return url.toString().replace(/\/$/, "");
}

export function buildLiteLLMGuardrailConfig(serverURL: string): string {
  return `guardrails:
  - guardrail_name: gram-risk
    litellm_params:
      guardrail: generic_guardrail_api
      mode: [pre_call, post_call]
      api_base: ${apiBase(serverURL)}/rpc/litellm.ingest
      headers:
        Gram-Key: os.environ/GRAM_LITELLM_INGEST_KEY
        Gram-Project: os.environ/GRAM_PROJECT_SLUG
      extra_headers:
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
        - x-opencode-request
      default_on: true
      streaming_end_of_stream_only: true
      fail_on_error: true
      unreachable_fallback: fail_closed`;
}

export function buildLiteLLMEnvironment(
  serverURL: string,
  projectSlug: string,
): string {
  return `export GRAM_LITELLM_INGEST_KEY="<PASTE_KEY_SHOWN_ABOVE>"
export GRAM_PROJECT_SLUG="${projectSlug}"
export LITELLM_OTEL_V2=true
export OTEL_EXPORTER=otlp_http
export OTEL_ENDPOINT="${apiBase(serverURL)}/rpc/hooks.otel"
export OTEL_HEADERS="Gram-Key=\${GRAM_LITELLM_INGEST_KEY},Gram-Project=\${GRAM_PROJECT_SLUG}"
export OTEL_SERVICE_NAME=litellm
export OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=no_content
export LITELLM_OTEL_INTEGRATION_ENABLE_METRICS=true
export LITELLM_OTEL_LEGACY_COMPAT=false`;
}

function liteLLMVerificationCommand(content: string): string {
  return `curl "\${LITELLM_PROXY_URL:-http://localhost:4000}/v1/chat/completions" \\
  --header "Authorization: Bearer $LITELLM_VIRTUAL_KEY" \\
  --header "X-Gram-Acting-Principal: $GRAM_LITELLM_ACTING_PRINCIPAL" \\
  --header "X-Gram-Acting-Principal-Contract: litellm-acting-principal.v1" \\
  --header "X-Gram-Inference-Invocation-ID: $GRAM_LITELLM_INVOCATION_ID" \\
  --header "Content-Type: application/json" \\
  --data '{"model":"'"$LITELLM_MODEL"'","messages":[{"role":"user","content":"${content}"}]}'`;
}

export const liteLLMVerificationCommands = {
  safe: liteLLMVerificationCommand("Reply with OK."),
  blocked: liteLLMVerificationCommand(
    "token=ghp_R2D2C3POLuk3Skywalker1234567890ab",
  ),
} as const;
