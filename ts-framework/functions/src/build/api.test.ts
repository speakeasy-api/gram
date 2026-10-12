import {
  mkdir,
  mkdtemp,
  readFile,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { build, resolveProject } from "./index.ts";

let dir: string;

beforeEach(async () => {
  dir = await mkdtemp(join(tmpdir(), "gram-build-api-test-"));
  await writeFile(
    join(dir, "package.json"),
    JSON.stringify({ name: "@acme/my-tools", type: "module" }),
  );
  await mkdir(join(dir, "src"));
  await writeFile(
    join(dir, "src", "functions.ts"),
    [
      "export default {",
      '  manifest() { return { version: "0.0.0", tools: [{ name: "greet" }] }; },',
      "};",
      "",
    ].join("\n"),
  );
});

afterEach(async () => {
  await rm(dir, { recursive: true, force: true });
});

test("resolveProject applies defaults and infers the slug from package.json", async () => {
  const project = await resolveProject({ cwd: dir });

  expect(project).toEqual({
    cwd: dir,
    outDir: join(dir, "dist"),
    zipFile: join(dir, "dist", "functions.zip"),
    deployStagingFile: join(dir, "speakeasy.deploy.json"),
    slug: "my-tools",
    deployProject: undefined,
    scale: undefined,
    memoryMiB: undefined,
  });
});

test("resolveProject reads speakeasy.config.ts and lets options override it", async () => {
  await writeFile(
    join(dir, "speakeasy.config.ts"),
    [
      "export default {",
      '  slug: "custom-slug",',
      '  outDir: "out",',
      '  deployProject: "my-project",',
      '  deployStagingFile: "deploy/stage.json",',
      "  scale: 2,",
      "  memoryMiB: 512,",
      "};",
      "",
    ].join("\n"),
  );

  const fromConfig = await resolveProject({ cwd: dir });
  expect(fromConfig).toMatchObject({
    slug: "custom-slug",
    outDir: join(dir, "out"),
    zipFile: join(dir, "out", "functions.zip"),
    deployStagingFile: join(dir, "deploy", "stage.json"),
    deployProject: "my-project",
    scale: 2,
    memoryMiB: 512,
  });

  const overridden = await resolveProject({ cwd: dir, outDir: "build" });
  expect(overridden.zipFile).toBe(join(dir, "build", "functions.zip"));
});

test("resolveProject uses an explicit config file", async () => {
  await writeFile(
    join(dir, "other.config.ts"),
    'export default { slug: "from-other" };\n',
  );

  const project = await resolveProject({
    cwd: dir,
    configFile: "other.config.ts",
  });

  expect(project.slug).toBe("from-other");
});

test("build writes the zip and manifest and returns the resolved project", async () => {
  const result = await build({
    cwd: dir,
    outDir: "out",
    logLevel: "warning",
  });

  const zipFile = join(dir, "out", "functions.zip");
  expect(result.project.zipFile).toBe(zipFile);
  expect(result.project.slug).toBe("my-tools");
  expect(result.files).toEqual([{ path: zipFile, size: expect.any(Number) }]);

  const manifest = JSON.parse(
    await readFile(join(dir, "out", "manifest.json"), "utf-8"),
  );
  expect(manifest).toEqual({ version: "0.0.0", tools: [{ name: "greet" }] });

  const zip = await readFile(zipFile);
  expect(zip.includes("manifest.json")).toBe(true);
  expect(zip.includes("functions.js")).toBe(true);
});

test("build honours the entrypoint option", async () => {
  await writeFile(
    join(dir, "src", "other.ts"),
    'export default { manifest() { return { version: "0.0.0", tools: [] }; } };\n',
  );

  await build({ cwd: dir, entrypoint: "src/other.ts", logLevel: "warning" });

  const manifest = JSON.parse(
    await readFile(join(dir, "dist", "manifest.json"), "utf-8"),
  );
  expect(manifest.tools).toEqual([]);
});

test("a project on the legacy file names resolves and builds unchanged", async () => {
  vi.spyOn(process.stderr, "write").mockImplementation(() => true);
  try {
    await rename(join(dir, "src", "functions.ts"), join(dir, "src", "gram.ts"));
    await writeFile(
      join(dir, "gram.config.ts"),
      'export default { slug: "legacy" };\n',
    );
    await writeFile(join(dir, "gram.deploy.json"), "{}\n");

    const project = await resolveProject({ cwd: dir });
    expect(project.slug).toBe("legacy");
    expect(project.deployStagingFile).toBe(join(dir, "gram.deploy.json"));

    const result = await build({ cwd: dir, logLevel: "warning" });
    expect(result.project.zipFile).toBe(join(dir, "dist", "functions.zip"));
    const manifest = JSON.parse(
      await readFile(join(dir, "dist", "manifest.json"), "utf-8"),
    );
    expect(manifest.tools).toEqual([{ name: "greet" }]);
  } finally {
    vi.restoreAllMocks();
  }
});

test("resolveProject keeps a gram.zip built by an older SDK deployable", async () => {
  await mkdir(join(dir, "dist"));
  await writeFile(join(dir, "dist", "gram.zip"), "zip");

  const project = await resolveProject({ cwd: dir });
  expect(project.zipFile).toBe(join(dir, "dist", "gram.zip"));
});

test("build removes a gram.zip left by an older SDK", async () => {
  await mkdir(join(dir, "dist"));
  await writeFile(join(dir, "dist", "gram.zip"), "stale");

  await build({ cwd: dir, logLevel: "warning" });

  expect(existsSync(join(dir, "dist", "gram.zip"))).toBe(false);
  expect(existsSync(join(dir, "dist", "functions.zip"))).toBe(true);
  expect((await resolveProject({ cwd: dir })).zipFile).toBe(
    join(dir, "dist", "functions.zip"),
  );
});
