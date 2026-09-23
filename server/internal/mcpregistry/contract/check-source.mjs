// Run with an explicitly supplied pinned checkout containing its installed Effect dependency.
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";
import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import assert from "node:assert/strict";
const source = resolve(process.argv[2]);
const sourceFile = resolve(source, "packages/registry-api/src/Schema.ts");
const require = createRequire(
  resolve(source, "packages/registry-api/package.json"),
);
assert.equal(
  createHash("sha256").update(readFileSync(sourceFile)).digest("hex"),
  "775a4ee2c905874c4b3155452e3fc234d155ef79cb4fb17f40c46f2a9aa70fb5",
  "pinned schema source hash",
);
const { Schema } = await import(pathToFileURL(require.resolve("effect")).href);
const { ServerResponse } = await import(pathToFileURL(sourceFile).href);
const document = Schema.toJsonSchemaDocument(ServerResponse);
assert.deepEqual(document.definitions, {});
const exported = {
  $schema: "https://json-schema.org/draft/2020-12/schema",
  ...document.schema,
};
function adapt(node) {
  if (!node || typeof node !== "object") return;
  if (typeof node.format === "string") {
    assert.ok(["uri", "date-time"].includes(node.format));
    node.format =
      node.format === "uri" ? "gram-source-uri" : "gram-source-date-time";
  }
  for (const value of Object.values(node)) adapt(value);
}
adapt(exported);
const read = (name) =>
  JSON.parse(readFileSync(new URL(name, import.meta.url), "utf8"));
assert.deepEqual(exported, read("./record.schema.json"));
for (const c of read("./conformance.json"))
  assert.equal(Schema.is(ServerResponse)(c.record), c.valid, c.name);
const manifest = read("../baseline/manifest.json");
assert.equal(manifest.count, 2, "starter manifest count");
assert.equal(manifest.records.length, 2, "starter manifest records length");
for (const record of manifest.records) {
  const raw = readFileSync(
    resolve(source, "data/servers", record.file.slice("records/".length)),
  );
  assert.equal(createHash("sha256").update(raw).digest("hex"), record.sha256);
  assert.equal(Schema.is(ServerResponse)(JSON.parse(raw)), true);
}
console.log(
  "Pinned schema export, shared source parity and two starter hashes pass.",
);
