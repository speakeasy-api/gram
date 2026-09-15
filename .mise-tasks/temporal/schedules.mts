#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Pause or resume this worktree's recurring Temporal schedules"
//MISE dir="{{ config_root }}"
//MISE hide=true

//USAGE flag "--state <state>" required=#true help="Desired schedule state" { choices "pause" "unpause" }
//USAGE flag "--lock-owner <pid>" hide=#true
//USAGE flag "--coalesce-with-lock" hide=#true

import fs from "node:fs/promises";
import path from "node:path";
import { $, sleep } from "zx";

type ScheduleState = "pause" | "unpause";

type Schedule = {
  scheduleId: string;
  paused: boolean;
  notes: string | null;
};

type LockContext = {
  path: string;
  identity: string;
  state: ScheduleState;
  owns: boolean;
};

class TaskError extends Error {}

const PAUSE_REASON = "worktree stack paused";
const RESUME_REASON = "worktree stack resumed";
const LOCK_ATTEMPTS = 60;
const temporal = [
  "docker",
  "compose",
  "-f",
  "compose.shared.yml",
  "-p",
  "gram-shared",
  "exec",
  "-T",
  "gram-temporal",
  "temporal",
];
const capture = $({ stdio: ["ignore", "pipe", "inherit"] });
const quiet = $({ stdio: ["ignore", "ignore", "inherit"] });
$.verbose = false;

function errno(error: unknown, code: string): boolean {
  return (error as NodeJS.ErrnoException)?.code === code;
}

function parseState(): ScheduleState {
  const state = process.env["usage_state"];
  if (state !== "pause" && state !== "unpause") {
    throw new TaskError("--state must be pause or unpause.");
  }
  return state;
}

function requiredEnv(name: string): string {
  const value = process.env[name];
  if (!value) throw new TaskError(`${name} is not set.`);
  return value;
}

function ownerFromIdentity(identity: string | null): string | null {
  if (!identity) return null;
  const owner = identity.split(":", 1)[0];
  return owner || null;
}

function stateFromIdentity(identity: string | null): string | null {
  if (!identity) return null;
  const separator = identity.indexOf(":");
  return separator === -1 ? null : identity.slice(separator + 1);
}

async function readLink(link: string): Promise<string | null> {
  try {
    return await fs.readlink(link);
  } catch (error) {
    if (errno(error, "ENOENT")) return null;
    throw error;
  }
}

async function processIsAlive(pid: string): Promise<boolean> {
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

async function tryAcquireLock(lock: LockContext): Promise<boolean> {
  try {
    await fs.symlink(lock.identity, lock.path);
    lock.owns = true;
    return true;
  } catch (error) {
    if (errno(error, "EEXIST")) return false;
    throw error;
  }
}

async function reapStaleLock(lock: LockContext): Promise<void> {
  const reapPath = `${lock.path}.reap`;
  try {
    await fs.symlink(String(process.pid), reapPath);
  } catch (error) {
    if (errno(error, "EEXIST")) return;
    throw error;
  }

  try {
    const identity = await readLink(lock.path);
    const owner = ownerFromIdentity(identity);
    if (owner && !(await processIsAlive(owner))) {
      console.error(
        `Clearing a stack lock left behind by a dead process (${owner}).`,
      );
      await fs.rm(lock.path, { force: true });
    }
  } finally {
    await fs.rm(reapPath, { force: true });
  }
}

async function acquireLock(
  lock: LockContext,
  lockOwner: string | undefined,
  coalesce: boolean,
): Promise<boolean> {
  if (lockOwner) {
    const expected = `${lockOwner}:${lock.state}`;
    if ((await readLink(lock.path)) !== expected) {
      throw new TaskError(
        `Process ${lockOwner} does not own this worktree's ${lock.state} stack lock.`,
      );
    }
    return true;
  }

  for (let attempt = 0; attempt < LOCK_ATTEMPTS; attempt += 1) {
    if (await tryAcquireLock(lock)) return true;
    await reapStaleLock(lock);
    if (await tryAcquireLock(lock)) return true;

    const currentIdentity = await readLink(lock.path);
    if (coalesce && stateFromIdentity(currentIdentity) === lock.state) {
      return false;
    }
    await sleep(1000);
  }

  const identity = await readLink(lock.path);
  const owner = ownerFromIdentity(identity) ?? "unknown";
  throw new TaskError(
    `Another stack lifecycle transition (pid ${owner}) has held this worktree's stack lock for a minute; giving up.\n` +
      `If nothing is running, remove ${lock.path} and retry.`,
  );
}

async function releaseLock(lock: LockContext): Promise<void> {
  if (lock.owns && (await readLink(lock.path)) === lock.identity) {
    await fs.rm(lock.path, { force: true });
  }
}

function parseSchedules(raw: string): Schedule[] {
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    throw new TaskError("Temporal returned invalid schedule JSON.");
  }
  if (!Array.isArray(value)) {
    throw new TaskError("Temporal returned an invalid schedule list.");
  }

  const schedules: Schedule[] = [];
  for (const entry of value) {
    if (!entry || typeof entry !== "object") continue;
    const scheduleId = Reflect.get(entry, "scheduleId");
    const info = Reflect.get(entry, "info");
    if (typeof scheduleId !== "string" || !scheduleId) continue;
    const paused =
      !!info &&
      typeof info === "object" &&
      Reflect.get(info, "paused") === true;
    const rawNotes =
      !!info && typeof info === "object" ? Reflect.get(info, "notes") : null;
    schedules.push({
      scheduleId,
      paused,
      notes: typeof rawNotes === "string" ? rawNotes : null,
    });
  }
  return schedules;
}

