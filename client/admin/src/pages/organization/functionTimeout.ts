// Mirrors server/internal/constants/functions.go. The server stays the
// authority; these only let most mistakes show inline before a request.
export const MIN_FUNCTION_TIMEOUT_SECONDS = 30;
export const MAX_FUNCTION_TIMEOUT_SECONDS = 600;
export const DEFAULT_FUNCTION_TIMEOUT_SECONDS = 60;

export type ParsedFunctionTimeout =
  | { seconds: number; problem: null }
  | { seconds: null; problem: string };

// Accepts a whole number of seconds within the bounds the server enforces.
export function parseFunctionTimeout(value: string): ParsedFunctionTimeout {
  const trimmed = value.trim();
  const bounds = `Enter a whole number of seconds from ${MIN_FUNCTION_TIMEOUT_SECONDS} to ${MAX_FUNCTION_TIMEOUT_SECONDS}.`;
  if (!/^\d+$/.test(trimmed)) {
    return { seconds: null, problem: bounds };
  }
  const seconds = Number(trimmed);
  if (
    seconds < MIN_FUNCTION_TIMEOUT_SECONDS ||
    seconds > MAX_FUNCTION_TIMEOUT_SECONDS
  ) {
    return { seconds: null, problem: bounds };
  }
  return { seconds, problem: null };
}

// "45 seconds", "1 minute", "1 minute 30 seconds", "10 minutes".
export function formatFunctionTimeout(seconds: number): string {
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  const parts: string[] = [];
  if (minutes > 0) {
    parts.push(`${minutes} ${minutes === 1 ? "minute" : "minutes"}`);
  }
  if (rest > 0 || minutes === 0) {
    parts.push(`${rest} ${rest === 1 ? "second" : "seconds"}`);
  }
  return parts.join(" ");
}
