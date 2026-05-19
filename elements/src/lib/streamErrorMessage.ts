// Friendly client-visible message produced when the LLM gateway reports the
// account has exhausted its chat credit allowance. The "Get Support" wording
// matches the existing button label rendered by the dashboard top header, so
// the user can act on the prompt without leaving the page.
export const CREDITS_EXHAUSTED_MESSAGE =
  'You\'ve reached the chat credit limit for this account. Click the "Get Support" button at the top of the page to reach out about upgrading.';

// Common credit-exhaustion fingerprints across the two error shapes we see:
// - Gram (goa) ServiceError with name="insufficient_credits" (HTTP 402)
// - OpenRouter upstream error containing "requires more credits"
// Both can arrive either as a string in `responseBody` or directly on the
// error object's `message`. We sniff defensively so a tweak to either provider
// doesn't reintroduce the silent "messages just stop" failure mode.
const CREDIT_HINTS = [
  "insufficient_credits",
  "token balance exhausted",
  "requires more credits",
  "insufficient credits",
];

const hasCreditHint = (text: string): boolean => {
  const lower = text.toLowerCase();
  return CREDIT_HINTS.some((hint) => lower.includes(hint.toLowerCase()));
};

const tryParseJson = (raw: string): unknown => {
  try {
    return JSON.parse(raw);
  } catch {
    return undefined;
  }
};

// extractCreditSignals walks the bag of fields the AI SDK / OpenRouter / Gram
// throw together on a stream error and returns true if any of them carry one
// of our credit-exhaustion fingerprints.
const isCreditsError = (error: unknown): boolean => {
  if (!error) return false;

  if (typeof error === "string") {
    return hasCreditHint(error);
  }

  if (typeof error !== "object") return false;

  const obj = error as {
    name?: unknown;
    message?: unknown;
    statusCode?: unknown;
    status?: unknown;
    responseBody?: unknown;
    cause?: unknown;
    error?: unknown;
  };

  if (typeof obj.name === "string" && obj.name === "insufficient_credits") {
    return true;
  }

  // 402 Payment Required is the canonical signal from Gram and most LLM
  // gateways for credit exhaustion. The AI SDK surfaces this on either
  // `statusCode` or `status` depending on the transport.
  const status =
    typeof obj.statusCode === "number"
      ? obj.statusCode
      : typeof obj.status === "number"
        ? obj.status
        : undefined;
  if (status === 402) return true;

  if (typeof obj.message === "string" && hasCreditHint(obj.message)) {
    return true;
  }

  if (typeof obj.responseBody === "string") {
    if (hasCreditHint(obj.responseBody)) return true;
    const parsed = tryParseJson(obj.responseBody);
    if (parsed && isCreditsError(parsed)) return true;
  }

  if (obj.error && isCreditsError(obj.error)) return true;
  if (obj.cause && isCreditsError(obj.cause)) return true;

  return false;
};

// describeStreamError maps a stream error into the string we want surfaced to
// the user. Returns the friendly credits message for credit exhaustion;
// otherwise returns undefined so callers can fall back to the AI SDK's
// default error rendering.
export const describeStreamError = (error: unknown): string | undefined => {
  if (isCreditsError(error)) return CREDITS_EXHAUSTED_MESSAGE;
  return undefined;
};
