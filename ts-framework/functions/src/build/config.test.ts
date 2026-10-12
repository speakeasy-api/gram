import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { findConfigFile, loadConfig } from "./config.ts";

let dir: string;
let stderr: string[];

beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), "functions-config-test-"));
  stderr = [];
  vi.spyOn(process.stderr, "write").mockImplementation((chunk) => {
    stderr.push(String(chunk));
    return true;
  });
});

afterEach(async () => {
  vi.restoreAllMocks();
  await rm(dir, { recursive: true, force: true });
});

async function touch(...parts: string[]) {
  const file = join(dir, ...parts);
  await mkdir(join(file, ".."), { recursive: true });
  await writeFile(file, "export default {};\n");
}

test("findConfigFile prefers speakeasy.config.* over gram.config.*", async () => {
  await touch("gram.config.ts");
  await touch("speakeasy.config.mjs");

  expect(findConfigFile(dir)).toBe(join(dir, "speakeasy.config.mjs"));
  expect(stderr).toEqual([]);
});

test("findConfigFile still reads gram.config.* with a note", async () => {
  await touch("gram.config.ts");

  expect(findConfigFile(dir)).toBe(join(dir, "gram.config.ts"));
  expect(stderr).toEqual([
    "Note: gram.config.ts is deprecated and still works. Rename it to speakeasy.config.ts.\n",
  ]);
});

test("findConfigFile returns undefined without a config file", () => {
  expect(findConfigFile(dir)).toBeUndefined();
});

test("defaults use the new file names when nothing exists", async () => {
  const config = await loadConfig(undefined, dir);

  expect(config.entrypoint).toBe(join("src", "functions.ts"));
  expect(config.deployStagingFile).toBe("speakeasy.deploy.json");
  expect(stderr).toEqual([]);
});

test("defaults prefer the new file names when both exist", async () => {
  await touch("src", "functions.ts");
  await touch("src", "gram.ts");
  await touch("speakeasy.deploy.json");
  await touch("gram.deploy.json");

  const config = await loadConfig(undefined, dir);

  expect(config.entrypoint).toBe(join("src", "functions.ts"));
  expect(config.deployStagingFile).toBe("speakeasy.deploy.json");
  expect(stderr).toEqual([]);
});

test("defaults fall back to the legacy file names when only they exist", async () => {
  await touch("src", "gram.ts");
  await touch("gram.deploy.json");

  const config = await loadConfig(undefined, dir);

  expect(config.entrypoint).toBe(join("src", "gram.ts"));
  expect(config.deployStagingFile).toBe("gram.deploy.json");
  expect(stderr).toEqual([
    "Note: gram.deploy.json is deprecated and still works. Rename it to speakeasy.deploy.json.\n",
  ]);
});

test("the default entrypoint is looked up in the config's cwd", async () => {
  await touch("app", "src", "gram.ts");
  await writeFile(
    join(dir, "speakeasy.config.mjs"),
    'export default { cwd: "app" };\n',
  );

  const config = await loadConfig(join(dir, "speakeasy.config.mjs"), dir);

  expect(config.entrypoint).toBe(join("src", "gram.ts"));
});

test("configured paths win over the defaults", async () => {
  await touch("src", "gram.ts");
  await touch("gram.deploy.json");
  await writeFile(
    join(dir, "speakeasy.config.mjs"),
    'export default { entrypoint: "src/main.ts", deployStagingFile: "deploy.json" };\n',
  );

  const config = await loadConfig(join(dir, "speakeasy.config.mjs"), dir);

  expect(config.entrypoint).toBe("src/main.ts");
  expect(config.deployStagingFile).toBe("deploy.json");
  expect(stderr).toEqual([]);
});

test("invalid config values throw", async () => {
  await writeFile(
    join(dir, "speakeasy.config.mjs"),
    "export default { scale: -1 };\n",
  );

  await expect(
    loadConfig(join(dir, "speakeasy.config.mjs"), dir),
  ).rejects.toThrow();
});
