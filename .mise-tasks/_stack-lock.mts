// Helpers for the per-worktree stack lock (`<gitdir>/gram-stack-lock`) taken
// by pause, wake, start and anything that must not interleave with them. The
// lock is a symlink whose target is `<pid>:<intent>`: creating it is the atomic
// test-and-set, and the target records the owner in the same step. The bash
// tasks (wake.sh, pause.sh, start) implement the same protocol inline.
//
// Not a task: this file is not executable, so mise does not list it.

import fs from "node:fs/promises";

function errno(error: unknown, code: string): boolean {
  return (error as NodeJS.ErrnoException)?.code === code;
}

/** Reads a symlink's target, or null when it does not exist. */
export async function readLink(link: string): Promise<string | null> {
  try {
    return await fs.readlink(link);
  } catch (error) {
    if (errno(error, "ENOENT")) return null;
    throw error;
  }
}

export function ownerFromIdentity(identity: string | null): string | null {
  if (!identity) return null;
  const owner = identity.split(":", 1)[0];
  return owner || null;
}

export async function processIsAlive(pid: string): Promise<boolean> {
  if (!/^\d+$/.test(pid)) return false;
  try {
    process.kill(Number(pid), 0);
    return true;
  } catch (error) {
    if (errno(error, "ESRCH")) return false;
    if (errno(error, "EPERM")) return true;
    throw error;
  }
}

/** Creates the lock with this identity; false if someone else holds it. */
export async function tryCreateLock(
  path: string,
  identity: string,
): Promise<boolean> {
  try {
    await fs.symlink(identity, path);
    return true;
  } catch (error) {
    if (errno(error, "EEXIST")) return false;
    throw error;
  }
}

/**
 * Clears a lock whose owner process is gone. Removal happens under its own
 * exclusive `.reap` symlink and re-reads the owner there, so a live holder
 * that took the lock in between is never evicted.
 */
export async function reapStaleLock(path: string): Promise<void> {
  const reapPath = `${path}.reap`;
  try {
    await fs.symlink(String(process.pid), reapPath);
  } catch (error) {
    if (errno(error, "EEXIST")) return;
    throw error;
  }

  try {
    const owner = ownerFromIdentity(await readLink(path));
    if (owner && !(await processIsAlive(owner))) {
      console.error(
        `Clearing a stack lock left behind by a dead process (${owner}).`,
      );
      await fs.rm(path, { force: true });
    }
  } finally {
    await fs.rm(reapPath, { force: true });
  }
}

/** Removes the lock only if this identity still owns it. */
export async function releaseLock(
  path: string,
  identity: string,
): Promise<void> {
  if ((await readLink(path)) === identity) {
    await fs.rm(path, { force: true });
  }
}
