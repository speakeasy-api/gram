import { isSecureUrl, loadConfig } from "./config.js";
import type { IngestBody } from "./mapping.js";

const TIMEOUT_MS = 5_000;
const MAX_ATTEMPTS = 2;
const RETRY_BASE_MS = 200;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// Fail-open by design: the agent must never block or throw because
// telemetry delivery failed. Swallows every error; the same
// idempotency_key is reused across attempts so a redelivery is a no-op.
export async function send(body: IngestBody): Promise<void> {
  const { url, key, project } = loadConfig();

  // Never transmit the key or payloads over a non-TLS endpoint (loadConfig has
  // already warned once). Fail-open: drop the event rather than throw.
  if (!isSecureUrl(url)) return;

  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), TIMEOUT_MS);
    try {
      const res = await fetch(`${url}/rpc/hooks.ingest`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": body.idempotency_key,
          ...(key ? { "Gram-Key": key } : {}),
          ...(project ? { "Gram-Project": project } : {}),
        },
        body: JSON.stringify(body),
        signal: controller.signal,
      });
      // We never read the response body; cancel it so the underlying
      // connection is released back to the pool instead of leaking.
      void res.body?.cancel();
      if (res.ok || attempt === MAX_ATTEMPTS) {
        return;
      }
    } catch {
      // network error, timeout, abort — fall through to retry/give-up below
    } finally {
      clearTimeout(timer);
    }
    if (attempt < MAX_ATTEMPTS) {
      // ponytail: fixed jittered backoff, no exponential curve — revisit if
      // ingest starts throttling under a bigger retry budget.
      await sleep(RETRY_BASE_MS + Math.random() * RETRY_BASE_MS);
    }
  }
}
