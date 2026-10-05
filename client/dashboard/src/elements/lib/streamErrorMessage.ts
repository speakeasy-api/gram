import {
  toUIMessageStream,
  type TextStreamPart,
  type ToolSet,
  type UIMessageChunk,
} from "ai";

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

const AI_ACCESS_DENIED = "ai_access_denied";

const CHILD_FIELDS: ReadonlyArray<keyof ErrorBag> = [
  "error",
  "cause",
  "lastError",
];

type ErrorTreeVisitor<T> = (value: unknown) => T | undefined;

// Visit every normalized error node once. responseBody is visited both as raw
// text and, when valid JSON, as a nested error envelope.
const visitErrorTree = <T>(
  error: unknown,
  visitor: ErrorTreeVisitor<T>,
  seen = new WeakSet<object>(),
): T | undefined => {
  const result = visitor(error);
  if (result !== undefined) return result;
  if (!error || typeof error !== "object" || seen.has(error)) return undefined;
  seen.add(error);

  const obj = error as ErrorBag;
  if (typeof obj.responseBody === "string") {
    const rawResult = visitor(obj.responseBody);
    if (rawResult !== undefined) return rawResult;
    const parsedResult = visitErrorTree(
      tryParseJson(obj.responseBody),
      visitor,
      seen,
    );
    if (parsedResult !== undefined) return parsedResult;
  }
  for (const field of CHILD_FIELDS) {
    const childResult = visitErrorTree(obj[field], visitor, seen);
    if (childResult !== undefined) return childResult;
  }
  if (Array.isArray(obj.errors)) {
    for (const inner of obj.errors) {
      const childResult = visitErrorTree(inner, visitor, seen);
      if (childResult !== undefined) return childResult;
    }
  }
  return undefined;
};

// Extract only the server-authored dedicated denial envelope. Never render a
// message from an unavailable/generic error, or from an object that merely
// mentions the code in free text. React renders the returned string as text.
const findAIAccessDenial = (error: unknown): ErrorBag | undefined =>
  visitErrorTree(error, (value) => {
    if (!value || typeof value !== "object") return undefined;
    const obj = value as ErrorBag;
    return obj.name === AI_ACCESS_DENIED ? obj : undefined;
  });

const findAIAccessDenialNote = (error: unknown): string | undefined => {
  const denial = findAIAccessDenial(error);
  if (typeof denial?.message !== "string") return undefined;
  return denial.message.trim() ? denial.message : undefined;
};

const hasCreditsError = (error: unknown): boolean =>
  visitErrorTree(error, (value) => {
    if (typeof value === "string") return hasCreditHint(value) || undefined;
    if (!value || typeof value !== "object") return undefined;

    const obj = value as ErrorBag;
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
    )
      return true;
    if (typeof obj.message === "string" && hasCreditHint(obj.message))
      return true;
    return undefined;
  }) === true;

// Tenant-authored denial notes are display content, never telemetry content.
export const sanitizeStreamErrorForTelemetry = (error: unknown): unknown =>
  findAIAccessDenial(error) ? new Error(AI_ACCESS_DENIED) : error;

export const describeStreamError = (error: unknown): string | undefined => {
  const denialNote = findAIAccessDenialNote(error);
  if (denialNote) return denialNote;
  if (hasCreditsError(error)) return CREDITS_EXHAUSTED_MESSAGE;
  return undefined;
};

export const describeStreamErrorForUI = (error: unknown): string =>
  describeStreamError(error) ??
  "An error occurred while generating a response.";

// `toUIMessageStream` masks model errors unless its own onError callback is
// provided. createUIMessageStream's onError only handles errors thrown while
// executing/merging streams, so configuring it alone cannot replace an error
// chunk already emitted by this converter. Keep this wrapper on the actual
// conversion boundary so selected safe messages survive into the rendered UI.
export const toElementsUIMessageStream = <TOOLS extends ToolSet>({
  stream,
  tools,
}: {
  stream: ReadableStream<TextStreamPart<TOOLS>>;
  tools?: TOOLS;
}): ReadableStream<UIMessageChunk> =>
  toUIMessageStream({
    stream,
    tools,
    onError: describeStreamErrorForUI,
  });
