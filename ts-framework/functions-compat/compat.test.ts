import { execFile } from "node:child_process";
import { existsSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { promisify } from "node:util";
import { expect, test } from "vitest";

const execFileAsync = promisify(execFile);

type PackageJSON = {
  name: string;
  version: string;
  exports: Record<string, { import: string; types?: string }>;
  bin?: Record<string, string>;
  dependencies?: Record<string, string>;
};

const here = import.meta.dirname;

async function readPackage(dir: string): Promise<PackageJSON> {
  return JSON.parse(await readFile(join(dir, "package.json"), "utf-8"));
}

// The re-exports resolve to the built primary package. CI builds it before
// testing; locally, the tests that import it are skipped until it is built.
const built = existsSync(join(here, "..", "functions", "dist", "index.js"));
const buildHint =
  "build @speakeasy-api/functions first: aube run --filter ./ts-framework/functions build";
if (!built && process.env["CI"]) {
  throw new Error(`The compat tests need the primary package: ${buildHint}`);
}
if (!built) {
  console.warn(`Skipping the re-export tests: ${buildHint}`);
}
const testBuilt = built ? test : test.skip;

const compat = await readPackage(here);
const primary = await readPackage(join(here, "..", "functions"));

// The primary package's ./gf entry backs the gf bin below rather than an
// import path, so the compat package has no matching subpath.
const binOnlyEntries = new Set(["./gf"]);

test("the compat package versions in lockstep with the primary package", () => {
  expect(compat.name).toBe("@gram-ai/functions");
  expect(primary.name).toBe("@speakeasy-api/functions");
  expect(compat.version).toBe(primary.version);
  expect(compat.dependencies).toEqual({
    "@speakeasy-api/functions": "workspace:*",
  });
});

test("the compat package exports every subpath of the primary package", () => {
  const expected = Object.keys(primary.exports).filter(
    (subpath) => !binOnlyEntries.has(subpath),
  );
  expect(Object.keys(compat.exports).sort()).toEqual(expected.sort());
  for (const entry of Object.values(compat.exports)) {
    expect(entry.types).toBeDefined();
  }
});

for (const subpath of Object.keys(compat.exports)) {
  testBuilt(
    `@gram-ai/functions${subpath.slice(1)} re-exports @speakeasy-api/functions${subpath.slice(1)}`,
    async () => {
      const suffix = subpath.slice(1);
      const legacy = await import(`@gram-ai/functions${suffix}`);
      const current = await import(`@speakeasy-api/functions${suffix}`);

      expect(Object.keys(legacy).sort()).toEqual(Object.keys(current).sort());
      expect(Object.keys(legacy).length).toBeGreaterThan(0);
      for (const name of Object.keys(current)) {
        expect(legacy[name], name).toBe(current[name]);
      }
    },
  );
}

testBuilt(
  "the legacy names stay available through @gram-ai/functions",
  async () => {
    const legacy = await import("@gram-ai/functions");
    const legacyMCP = await import("@gram-ai/functions/mcp");

    expect(legacy.Gram).toBe(legacy.Functions);
    expect(new legacy.Gram()).toBeInstanceOf(legacy.Functions);
    expect(legacyMCP.fromGram).toBe(legacyMCP.fromFunctions);
    expect(legacyMCP.withGram).toBe(legacyMCP.withFunctions);
  },
);

testBuilt("the gf bin runs the primary package's CLI", async () => {
  expect(compat.bin).toEqual({ gf: "./bin/gf.js" });

  const { stdout } = await execFileAsync(process.execPath, [
    join(here, "bin", "gf.js"),
    "--version",
  ]);
  expect(stdout.trim()).toBe(primary.version);
});
