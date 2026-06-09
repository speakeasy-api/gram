// oxlint-disable-next-line import/default -- Vite ?worker&url URL import has no named default in TS resolution
import WorkerUrl from "@pierre/diffs/worker/worker.js?worker&url";

export function workerFactory(): Worker {
  return new Worker(WorkerUrl, { type: "module" });
}
