import { rmSync } from "node:fs";

const directories = new Set<string>();
const signals = ["SIGINT", "SIGTERM"] as const;

function removeListeners() {
  process.removeListener("exit", cleanupAll);
  for (const signal of signals) process.removeListener(signal, onSignal);
}

function cleanupAll() {
  for (const directory of directories) {
    try {
      rmSync(directory, { recursive: true, force: true });
    } catch {
      // Cleanup is best effort; a filesystem error must not swallow termination.
    }
  }
  directories.clear();
  removeListeners();
}

function onSignal(signal: NodeJS.Signals) {
  cleanupAll();
  // Restore default termination only when no other owner handles this signal.
  // Existing worker handlers already receive it; do not deliver it twice.
  if (process.listenerCount(signal) === 0) process.kill(process.pid, signal);
}

/** Register worker-scoped storage, with one set of listeners across build retries. */
export function registerWorkerTempDirectory(directory: string): () => void {
  if (directories.size === 0) {
    process.once("exit", cleanupAll);
    for (const signal of signals) process.once(signal, onSignal);
  }
  directories.add(directory);
  return () => {
    rmSync(directory, { recursive: true, force: true });
    directories.delete(directory);
    if (directories.size === 0) removeListeners();
  };
}
