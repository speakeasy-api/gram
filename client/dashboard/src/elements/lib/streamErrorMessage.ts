// Shown verbatim in the chat thread when the gateway rejects a request for
// lack of chat credits. The "Get Support" wording matches the dashboard's
// top-header button label so the prompt is directly actionable.
export const CREDITS_EXHAUSTED_MESSAGE =
  'You\'ve reached the chat credit limit for this account. Click the "Get Support" button at the top of the page to reach out about upgrading.';

// Lowercase substrings that identify credit exhaustion across providers:
// Gram goa ServiceError ("insufficient_credits", "token balance exhausted"),
// OpenRouter ("requires more credits"), and casual-prose variants.
const CREDIT_HINTS = [
  "insufficient_credits",
  "token balance exhausted",
  "requires more credits",
  "insufficient credits",
];

const hasCreditHint = (text: string): boolean => {
  const lower = text.toLowerCase();
  return CREDIT_HINTS.some((hint) => lower.includes(hint));
};

const tryParseJson = (raw: string): unknown => {
  try {
    return JSON.parse(raw);
  } catch {
    return undefined;
  }
};

type ErrorBag = {
  name?: unknown;
  message?: unknown;
  statusCode?: unknown;
  status?: unknown;
  responseBody?: unknown;
  cause?: unknown;
  error?: unknown;
  // AI_RetryError wraps the underlying AI_APICallError (which is where
  // statusCode lives) on `lastError` and `errors[]`. Production typically
  // returns a bare 402 with empty body, so we must descend through these.
  lastError?: unknown;
  errors?: unknown;
  // Older transport shape — fetch-wrapper errors sometimes attach the raw
  // Response on `.response`.
  response?: unknown;
};

const CHILD_FIELDS: ReadonlyArray<keyof ErrorBag> = [
  "error",
  "cause",
  "lastError",
];

// Tracks objects already visited so circular references (`err.cause === err`)
// don't recurse forever. WeakSet so we don't pin the error tree alive.
const walk = (error: unknown, seen: WeakSet<object>): boolean => {
  if (!error) return false;

  if (typeof error === "string") return hasCreditHint(error);
  if (typeof error !== "object") return false;
  if (seen.has(error)) return false;
  seen.add(error);

  const obj = error as ErrorBag;

  if (obj.name === "insufficient_credits") return true;

  const status =
    typeof obj.statusCode === "number"
      ? obj.statusCode
      : typeof obj.status === "number"
        ? obj.status
        : undefined;
  if (status === 402) return true;

  if (
    obj.response &&
    typeof obj.response === "object" &&
    (obj.response as { status?: unknown }).status === 402
  ) {
    return true;
  }

  if (typeof obj.message === "string" && hasCreditHint(obj.message))
    return true;

  if (typeof obj.responseBody === "string") {
    if (hasCreditHint(obj.responseBody)) return true;
    const parsed = tryParseJson(obj.responseBody);
    if (parsed && walk(parsed, seen)) return true;
  }

  for (const field of CHILD_FIELDS) {
    if (walk(obj[field], seen)) return true;
  }

  if (Array.isArray(obj.errors)) {
    for (const inner of obj.errors) {
      if (walk(inner, seen)) return true;
    }
  }

  return false;
};

export const describeStreamError = (error: unknown): string | undefined => {
  if (walk(error, new WeakSet())) return CREDITS_EXHAUSTED_MESSAGE;
  return undefined;
};
