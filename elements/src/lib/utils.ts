import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

export function assertNever(value: unknown): never {
  throw new Error(`Unexpected value: ${String(value)}`);
}

/**
 * Two-letter initials from a display name or email handle: first letter of
 * the first and last word for a multi-word name ("Adam Bull" -> "AB"), or
 * the first two characters of the email's local part / a single-word name
 * otherwise ("adam@..." -> "AD").
 */
export function initialsOf(identifier: string): string {
  const handle = identifier.includes("@")
    ? (identifier.split("@")[0] ?? identifier)
    : identifier;
  const words = handle.trim().split(/\s+/).filter(Boolean);
  if (words.length >= 2) {
    return (
      words[0]!.charAt(0) + words[words.length - 1]!.charAt(0)
    ).toUpperCase();
  }
  return handle.slice(0, 2).toUpperCase();
}

export function assert(condition: unknown, message: string): asserts condition {
  if (!condition) {
    throw new Error(message);
  }
}

/** Sleep that respects AbortSignal for clean cancellation. */
export function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException("Aborted", "AbortError"));
      return;
    }
    const onAbort = () => {
      clearTimeout(timeout);
      signal?.removeEventListener("abort", onAbort);
      reject(new DOMException("Aborted", "AbortError"));
    };
    const timeout = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}
