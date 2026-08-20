/**
 * Findings that have been restored but may still come back from the suppressed
 * listing, which is served from a ClickHouse mirror that lags the write. The
 * listing hides these ids until the mirror can be trusted to have caught up.
 *
 * Module-scoped rather than per-hook on purpose. `restore` and `dismiss` are
 * called from several independent useDismissFinding instances — the signals
 * drawer, the risk events log, the suppressed section — and a re-suppress from
 * any of them has to stop the listing hiding that row straight away. Held
 * per-instance, a finding suppressed again from one component could stay
 * invisible in another for the rest of the expiry window.
 *
 * Expiry timers deliberately outlive component unmount: they belong to this
 * store, not to whoever happened to trigger the restore, and an expiry that
 * stopped when the last listing unmounted would leave the id hidden for every
 * later mount. They stay bounded — at most one per hidden id, each removing
 * itself from the map when it fires — and drain to nothing once every held id
 * has expired.
 */

const restoredIds = new Set<string>();
const expiryTimers = new Map<string, ReturnType<typeof setTimeout>>();
const listeners = new Set<() => void>();

// useSyncExternalStore compares snapshots by identity, so subscribers would
// never see a Set mutated in place. Every change publishes a fresh one.
let snapshot: ReadonlySet<string> = new Set();

function publish(): void {
  snapshot = new Set(restoredIds);
  listeners.forEach((listener) => {
    listener();
  });
}

export function subscribeRestoredFindings(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export function getRestoredFindings(): ReadonlySet<string> {
  return snapshot;
}

/**
 * Hide `ids` while a restore is in flight. No expiry is scheduled here: the
 * request can outlast the expiry window, and a clock started now could lapse
 * before the write had even landed, un-hiding a row the mirror is still
 * serving. `expireRestoredAfter` starts it once the outcome is known.
 */
export function holdRestored(ids: string[]): void {
  if (ids.length === 0) return;
  for (const id of ids) {
    // A second restore of the same id supersedes the first, so the earlier
    // hold's clock must not cut the new one short.
    clearExpiry(id);
    restoredIds.add(id);
  }
  publish();
}

/** Start the expiry clock for ids whose restore has actually landed. */
export function expireRestoredAfter(ids: string[], ms: number): void {
  for (const id of ids) {
    // Released in the meantime (suppressed again, say) — leave it released.
    if (!restoredIds.has(id)) continue;
    clearExpiry(id);
    expiryTimers.set(
      id,
      setTimeout(() => {
        expiryTimers.delete(id);
        if (restoredIds.delete(id)) publish();
      }, ms),
    );
  }
}

/**
 * Put ids straight back on the suppressed listing: the restore failed, or the
 * finding has been suppressed again and belongs there regardless of any expiry
 * still pending.
 */
export function releaseRestored(ids: string[]): void {
  let changed = false;
  for (const id of ids) {
    clearExpiry(id);
    if (restoredIds.delete(id)) changed = true;
  }
  if (changed) publish();
}

function clearExpiry(id: string): void {
  const timer = expiryTimers.get(id);
  if (timer === undefined) return;
  clearTimeout(timer);
  expiryTimers.delete(id);
}

/** Pending expiry timers. Exported so tests can assert the map drains. */
export function pendingRestoredExpiries(): number {
  return expiryTimers.size;
}

/** Drops all state. Module-scoped state outlives a test, so tests reset it. */
export function resetRestoredFindings(): void {
  expiryTimers.forEach(clearTimeout);
  expiryTimers.clear();
  restoredIds.clear();
  publish();
}
