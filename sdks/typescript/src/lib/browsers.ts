const gt: unknown = typeof globalThis === "undefined" ? null : globalThis;
const webWorkerLike =
  typeof gt === "object" &&
  gt != null &&
  "importScripts" in gt &&
  typeof gt["importScripts"] === "function";
export const isBrowserLike =
  webWorkerLike ||
  (typeof navigator !== "undefined" && "serviceWorker" in navigator) ||
  (typeof window === "object" && typeof window.document !== "undefined");