async function listSchedules(namespace: string): Promise<Schedule[]> {
  const result =
    await capture`${temporal} schedule list --namespace ${namespace} --output json`.nothrow();
  if (result.exitCode !== 0) {
    throw new TaskError(`Could not list Temporal schedules in ${namespace}.`);
  }
  return parseSchedules(result.stdout);
}

async function toggleSchedule(
  namespace: string,
  scheduleId: string,
  state: ScheduleState,
): Promise<boolean> {
  const toggle = state === "pause" ? "--pause" : "--unpause";
  const reason = state === "pause" ? PAUSE_REASON : RESUME_REASON;
  const result =
    await quiet`${temporal} schedule toggle --namespace ${namespace} --schedule-id ${scheduleId} ${toggle} --reason ${reason}`.nothrow();
  return result.exitCode === 0;
}

async function readScheduleIds(stateFile: string): Promise<string[]> {
  try {
    return (await fs.readFile(stateFile, "utf8")).split("\n").filter(Boolean);
  } catch (error) {
    if (errno(error, "ENOENT")) return [];
    throw error;
  }
}

async function writeScheduleIds(
  stateFile: string,
  pendingFile: string,
  scheduleIds: Iterable<string>,
): Promise<void> {
  const ids = [...scheduleIds];
  await fs.writeFile(pendingFile, ids.length > 0 ? `${ids.join("\n")}\n` : "");
  await fs.rename(pendingFile, stateFile);
}

async function pauseSchedules(
  namespace: string,
  stateFile: string,
  pendingFile: string,
): Promise<boolean> {
  const schedules = await listSchedules(namespace);
  const recordedIds = new Set(await readScheduleIds(stateFile));
  let failed = false;

  for (const schedule of schedules) {
    if (schedule.paused) continue;

    // Record intent first. If the process exits after Temporal accepts the
    // pause, wake can still identify and safely resume it.
    if (!recordedIds.has(schedule.scheduleId)) {
      recordedIds.add(schedule.scheduleId);
      await writeScheduleIds(stateFile, pendingFile, recordedIds);
    }

    if (!(await toggleSchedule(namespace, schedule.scheduleId, "pause"))) {
      console.error(
        `Could not pause Temporal schedule ${schedule.scheduleId}.`,
      );
      failed = true;
    }
  }
  return !failed;
}

async function unpauseSchedules(
  namespace: string,
  stateFile: string,
  pendingFile: string,
): Promise<boolean> {
  const recordedIds = await readScheduleIds(stateFile);
  if (recordedIds.length === 0) {
    await fs.rm(stateFile, { force: true });
    return true;
  }

  const schedules = new Map(
    (await listSchedules(namespace)).map((schedule) => [
      schedule.scheduleId,
      schedule,
    ]),
  );
  const unresolved: string[] = [];

  for (const scheduleId of recordedIds) {
    const schedule = schedules.get(scheduleId);
    if (!schedule || !schedule.paused || schedule.notes !== PAUSE_REASON) {
      continue;
    }

    if (!(await toggleSchedule(namespace, scheduleId, "unpause"))) {
      console.error(`Could not unpause Temporal schedule ${scheduleId}.`);
      unresolved.push(scheduleId);
    }
  }

  if (unresolved.length > 0) {
    await writeScheduleIds(stateFile, pendingFile, unresolved);
    return false;
  }

  await fs.rm(stateFile, { force: true });
  return true;
}

async function main(): Promise<void> {
  const state = parseState();
  const namespace = requiredEnv("TEMPORAL_NAMESPACE");
  const { stdout } = await capture`git rev-parse --absolute-git-dir`;
  const gitDir = stdout.trim();
  if (!gitDir) throw new TaskError("Could not resolve the Git directory.");

  const stateFile = path.join(gitDir, "gram-stack-paused-schedules");
  const pendingFile = `${stateFile}.${process.pid}`;
  const lock: LockContext = {
    path: path.join(gitDir, "gram-stack-lock"),
    identity: `${process.pid}:${state}`,
    state,
    owns: false,
  };

  try {
    const shouldRun = await acquireLock(
      lock,
      process.env["usage_lock_owner"] || undefined,
      process.env["usage_coalesce_with_lock"] === "true",
    );
    if (!shouldRun) return;

    const succeeded =
      state === "pause"
        ? await pauseSchedules(namespace, stateFile, pendingFile)
        : await unpauseSchedules(namespace, stateFile, pendingFile);
    if (!succeeded) process.exitCode = 1;
  } finally {
    await fs.rm(pendingFile, { force: true });
    await releaseLock(lock);
  }
}

try {
  await main();
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
}
