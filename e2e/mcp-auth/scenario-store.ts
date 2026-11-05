import {
  appendFileSync,
  mkdirSync,
  readFileSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { randomBytes, randomUUID } from "node:crypto";
import { resolve } from "node:path";

export type ScenarioKind = "mock" | "ema" | "workload";

function readText(path: string): string | undefined {
  try {
    return readFileSync(path, "utf8");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return undefined;
    throw error;
  }
}

/** Replace whole files so a reader in another process never sees a partial write. */
export function writeJSONFile(path: string, value: unknown) {
  const temporary = `${path}.${randomUUID()}.tmp`;
  writeFileSync(temporary, JSON.stringify(value), { mode: 0o600 });
  renameSync(temporary, path);
}

export function readJSONFile<T>(path: string): T | undefined {
  const text = readText(path);
  return text === undefined ? undefined : (JSON.parse(text) as T);
}

/**
 * The mock host keeps nothing in memory. A scenario's protocol state, controls
 * and journal live here, so test code can read and change them synchronously
 * and a request from an earlier test is still answered.
 */
export class ScenarioFiles {
  readonly dir: string;
  constructor(dir: string) {
    this.dir = dir;
  }
  map<V>(name: string) {
    const path = resolve(this.dir, `${name}.json`);
    const load = () =>
      new Map<string, V>(
        Object.entries(readJSONFile<Record<string, V>>(path) ?? {}),
      );
    const save = (value: Map<string, V>) =>
      writeJSONFile(path, Object.fromEntries(value));
    return {
      get: (key: string) => load().get(key),
      has: (key: string) => load().has(key),
      set(key: string, value: V) {
        const current = load();
        current.set(key, value);
        save(current);
      },
      delete(key: string) {
        const current = load();
        current.delete(key);
        save(current);
      },
      entries: () => [...load().entries()],
      values: () => [...load().values()],
      clear: () => save(new Map()),
    };
  }
  journal<V>(name: string) {
    const path = resolve(this.dir, `${name}.jsonl`);
    return {
      push: (value: V) =>
        appendFileSync(path, `${JSON.stringify(value)}\n`, { mode: 0o600 }),
      list: (): V[] =>
        (readText(path) ?? "")
          .split("\n")
          .filter(Boolean)
          .map((line) => JSON.parse(line) as V),
      clear: () => rmSync(path, { force: true }),
    };
  }
}

export type ScenarioRequest = {
  files: ScenarioFiles;
  /** Listener origin plus this scenario's path prefix. */
  origin: string;
  /** Request path with the scenario prefix removed. */
  path: string;
  closing: () => boolean;
};

export function createScenario(runDir: string, kind: ScenarioKind) {
  const id = randomBytes(6).toString("hex");
  const dir = resolve(runDir, "scenarios", id);
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  writeFileSync(resolve(dir, "kind"), kind, { mode: 0o600 });
  return { id, files: new ScenarioFiles(dir) };
}

export function scenarioKind(runDir: string, id: string) {
  return readText(resolve(runDir, "scenarios", id, "kind")) as
    | ScenarioKind
    | undefined;
}

export function scenarioFiles(runDir: string, id: string) {
  return new ScenarioFiles(resolve(runDir, "scenarios", id));
}

const scenarioPrefix = /\/s\/([0-9a-f]{12})(?=\/|$)/g;

/** Accepts `/s/<id>/…` and well-known forms such as `/.well-known/x/s/<id>/…`. */
export function parseScenarioPath(pathname: string) {
  const id = [...pathname.matchAll(scenarioPrefix)][0]?.[1];
  if (!id) return undefined;
  return { id, path: pathname.replaceAll(scenarioPrefix, "") || "/" };
}
