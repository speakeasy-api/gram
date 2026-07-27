import { isSecureUrl, loadConfig } from "./config.js";
import type { IngestBody } from "./mapping.js";

const TIMEOUT_MS = 5_000;
const MAX_ATTEMPTS = 2;
const RETRY_BASE_MS = 200;

// The server's verdict for a hook event (the IngestHookResult body). decision is
// "allow" | "deny"; a deny carries the policy's reason/message for the user.
export type Verdict = {
  decision: "allow" | "deny";
  reason?: string;
  message?: string;
};

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// send posts a canonical hook event and returns the server's verdict. It is
// fail-open by design: the agent must never block or throw because telemetry
// delivery failed. Returns undefined — proceed without blocking — for every
// ambiguity: insecure URL, network error, timeout/abort, non-2xx status, or a
// missing/unparseable body. Only an explicit 2xx verdict is returned; the
// caller enforces (throws) solely on decision === "deny". The same
// idempotency_key is reused across attempts so a redelivery is a no-op.
export async function send(body: IngestBody): Promise<Verdict | undefined> {
  const { url, key, project } = loadConfig();

  // Never transmit the key or payloads over a non-TLS endpoint (loadConfig has
  // already warned once). Fail-open: drop the event rather than throw.
  if (!isSecureUrl(url)) return undefined;

  // One 5s budget spans both attempts + backoff so the block path can't hold
  // the agent for ~10s: each attempt gets only the time left before the shared
  // deadline, and we bail if there's no budget for a retry. A timeout fails
  // open, so the ceiling stays "agent waits ≤5s then proceeds."
  const deadline = Date.now() + TIMEOUT_MS;
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    const remaining = deadline - Date.now();
    if (remaining <= 0) return undefined;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), remaining);
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
      if (res.ok) {
        return await parseVerdict(res);
      }
      if (attempt === MAX_ATTEMPTS) {
        // Non-2xx from the server carries no verdict we can act on: fail-open.
        void res.body?.cancel();
        return undefined;
      }
      void res.body?.cancel();
    } catch {
      // network error, timeout, abort — fall through to retry/give-up below
    } finally {
      clearTimeout(timer);
    }
    if (attempt < MAX_ATTEMPTS) {
      // ponytail: fixed jittered backoff, no exponential curve — revisit if
      // ingest starts throttling under a bigger retry budget.
      const backoff = RETRY_BASE_MS + Math.random() * RETRY_BASE_MS;
      // Skip the retry entirely if the backoff would blow the shared deadline.
      if (Date.now() + backoff >= deadline) return undefined;
      await sleep(backoff);
    }
  }
  return undefined;
}

// parseVerdict reads the 2xx body into a Verdict, failing open (undefined) on a
// missing/unparseable body or an unrecognized decision.
async function parseVerdict(res: Response): Promise<Verdict | undefined> {
  try {
    const raw = (await res.json()) as {
      decision?: unknown;
      reason?: unknown;
      message?: unknown;
    };
    if (raw.decision !== "allow" && raw.decision !== "deny") return undefined;
    return {
      decision: raw.decision,
      ...(typeof raw.reason === "string" ? { reason: raw.reason } : {}),
      ...(typeof raw.message === "string" ? { message: raw.message } : {}),
    };
  } catch {
    return undefined;
  }
}
