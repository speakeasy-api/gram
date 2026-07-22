import { getLogger } from "@logtape/logtape";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test } from "vitest";
import { createZipArchive } from "./gram.ts";

let outDir: string;

beforeEach(async () => {
  outDir = await mkdtemp(join(tmpdir(), "gram-zip-test-"));
});

afterEach(async () => {
  await rm(outDir, { recursive: true, force: true });
});

test("createZipArchive produces a zip with manifest and function entries", async () => {
  const manifestFilename = join(outDir, "manifest.json");
  const funcFilename = join(outDir, "functions.js");
  const zipFilename = join(outDir, "gram.zip");

  await writeFile(manifestFilename, JSON.stringify({ tools: [] }));
  await writeFile(funcFilename, "export default {};\n");

  await createZipArchive(getLogger(["gram-test"]), {
    manifestFilename,
    funcFilename,
    zipFilename,
  });

  const zip = await readFile(zipFilename);

  // Local file header signature.
  expect(zip.subarray(0, 4)).toEqual(Buffer.from("PK\x03\x04", "binary"));

  // Entry names are stored uncompressed in local and central headers.
  expect(zip.includes("manifest.json")).toBe(true);
  expect(zip.includes("functions.js")).toBe(true);

  // End of central directory record: signature PK\x05\x06, total entry
  // count is the uint16 at offset 10 (no zip comment is written).
  const eocd = zip.subarray(zip.length - 22);
  expect(eocd.subarray(0, 4)).toEqual(Buffer.from("PK\x05\x06", "binary"));
  expect(eocd.readUInt16LE(10)).toBe(2);
});
